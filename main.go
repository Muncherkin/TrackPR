// Command trackpr is a terminal UI for tracking the open pull requests you have
// authored, grouped by the local git repositories discovered under your home
// directory. The right pane is a folder tree of those repos; selecting a folder
// filters the PR log to repos living under it.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	root := flag.String("root", "", "directory to scan for git repositories (default: $HOME)")
	flag.Parse()

	scanRoot := *root
	if scanRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "trackpr: cannot determine home directory:", err)
			os.Exit(1)
		}
		scanRoot = home
	}

	repos, err := DiscoverRepos(scanRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trackpr: scanning", scanRoot, "failed:", err)
		os.Exit(1)
	}
	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "trackpr: no git repositories found under", scanRoot)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(scanRoot, repos), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "trackpr:", err)
		os.Exit(1)
	}
}
