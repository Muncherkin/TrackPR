package main

import (
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type focusArea int

const (
	focusPRs focusArea = iota
	focusTree
)

type rowKind int

const (
	rowHeader rowKind = iota // a repo group header
	rowPR                    // a pull request
)

type prRow struct {
	kind  rowKind
	repo  string // group header: nameWithOwner
	count int    // group header: number of PRs in the group
	pr    *PR    // pr row
}

// Auto-refresh: re-fetch PRs after this much idle time, where any keypress
// resets the clock so a refresh never lands mid-navigation. idleCheckInterval
// is how often the idle timer is sampled.
const (
	refreshAfter      = 60 * time.Second
	idleCheckInterval = 2 * time.Second
)

// Messages.
type prsMsg struct {
	prs []PR
	err error
}
type (
	tickMsg     struct{} // spinner animation tick
	idleTickMsg struct{} // periodic idle-timer sample
)

type model struct {
	scanRoot  string
	repos     []Repo
	nwoToRepo map[string][]*Repo // github nameWithOwner -> local repos
	prs       []PR               // PRs whose repo exists locally

	tree    *TreeNode
	visible []*TreeNode // flattened visible tree
	rows    []prRow     // rendered PR-pane rows (headers + prs), after filtering

	selPrefix string // active tree filter; "" = all
	focus     focusArea

	prCursor   int
	prOffset   int
	treeCursor int
	treeOffset int

	loading      bool // initial fetch, before any PRs are on screen
	refreshing   bool // background re-fetch while the PR list stays visible
	err          error
	frame        int
	lastActivity time.Time // last keypress; auto-refresh fires refreshAfter past this

	width, height int
	leftW, rightW int // total pane widths (incl. borders)
	contentH      int // content rows inside a pane (excludes border + title)
}

func newModel(scanRoot string, repos []Repo) model {
	nwo := map[string][]*Repo{}
	for i := range repos {
		if repos[i].NameWithOwner != "" {
			nwo[repos[i].NameWithOwner] = append(nwo[repos[i].NameWithOwner], &repos[i])
		}
	}
	m := model{
		scanRoot:     scanRoot,
		repos:        repos,
		nwoToRepo:    nwo,
		tree:         BuildTree(repos, "All repositories"),
		focus:        focusPRs,
		loading:      true,
		lastActivity: time.Now(),
	}
	m.rebuildVisible()
	return m
}

// rebuildVisible recomputes the flattened tree shown in the right pane. Once
// PRs have loaded it omits any folder or repo with no open PRs; while loading
// it shows the full tree so discovery progress is visible.
func (m *model) rebuildVisible() {
	if m.loading {
		m.visible = m.tree.Flatten()
	} else {
		m.visible = m.flattenActive()
	}
	if m.treeCursor >= len(m.visible) {
		m.treeCursor = len(m.visible) - 1
	}
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
	m.clampScroll()
}

// flattenActive walks the tree in display order, skipping subtrees that contain
// no open PRs. The root ("all repositories") is always kept.
func (m *model) flattenActive() []*TreeNode {
	var out []*TreeNode
	var walk func(*TreeNode)
	walk = func(n *TreeNode) {
		if n.Prefix != "" && m.countPRsUnder(n.Prefix) == 0 {
			return
		}
		out = append(out, n)
		if n.Expanded {
			for _, c := range n.Children {
				walk(c)
			}
		}
	}
	walk(m.tree)
	return out
}

// reposWithPRs counts the distinct local repositories that have an open PR.
func (m *model) reposWithPRs() int {
	seen := map[string]bool{}
	for _, pr := range m.prs {
		for _, r := range m.nwoToRepo[pr.RepoNWO] {
			seen[r.Rel] = true
		}
	}
	return len(seen)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), m.tickCmd(), m.idleTickCmd())
}

func fetchCmd() tea.Cmd {
	return func() tea.Msg {
		prs, err := FetchPRs()
		return prsMsg{prs: prs, err: err}
	}
}

func (m model) tickCmd() tea.Cmd {
	return tea.Tick(110*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) idleTickCmd() tea.Cmd {
	return tea.Tick(idleCheckInterval, func(time.Time) tea.Msg { return idleTickMsg{} })
}

func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		bin := "open" // darwin
		if runtime.GOOS == "linux" {
			bin = "xdg-open"
		}
		_ = exec.Command(bin, url).Start()
		return nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.recomputeLayout()
		m.clampScroll()
		return m, nil

	case tickMsg:
		if m.loading || m.refreshing {
			m.frame++
			return m, m.tickCmd()
		}
		return m, nil

	case idleTickMsg:
		// Auto-refresh once the app has been idle for refreshAfter; any
		// keypress moves lastActivity forward, resetting the countdown. This
		// is a background refresh: the PR list stays on screen and only a
		// small spinner signals the re-fetch.
		if !m.loading && !m.refreshing && time.Since(m.lastActivity) >= refreshAfter {
			m.refreshing = true
			m.lastActivity = time.Now()
			return m, tea.Batch(fetchCmd(), m.tickCmd(), m.idleTickCmd())
		}
		return m, m.idleTickCmd()

	case prsMsg:
		m.loading = false
		m.refreshing = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		local := make([]PR, 0, len(msg.prs))
		for _, pr := range msg.prs {
			if _, ok := m.nwoToRepo[pr.RepoNWO]; ok {
				local = append(local, pr)
			}
		}
		m.prs = local
		m.rebuildRows()
		m.expandToPRs()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any interaction resets the auto-refresh countdown.
	m.lastActivity = time.Now()

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "tab", "shift+tab":
		if m.focus == focusPRs {
			m.focus = focusTree
		} else {
			m.focus = focusPRs
		}

	case "r":
		if !m.loading && !m.refreshing {
			// Background refresh: keep whatever is on screen and show only a
			// small spinner, rather than blanking the pane.
			m.refreshing = true
			return m, tea.Batch(fetchCmd(), m.tickCmd())
		}

	case "up", "k":
		if m.focus == focusTree {
			m.moveTree(-1)
		} else {
			m.movePR(-1)
		}

	case "down", "j":
		if m.focus == focusTree {
			m.moveTree(1)
		} else {
			m.movePR(1)
		}

	case "g", "home":
		if m.focus == focusTree {
			m.treeCursor = 0
		} else {
			m.prCursor = m.firstPRRow()
		}
		m.clampScroll()

	case "G", "end":
		if m.focus == focusTree {
			m.treeCursor = len(m.visible) - 1
		} else {
			m.prCursor = m.lastPRRow()
		}
		m.clampScroll()

	case "right", "l":
		if m.focus == focusTree {
			m.setExpanded(true)
		}

	case "left", "h":
		if m.focus == focusTree {
			m.setExpanded(false)
		}

	case " ":
		if m.focus == focusTree {
			m.toggleExpanded()
		}

	case "enter":
		if m.focus == focusTree {
			m.applyTreeFilter()
		} else if url := m.currentPRURL(); url != "" {
			return m, openURLCmd(url)
		}

	case "esc", "backspace", "0":
		m.selPrefix = ""
		m.rebuildRows()
	}
	return m, nil
}

// --- tree navigation ---

func (m *model) moveTree(d int) {
	if len(m.visible) == 0 {
		return
	}
	m.treeCursor += d
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
	if m.treeCursor >= len(m.visible) {
		m.treeCursor = len(m.visible) - 1
	}
	m.clampScroll()
}

func (m *model) setExpanded(v bool) {
	if len(m.visible) == 0 {
		return
	}
	n := m.visible[m.treeCursor]
	if len(n.Children) > 0 {
		n.Expanded = v
		m.rebuildVisible()
	}
}

func (m *model) toggleExpanded() {
	if len(m.visible) == 0 {
		return
	}
	n := m.visible[m.treeCursor]
	if len(n.Children) > 0 {
		n.Expanded = !n.Expanded
		m.rebuildVisible()
	}
}

// applyTreeFilter sets the PR filter to the highlighted node's subtree.
func (m *model) applyTreeFilter() {
	if len(m.visible) == 0 {
		return
	}
	n := m.visible[m.treeCursor]
	m.selPrefix = n.Prefix
	if len(n.Children) > 0 && !n.Expanded {
		n.Expanded = true
	}
	m.rebuildVisible()
	m.rebuildRows()
}

// --- PR navigation ---

func (m *model) movePR(d int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.prCursor
	for {
		i += d
		if i < 0 || i >= len(m.rows) {
			return // no further PR row in that direction; stay put
		}
		if m.rows[i].kind == rowPR {
			m.prCursor = i
			m.clampScroll()
			return
		}
	}
}

func (m *model) firstPRRow() int {
	for i, r := range m.rows {
		if r.kind == rowPR {
			return i
		}
	}
	return 0
}

func (m *model) lastPRRow() int {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].kind == rowPR {
			return i
		}
	}
	return 0
}

func (m *model) currentPRURL() string {
	if m.prCursor >= 0 && m.prCursor < len(m.rows) && m.rows[m.prCursor].kind == rowPR {
		return m.rows[m.prCursor].pr.URL
	}
	return ""
}

// --- data assembly ---

// rebuildRows recomputes the PR-pane rows for the current filter: PRs are
// grouped by repo, sorted within a group by update time ascending, and the
// groups themselves are ordered by their most recent update ascending.
func (m *model) rebuildRows() {
	// Remember the highlighted PR so a refresh (or filter change) keeps it
	// selected instead of snapping the cursor back to the top.
	prevURL := ""
	if m.prCursor >= 0 && m.prCursor < len(m.rows) && m.rows[m.prCursor].kind == rowPR {
		prevURL = m.rows[m.prCursor].pr.URL
	}

	groups := map[string][]PR{}
	for _, pr := range m.prs {
		if m.prMatches(pr) {
			groups[pr.RepoNWO] = append(groups[pr.RepoNWO], pr)
		}
	}

	type grp struct {
		nwo    string
		prs    []PR
		latest time.Time
	}
	gs := make([]grp, 0, len(groups))
	for nwo, ps := range groups {
		sort.Slice(ps, func(i, j int) bool { return ps[i].UpdatedAt.Before(ps[j].UpdatedAt) })
		gs = append(gs, grp{nwo: nwo, prs: ps, latest: ps[len(ps)-1].UpdatedAt})
	}
	sort.Slice(gs, func(i, j int) bool {
		if !gs[i].latest.Equal(gs[j].latest) {
			return gs[i].latest.Before(gs[j].latest)
		}
		return gs[i].nwo < gs[j].nwo
	})

	rows := make([]prRow, 0)
	for gi := range gs {
		rows = append(rows, prRow{kind: rowHeader, repo: gs[gi].nwo, count: len(gs[gi].prs)})
		for pi := range gs[gi].prs {
			rows = append(rows, prRow{kind: rowPR, pr: &gs[gi].prs[pi]})
		}
	}
	m.rows = rows
	m.prCursor = m.firstPRRow()
	if prevURL != "" {
		for i, r := range m.rows {
			if r.kind == rowPR && r.pr.URL == prevURL {
				m.prCursor = i
				break
			}
		}
	}
	m.prOffset = 0
	m.clampScroll()
}

// expandToPRs opens the folders that contain an open PR (and the root) so the
// relevant repositories reveal themselves, while noise stays collapsed.
func (m *model) expandToPRs() {
	want := map[string]bool{}
	for _, pr := range m.prs {
		for _, r := range m.nwoToRepo[pr.RepoNWO] {
			prefix := ""
			for _, p := range strings.Split(r.Rel, "/") {
				if prefix == "" {
					prefix = p
				} else {
					prefix += "/" + p
				}
				want[prefix] = true
			}
		}
	}
	var walk func(*TreeNode)
	walk = func(n *TreeNode) {
		if len(n.Children) > 0 && (n.Prefix == "" || want[n.Prefix]) {
			n.Expanded = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(m.tree)
	m.rebuildVisible()
}

func (m *model) prMatches(pr PR) bool {
	for _, r := range m.nwoToRepo[pr.RepoNWO] {
		if relHasPrefix(r.Rel, m.selPrefix) {
			return true
		}
	}
	return false
}

// countPRsUnder returns how many open PRs live under the given path prefix.
func (m *model) countPRsUnder(prefix string) int {
	n := 0
	for _, pr := range m.prs {
		for _, r := range m.nwoToRepo[pr.RepoNWO] {
			if relHasPrefix(r.Rel, prefix) {
				n++
				break
			}
		}
	}
	return n
}

// --- layout / scrolling ---

func (m *model) recomputeLayout() {
	if m.width < 40 || m.height < 10 {
		m.contentH = 0
		return
	}
	left := m.width * 62 / 100
	if left < 30 {
		left = 30
	}
	right := m.width - left - 1
	if right < 18 {
		right = 18
		left = m.width - right - 1
	}
	m.leftW, m.rightW = left, right
	m.contentH = m.height - 2 /*header+footer*/ - 2 /*pane border*/ - 1 /*pane title*/
	if m.contentH < 1 {
		m.contentH = 1
	}
}

func (m *model) clampScroll() {
	m.prOffset = clampOffset(m.prCursor, m.prOffset, m.contentH, len(m.rows))
	m.treeOffset = clampOffset(m.treeCursor, m.treeOffset, m.contentH, len(m.visible))
}

func clampOffset(cursor, offset, height, total int) int {
	if height < 1 {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	if offset > total-height {
		offset = total - height
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}
