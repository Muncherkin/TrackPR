package main

import (
	"bufio"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Repo is a git repository discovered on the local filesystem.
type Repo struct {
	Path          string // absolute path to the working tree
	Rel           string // slash-separated path relative to the scan root, e.g. "acme/web"
	Owner         string // GitHub owner, "" if the remote is not on github.com
	Name          string // GitHub repo name
	NameWithOwner string // "owner/name", "" if not a github remote
}

// Directory names we never descend into: package caches, build output, and
// tooling dirs that are git repos themselves but never hold the user's PRs.
var skipDirs = map[string]bool{
	"node_modules": true, "Library": true, "Applications": true,
	"vendor": true, "Pods": true, "target": true, "build": true,
	"dist": true, ".build": true, ".next": true, "__pycache__": true,
	".venv": true, "venv": true, ".cache": true, "Caches": true,
}

// DiscoverRepos walks root and returns every git repository underneath it,
// resolving each one's GitHub remote. It does not descend into a repository
// once found, so vendored submodules nested inside a working tree are skipped.
func DiscoverRepos(root string) ([]Repo, error) {
	root = filepath.Clean(root)
	var repos []Repo

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || !d.IsDir() {
			return nil // skip unreadable entries and files
		}
		if isGitRepo(path) {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				rel = filepath.Base(path)
			}
			rel = filepath.ToSlash(rel)
			// Skip repos living under a hidden path component (e.g. ~/.nvm,
			// ~/.oh-my-zsh) — tooling clones, never the user's own projects.
			if !hasHiddenComponent(rel) {
				repos = append(repos, Repo{Path: path, Rel: rel})
			}
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || skipDirs[base] {
			return filepath.SkipDir
		}
		// Prune the Go module/build cache (~/go/pkg) — huge and never a repo.
		if base == "pkg" && filepath.Base(filepath.Dir(path)) == "go" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	resolveRemotes(repos)
	sort.Slice(repos, func(i, j int) bool { return repos[i].Rel < repos[j].Rel })
	return repos, nil
}

func hasHiddenComponent(rel string) bool {
	for _, p := range strings.Split(rel, "/") {
		if p != "." && p != ".." && strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
}

func isGitRepo(dir string) bool {
	// .git is a directory in normal clones, a file in submodules/worktrees.
	fi, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (fi.IsDir() || fi.Mode().IsRegular())
}

// resolveRemotes fills Owner/Name/NameWithOwner for each repo concurrently.
func resolveRemotes(repos []Repo) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			owner, name := parseRemote(repos[i].Path)
			repos[i].Owner, repos[i].Name = owner, name
			if owner != "" && name != "" {
				repos[i].NameWithOwner = owner + "/" + name
			}
		}(i)
	}
	wg.Wait()
}

var remoteREs = []*regexp.Regexp{
	regexp.MustCompile(`^git@github\.com:([^/]+)/(.+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/(.+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^https?://(?:[^@]+@)?github\.com/([^/]+)/(.+?)(?:\.git)?/?$`),
}

func parseRemote(repoPath string) (owner, name string) {
	url := gitRemoteURL(repoPath, "origin")
	if url == "" {
		url = gitRemoteURL(repoPath, "")
	}
	url = strings.TrimSpace(url)
	for _, re := range remoteREs {
		if m := re.FindStringSubmatch(url); m != nil {
			return m[1], strings.TrimSuffix(m[2], ".git")
		}
	}
	return "", ""
}

// gitRemoteURL returns the URL of the named remote, or (if remote == "") the
// URL of the first remote configured for the repo.
func gitRemoteURL(repoPath, remote string) string {
	var args []string
	if remote != "" {
		args = []string{"-C", repoPath, "config", "--get", "remote." + remote + ".url"}
	} else {
		args = []string{"-C", repoPath, "config", "--get-regexp", `^remote\..*\.url$`}
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	if remote != "" {
		return strings.TrimSpace(string(out))
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	if sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) >= 2 {
			return fields[len(fields)-1]
		}
	}
	return ""
}
