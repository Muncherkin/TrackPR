package main

import (
	"sort"
	"strings"
)

// TreeNode is one entry in the right-hand folder tree. Folders group repos;
// repos are leaves. Prefix is the slash-path the node represents and is used to
// filter the PR list to repos living under it.
type TreeNode struct {
	Label    string
	Prefix   string // "" for the root ("all repositories")
	Depth    int
	IsRepo   bool
	Repo     *Repo
	Children []*TreeNode
	Expanded bool
}

// BuildTree assembles the folder hierarchy from the repos' relative paths.
// rootLabel names the synthetic root node that selects every repo.
func BuildTree(repos []Repo, rootLabel string) *TreeNode {
	root := &TreeNode{Label: rootLabel, Prefix: "", Expanded: true}

	for i := range repos {
		r := &repos[i]
		parts := strings.Split(r.Rel, "/")
		cur := root
		prefix := ""
		for j, p := range parts {
			if prefix == "" {
				prefix = p
			} else {
				prefix += "/" + p
			}
			var child *TreeNode
			for _, c := range cur.Children {
				if c.Label == p {
					child = c
					break
				}
			}
			if child == nil {
				child = &TreeNode{Label: p, Prefix: prefix, Depth: j + 1}
				cur.Children = append(cur.Children, child)
			}
			if j == len(parts)-1 {
				child.IsRepo = true
				child.Repo = r
			}
			cur = child
		}
	}

	sortTree(root)
	return root
}

// sortTree orders siblings: folders before repos, then alphabetically.
func sortTree(n *TreeNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		af, bf := len(a.Children) > 0, len(b.Children) > 0
		if af != bf {
			return af // folders first
		}
		return a.Label < b.Label
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// Flatten returns the nodes that are currently visible (respecting expansion),
// in display order.
func (n *TreeNode) Flatten() []*TreeNode {
	var out []*TreeNode
	var walk func(*TreeNode)
	walk = func(node *TreeNode) {
		out = append(out, node)
		if node.Expanded {
			for _, c := range node.Children {
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

// relHasPrefix reports whether a repo's relative path lives under prefix.
// An empty prefix (the root) matches everything.
func relHasPrefix(rel, prefix string) bool {
	if prefix == "" {
		return true
	}
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}
