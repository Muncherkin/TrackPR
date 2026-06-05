package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func sampleRepos() []Repo {
	mk := func(rel, nwo string) Repo {
		return Repo{Path: "/h/" + rel, Rel: rel, NameWithOwner: nwo}
	}
	return []Repo{
		mk("acme/api", "acme/api"),
		mk("acme/web", "acme/web"),
		mk("acme/mobile-ws/mobile-app", "acme/mobile-app"),
		mk("globex/auth", "globex/auth"),
		mk("globex/services/backend", "globex/backend"),
		mk("hobby/snake", "playground/snake"),
	}
}

func samplePRs(now time.Time) []PR {
	return []PR{
		{Number: 42, Title: "Add pagination to the list endpoint", RepoNWO: "acme/api", UpdatedAt: now.Add(-48 * time.Hour)},
		{Number: 51, Title: "Fix race condition on startup", RepoNWO: "acme/api", UpdatedAt: now.Add(-5 * time.Hour)},
		{Number: 8489, Title: "Refactor the chart rendering pipeline", RepoNWO: "acme/web", UpdatedAt: now.Add(-26 * time.Hour), IsDraft: true},
		{Number: 12, Title: "Rotate OAuth refresh tokens", RepoNWO: "globex/auth", UpdatedAt: now.Add(-72 * time.Hour)},
	}
}

func renderModel(t *testing.T, w, h int) (model, string) {
	t.Helper()
	now := time.Now()
	var mi tea.Model = newModel("/h", sampleRepos())
	mi, _ = mi.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mi, _ = mi.Update(prsMsg{prs: samplePRs(now)})
	m := mi.(model)
	return m, m.View()
}

// Every rendered line must be exactly the terminal width and the frame exactly
// the terminal height — otherwise the layout wraps and the panes break.
func TestFrameDimensions(t *testing.T) {
	for _, dim := range [][2]int{{100, 30}, {80, 24}, {120, 40}, {60, 20}} {
		w, h := dim[0], dim[1]
		_, out := renderModel(t, w, h)
		lines := strings.Split(out, "\n")
		if len(lines) != h {
			t.Errorf("%dx%d: got %d lines, want %d", w, h, len(lines), h)
		}
		for i, ln := range lines {
			if got := lipgloss.Width(ln); got != w {
				t.Errorf("%dx%d: line %d width=%d want %d: %q", w, h, i, got, w, ln)
			}
		}
	}
}

// PRs group by repo, sort by update time ascending within a group, and groups
// order by their most recent update ascending.
func TestGroupingOrder(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	var seq []string
	for _, r := range m.rows {
		if r.kind == rowHeader {
			seq = append(seq, "REPO:"+r.repo)
		} else {
			seq = append(seq, r.pr.RepoNWO+"#"+itoa(r.pr.Number))
		}
	}
	want := []string{
		"REPO:globex/auth", "globex/auth#12",
		"REPO:acme/web", "acme/web#8489",
		"REPO:acme/api", "acme/api#42", "acme/api#51",
	}
	if strings.Join(seq, "|") != strings.Join(want, "|") {
		t.Errorf("row order mismatch:\n got %v\nwant %v", seq, want)
	}
}

// Selecting a folder prefix filters the PR list to repos under it.
func TestTreeFilter(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	m.selPrefix = "globex"
	m.rebuildRows()
	for _, r := range m.rows {
		if r.kind == rowPR && !strings.HasPrefix(r.pr.RepoNWO, "globex/") {
			t.Errorf("filter 'globex' leaked PR from %s", r.pr.RepoNWO)
		}
	}
	if n := m.countPRsUnder("globex"); n != 1 {
		t.Errorf("countPRsUnder(globex)=%d want 1", n)
	}
	if n := m.countPRsUnder("acme/api"); n != 2 {
		t.Errorf("countPRsUnder(acme/api)=%d want 2", n)
	}
}

// Once PRs load, the tree shows only folders/repos that contain open PRs.
func TestTreeActiveFilter(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	labels := map[string]bool{}
	for _, n := range m.visible {
		labels[n.Prefix] = true
		if n.Prefix != "" && m.countPRsUnder(n.Prefix) == 0 {
			t.Errorf("tree shows %q which has no open PRs", n.Prefix)
		}
	}
	if !labels[""] {
		t.Error("root node should always be visible")
	}
	// hobby/snake has no PR in the sample set; it must be hidden.
	if labels["hobby"] || labels["hobby/snake"] {
		t.Error("PR-less folder 'hobby' leaked into the tree")
	}
	// acme/web has PRs and must be present.
	if !labels["acme"] || !labels["acme/web"] {
		t.Error("expected acme and acme/web to be visible")
	}
}

func TestTreeShape(t *testing.T) {
	tree := BuildTree(sampleRepos(), "All repositories")
	// top-level folders, alphabetical, folders before repos
	var top []string
	for _, c := range tree.Children {
		top = append(top, c.Label)
	}
	want := []string{"acme", "globex", "hobby"}
	if strings.Join(top, ",") != strings.Join(want, ",") {
		t.Errorf("top level = %v want %v", top, want)
	}
}

// With zero open PRs the tree collapses to just the always-present root, and
// the frame must still render at the right dimensions.
func TestNoPRsKeepsRoot(t *testing.T) {
	var mi tea.Model = newModel("/h", sampleRepos())
	mi, _ = mi.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mi, _ = mi.Update(prsMsg{prs: nil})
	m := mi.(model)
	if len(m.visible) != 1 || m.visible[0].Prefix != "" {
		t.Fatalf("expected only the root node visible, got %d nodes", len(m.visible))
	}
	if len(m.rows) != 0 {
		t.Errorf("expected no PR rows, got %d", len(m.rows))
	}
	out := m.View()
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != 100 {
			t.Errorf("line %d width=%d want 100", i, w)
		}
	}
}

// After refreshAfter of idle time, an idle tick starts a background refresh:
// the PR list stays loaded (loading false) while refreshing flips true.
func TestIdleRefreshTriggers(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	m.loading = false
	m.lastActivity = time.Now().Add(-(refreshAfter + time.Second))
	var mi tea.Model = m
	mi, cmd := mi.Update(idleTickMsg{})
	if got := mi.(model); !got.refreshing {
		t.Error("expected auto-refresh to start after the idle period")
	} else if got.loading {
		t.Error("background refresh must not blank the pane via loading")
	}
	if cmd == nil {
		t.Error("expected refresh commands to be scheduled")
	}
}

// An idle tick that arrives while the app was recently used does nothing.
func TestIdleRefreshSkippedWhenRecent(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	m.loading = false
	m.lastActivity = time.Now()
	var mi tea.Model = m
	mi, _ = mi.Update(idleTickMsg{})
	if got := mi.(model); got.loading || got.refreshing {
		t.Error("auto-refresh should not fire right after interaction")
	}
}

// A keypress resets the idle countdown so a refresh won't land mid-navigation.
func TestKeypressResetsIdleClock(t *testing.T) {
	m, _ := renderModel(t, 100, 30)
	m.lastActivity = time.Now().Add(-(refreshAfter + time.Second))
	var mi tea.Model = m
	mi, _ = mi.Update(tea.KeyMsg{Type: tea.KeyDown})
	if d := time.Since(mi.(model).lastActivity); d > time.Second {
		t.Errorf("keypress did not reset the idle clock; since=%s", d)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
