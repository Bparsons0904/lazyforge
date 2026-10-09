package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type boxKind int

const (
	boxCRs boxKind = iota
	boxIssues
	boxRuns
)

const (
	focusWeight     = 12 // with unfocusedWeight, the mockup's 2.4:1
	unfocusedWeight = 5
	minBoxHeight    = 3 // border plus one row
)

type boxes struct {
	repo     domain.RepoRef
	showRuns bool
	crs      []domain.ChangeRequest
	issues   []domain.Issue
	runs     []domain.Run
	loaded   [3]bool
	cursor   [3]int
	focus    boxKind
	marked   map[int]bool // CR numbers marked for a bulk merge
}

func (b *boxes) toggleMark(n int) {
	if b.marked[n] {
		delete(b.marked, n)
		return
	}
	if b.marked == nil {
		b.marked = map[int]bool{}
	}
	b.marked[n] = true
}

func (b boxes) count() int {
	if b.showRuns {
		return 3
	}
	return 2
}

func (b boxes) len(k boxKind) int {
	switch k {
	case boxCRs:
		return len(b.crs)
	case boxIssues:
		return len(b.issues)
	default:
		return len(b.runs)
	}
}

// setCursor moves the focused box's cursor to i, stopping at either end.
func (b *boxes) setCursor(i int) {
	b.cursor[b.focus] = max(min(i, b.len(b.focus)-1), 0)
}

func (b *boxes) clampCursors() {
	for k := range b.cursor {
		b.cursor[k] = max(min(b.cursor[k], b.len(boxKind(k))-1), 0)
	}
}

// selected returns the item under the focused box's cursor: a ChangeRequest, Issue or Run, or nil.
func (b boxes) selected() any {
	i := b.cursor[b.focus]
	if i >= b.len(b.focus) {
		return nil
	}
	switch b.focus {
	case boxCRs:
		return b.crs[i]
	case boxIssues:
		return b.issues[i]
	default:
		return b.runs[i]
	}
}

func boxTitle(k boxKind, term string) string {
	switch k {
	case boxCRs:
		return crBoxTitle(term)
	case boxIssues:
		return "Issues"
	default:
		return "Actions"
	}
}

// crBoxTitle follows design.md's box names; an unknown term falls back to its plural.
func crBoxTitle(term string) string {
	switch term {
	case "PR":
		return "Pull requests"
	case "MR":
		return "Merge requests"
	default:
		return term + "s"
	}
}

// view renders the boxes as a w×h column; focus < 0 renders the equal-height preview.
func (b boxes) view(w, h int, focus int, active bool, term string, now time.Time) string {
	n := b.count()
	hs := splitHeights(h, n, focus)
	panes := make([]string, 0, n)
	for i := range n {
		k := boxKind(i)
		focused := i == focus
		accent := style.RepoAccents.Title(i, focused && active)
		title := accent.Render(fmt.Sprintf("[%d] %s", i+1, boxTitle(k, term)))
		if b.loaded[k] {
			title += " " + style.Count.Render(fmt.Sprint(b.len(k)))
		}
		panes = append(panes, frameWith(style.RepoAccents.Border(i, focused && active), title, b.rows(k, w-4, hs[i]-2, focused, now), w, hs[i]))
	}
	return strings.Join(panes, "\n")
}

// rows renders the visible rows of box k, scrolled so the cursor stays on screen.
func (b boxes) rows(k boxKind, w, h int, focused bool, now time.Time) []string {
	switch {
	case !b.loaded[k]:
		return []string{style.Faint.Render("Loading…")}
	case b.len(k) == 0:
		return []string{style.Faint.Render("— none —")}
	}
	// Keep the tag and age columns stable across the entire section.
	tagWidth, ageWidth := 0, 0
	for i := range b.len(k) {
		var tag, elapsed string
		switch k {
		case boxCRs:
			tag = fmt.Sprintf("#%d", b.crs[i].Number)
			elapsed = age(now, b.crs[i].UpdatedAt)
		case boxIssues:
			tag = fmt.Sprintf("#%d", b.issues[i].Number)
			elapsed = age(now, b.issues[i].UpdatedAt)
		default:
			tag = b.runs[i].Workflow
			elapsed = age(now, b.runs[i].StartedAt)
		}
		tagWidth = max(tagWidth, lipgloss.Width(tag))
		ageWidth = max(ageWidth, lipgloss.Width(elapsed))
	}
	// Leave room for the title/branch even when a workflow name is very long.
	tagWidth = min(tagWidth, max((w-ageWidth-4)/2, 0))
	cur := b.cursor[k]
	first := max(cur-h+1, 0)
	var out []string
	for i := first; i < b.len(k) && i < first+max(h, 0); i++ {
		base := lipgloss.NewStyle()
		if focused && i == cur {
			base = style.Selected
		}
		on := func(s lipgloss.Style, t string) string { return s.Inherit(base).Render(t) }
		var tag, label, meta string
		ts := style.RepoAccents.Text(int(k))
		switch k {
		case boxCRs:
			cr := b.crs[i]
			tag = fitLine(truncate(fmt.Sprintf("#%d", cr.Number), tagWidth), tagWidth) + " "
			label = cr.Title
			meta = ciIcon(cr.CI, base) + on(style.Faint, " "+fitLine(age(now, cr.UpdatedAt), ageWidth))
			if b.marked[cr.Number] {
				out = append(out, on(style.Mark, "◆ ")+tagRow(tag, ts, label, style.Text, meta, w-2, base))
				continue
			}
		case boxIssues:
			is := b.issues[i]
			tag = fitLine(truncate(fmt.Sprintf("#%d", is.Number), tagWidth), tagWidth) + " "
			label = is.Title
			m := fitLine(age(now, is.UpdatedAt), ageWidth)
			if is.Comments > 0 {
				m = fmt.Sprintf("%d💬 %s", is.Comments, m)
			}
			meta = on(style.Faint, m)
		default:
			r := b.runs[i]
			tag = fitLine(truncate(r.Workflow, tagWidth), tagWidth) + " "
			meta = on(style.Faint, fitLine(age(now, r.StartedAt), ageWidth))
			out = append(out, ciIcon(r.Status, base)+on(style.Text, " ")+tagRow(tag, ts, r.Branch, style.Text, meta, w-2, base))
			continue
		}
		out = append(out, tagRow(tag, ts, label, style.Text, meta, w, base))
	}
	return out
}

// splitHeights divides total rows among n boxes, giving the focused one the larger share.
// Every box keeps minBoxHeight when total allows it; the heights always sum to total.
func splitHeights(total, n, focus int) []int {
	hs := make([]int, n)
	if n == 0 || total <= 0 {
		return hs
	}
	weights := make([]int, n)
	sum := 0
	for i := range weights {
		weights[i] = 1
		if focus >= 0 {
			weights[i] = unfocusedWeight
			if i == focus {
				weights[i] = focusWeight
			}
		}
		sum += weights[i]
	}
	used := 0
	for i := range hs {
		hs[i] = total * weights[i] / sum
		used += hs[i]
	}
	for i := 0; used < total; i = (i + 1) % n {
		if focus >= 0 {
			i = focus
		}
		hs[i]++
		used++
	}
	if total < minBoxHeight*n {
		return hs
	}
	for i := range hs {
		for hs[i] < minBoxHeight {
			hs[slices.Index(hs, slices.Max(hs))]--
			hs[i]++
		}
	}
	return hs
}
