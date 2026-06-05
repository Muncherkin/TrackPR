package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// --- palette ---

var (
	accent    = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"} // violet
	accent2   = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"} // cyan
	fgColor   = lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"}
	subtle    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	faint     = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	borderDim = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#374151"}
	good      = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	warn      = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	danger    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	selBg     = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#3730A3"}
)

// --- styles ---

var (
	headerLogo = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
	headerSub  = lipgloss.NewStyle().Foreground(subtle)
	headerPath = lipgloss.NewStyle().Foreground(faint).Italic(true)

	paneTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent2)
	paneTitleDim    = lipgloss.NewStyle().Foreground(faint)
	dimStyle        = lipgloss.NewStyle().Foreground(subtle)
	errStyle        = lipgloss.NewStyle().Foreground(danger).Bold(true)

	groupBarStyle    = lipgloss.NewStyle().Foreground(accent)
	groupNameStyle   = lipgloss.NewStyle().Bold(true).Foreground(fgColor)
	groupCountStyle  = lipgloss.NewStyle().Foreground(faint)

	prNumStyle   = lipgloss.NewStyle().Foreground(accent2)
	prTitleStyle = lipgloss.NewStyle().Foreground(fgColor)
	prTimeStyle  = lipgloss.NewStyle().Foreground(faint)
	draftStyle   = lipgloss.NewStyle().Foreground(warn)
	prSelStyle   = lipgloss.NewStyle().Foreground(fgColor).Background(selBg)

	rootGlyphStyle    = lipgloss.NewStyle().Foreground(accent)
	folderGlyphStyle  = lipgloss.NewStyle().Foreground(accent2)
	repoGlyphStyle    = lipgloss.NewStyle().Foreground(good)
	treeLabelStyle    = lipgloss.NewStyle().Foreground(fgColor)
	treeSelLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	treeCursorStyle   = lipgloss.NewStyle().Foreground(fgColor).Background(selBg)
	badgeDimStyle     = lipgloss.NewStyle().Foreground(faint)
	badgeSelStyle     = lipgloss.NewStyle().Bold(true).Foreground(accent)

	footerStatusStyle = lipgloss.NewStyle().Foreground(subtle)
	footerHelpStyle   = lipgloss.NewStyle().Foreground(faint)
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrame(i int) string { return spinFrames[((i % len(spinFrames)) + len(spinFrames)) % len(spinFrames)] }

func paneBox(focused bool, innerW, innerH int) lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(innerW).Height(innerH)
	if focused {
		return s.BorderForeground(accent)
	}
	return s.BorderForeground(borderDim)
}

// --- top-level view ---

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.contentH == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			dimStyle.Render("Terminal too small — resize to at least 40×10."))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderPRPane(), m.renderTreePane())
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
}

func (m model) renderHeader() string {
	left := headerLogo.Render(" TrackPR ") + headerSub.Render("  open pull requests authored by you")
	right := headerPath.Render(prettyPath(m.scanRoot) + " ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return padTo(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) renderFooter() string {
	var status string
	switch {
	case m.loading:
		status = footerStatusStyle.Render(spinnerFrame(m.frame) + " fetching open PRs…")
	case m.err != nil:
		status = errStyle.Render("⚠ error — press r to retry")
	default:
		shown := m.countPRsUnder(m.selPrefix)
		filt := "all repos"
		if m.selPrefix != "" {
			filt = m.selPrefix
		}
		prefix := ""
		if m.refreshing {
			prefix = spinnerFrame(m.frame) + " " // refreshing in the background
		}
		status = footerStatusStyle.Render(
			prefix + fmt.Sprintf("%d open PR%s · filter: %s", shown, plural(shown), filt))
	}
	full := "tab switch · ↑↓ move · ⏎ open/filter · ←→ fold · r refresh · esc clear · q quit"
	short := "↑↓ move · tab switch · ⏎ open · q quit"
	help := footerHelpStyle.Render(full)
	if lipgloss.Width(status)+lipgloss.Width(help)+1 > m.width {
		help = footerHelpStyle.Render(short)
	}
	gap := m.width - lipgloss.Width(status) - lipgloss.Width(help)
	if gap < 1 {
		return padTo(status, m.width)
	}
	return status + strings.Repeat(" ", gap) + help
}

// --- left pane: PR log ---

func (m model) renderPRPane() string {
	w := m.leftW - 2
	title := m.prPaneTitle(w)

	var lines []string
	switch {
	case m.loading:
		lines = centeredLines(dimStyle.Render(spinnerFrame(m.frame)+"  fetching your open PRs…"), w, m.contentH)
	case m.err != nil:
		lines = centeredLines(errStyle.Render("⚠ "+truncate(m.err.Error(), w-4)), w, m.contentH)
	case len(m.rows) == 0:
		msg := "No open PRs in your local repositories."
		if m.selPrefix != "" {
			msg = "No open PRs under " + m.selPrefix
		}
		lines = centeredLines(dimStyle.Render(msg), w, m.contentH)
	default:
		lines = m.prContentLines(w)
	}

	content := title + "\n" + strings.Join(lines, "\n")
	return paneBox(m.focus == focusPRs, w, m.contentH+1).MarginRight(1).Render(content)
}

func (m model) prPaneTitle(w int) string {
	left := paneTitleStyle.Render("Pull Requests")
	var parts []string
	if m.refreshing {
		parts = append(parts, spinnerFrame(m.frame))
	}
	if !m.loading && m.err == nil {
		parts = append(parts, fmt.Sprintf("%d open", m.countPRsUnder(m.selPrefix)))
	}
	right := paneTitleDim.Render(strings.Join(parts, " "))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return padTo(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) prContentLines(w int) []string {
	out := make([]string, 0, m.contentH)
	now := time.Now()
	end := min(m.prOffset+m.contentH, len(m.rows))
	for i := m.prOffset; i < end; i++ {
		r := m.rows[i]
		if r.kind == rowHeader {
			out = append(out, m.renderGroupHeader(r.repo, r.count, w))
		} else {
			out = append(out, m.renderPRLine(r.pr, w, i == m.prCursor && m.focus == focusPRs, now))
		}
	}
	for len(out) < m.contentH {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

func (m model) renderGroupHeader(nwo string, count, w int) string {
	cnt := fmt.Sprintf("%d open", count)
	nameW := w - 2 - lipgloss.Width(cnt) - 1
	if nameW < 4 {
		nameW = 4
	}
	name := truncate(nwo, nameW)
	gap := w - 2 - lipgloss.Width(name) - lipgloss.Width(cnt)
	if gap < 1 {
		gap = 1
	}
	return groupBarStyle.Render("▌ ") + groupNameStyle.Render(name) +
		strings.Repeat(" ", gap) + groupCountStyle.Render(cnt)
}

func (m model) renderPRLine(pr *PR, w int, sel bool, now time.Time) string {
	num := fmt.Sprintf("#%d", pr.Number)
	right := relTime(pr.UpdatedAt, now)
	const indent = 4

	tag := ""
	if pr.IsDraft {
		tag = "draft"
	}
	tagW := 0
	if tag != "" {
		tagW = lipgloss.Width(tag) + 2 // two leading spaces
	}
	titleW := w - indent - lipgloss.Width(num) - 2 - tagW - 1 - lipgloss.Width(right)
	if titleW < 4 {
		tag, tagW = "", 0
		titleW = w - indent - lipgloss.Width(num) - 2 - 1 - lipgloss.Width(right)
		if titleW < 4 {
			titleW = 4
		}
	}
	title := padTo(truncate(pr.Title, titleW), titleW)

	if sel {
		tagPlain := ""
		if tag != "" {
			tagPlain = "  " + tag
		}
		raw := strings.Repeat(" ", indent) + num + "  " + title + tagPlain + " " + right
		return prSelStyle.Render(padTo(raw, w))
	}

	tagSeg := ""
	if tag != "" {
		tagSeg = "  " + draftStyle.Render(tag)
	}
	return strings.Repeat(" ", indent) +
		prNumStyle.Render(num) + "  " +
		prTitleStyle.Render(title) +
		tagSeg + " " +
		prTimeStyle.Render(right)
}

// --- right pane: folder tree ---

func (m model) renderTreePane() string {
	w := m.rightW - 2
	title := m.treePaneTitle(w)

	out := make([]string, 0, m.contentH)
	end := min(m.treeOffset+m.contentH, len(m.visible))
	for i := m.treeOffset; i < end; i++ {
		out = append(out, m.renderTreeLine(m.visible[i], w, i == m.treeCursor && m.focus == focusTree))
	}
	for len(out) < m.contentH {
		out = append(out, strings.Repeat(" ", w))
	}

	content := title + "\n" + strings.Join(out, "\n")
	return paneBox(m.focus == focusTree, w, m.contentH+1).Render(content)
}

func (m model) treePaneTitle(w int) string {
	left := paneTitleStyle.Render("Repositories")
	var right string
	if m.loading {
		right = paneTitleDim.Render(fmt.Sprintf("%d scanned", len(m.repos)))
	} else {
		n := m.reposWithPRs()
		right = paneTitleDim.Render(fmt.Sprintf("%d with PRs", n))
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return padTo(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) renderTreeLine(n *TreeNode, w int, cursor bool) string {
	indent := strings.Repeat("  ", n.Depth)
	glyph := "  "
	switch {
	case n.Depth == 0:
		glyph = "⌂ "
	case len(n.Children) > 0 && n.Expanded:
		glyph = "▾ "
	case len(n.Children) > 0:
		glyph = "▸ "
	case n.IsRepo:
		glyph = "● "
	}

	badge := ""
	if c := m.countPRsUnder(n.Prefix); c > 0 {
		badge = fmt.Sprintf(" %d", c)
	}

	prefixW := lipgloss.Width(indent) + lipgloss.Width(glyph)
	labelW := w - prefixW - lipgloss.Width(badge)
	if labelW < 3 {
		labelW = 3
	}
	label := truncate(n.Label, labelW)
	gap := w - prefixW - lipgloss.Width(label) - lipgloss.Width(badge)
	if gap < 0 {
		gap = 0
	}

	if cursor {
		raw := indent + glyph + label + strings.Repeat(" ", gap) + badge
		return treeCursorStyle.Render(padTo(raw, w))
	}

	gStyle := folderGlyphStyle
	switch {
	case n.Depth == 0:
		gStyle = rootGlyphStyle
	case n.IsRepo && len(n.Children) == 0:
		gStyle = repoGlyphStyle
	}
	labelStyle := treeLabelStyle
	badgeStyle := badgeDimStyle
	if n.Prefix == m.selPrefix {
		labelStyle = treeSelLabelStyle
		badgeStyle = badgeSelStyle
	}
	return indent + gStyle.Render(glyph) + labelStyle.Render(label) +
		strings.Repeat(" ", gap) + badgeStyle.Render(badge)
}

// --- helpers ---

func relTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteString("…")
	return b.String()
}

func padTo(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func center(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	l := (w - sw) / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", w-sw-l)
}

func centeredLines(s string, w, h int) []string {
	lines := make([]string, h)
	mid := h / 2
	for i := range lines {
		if i == mid {
			lines[i] = center(s, w)
		} else {
			lines[i] = strings.Repeat(" ", w)
		}
	}
	return lines
}

func prettyPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if p == home {
			return "~"
		}
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
	}
	return p
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
