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

const boxRepo boxKind = 5 // number = kind+1; 3 and 4 reserved for Renovate and Releases

const (
	focusWeight     = 12 // with unfocusedWeight, the mockup's 2.4:1
	unfocusedWeight = 5
	minBoxHeight    = 3 // border plus one row
)

// readmeState is the selected repo's README; ok is false until the first load lands.
type readmeState struct {
	ok bool
	r  domain.Readme
}

// branchesState is the selected repo's branch list; ok is false until the first load lands.
// commits holds each branch's recent commits; a branch missing from it is still loading.
type branchesState struct {
	ok      bool
	list    []domain.Branch
	commits map[string][]domain.Commit
}

func (b *branchesState) setCommits(branch string, cs []domain.Commit) {
	if b.commits == nil {
		b.commits = map[string][]domain.Commit{}
	}
	b.commits[branch] = cs
}

type boxes struct {
	repo         domain.RepoRef
	repoRow      domain.Repo
	showRuns     bool
	showRepo     bool
	showBranches bool
	showFiles    bool
	crs          []domain.ChangeRequest
	issues       []domain.Issue
	runs         []domain.Run
	readme       readmeState
	branches     branchesState
	files        filesState
	loaded       [boxRepo + 1]bool
	cursor       [boxRepo + 1]int
	focus        boxKind
	marked       map[int]bool // CR numbers marked for a bulk merge
}

// kinds lists the visible boxes in display order.
func (b boxes) kinds() []boxKind {
	ks := []boxKind{boxCRs, boxIssues}
	if b.showRuns {
		ks = append(ks, boxRuns)
	}
	if b.showRepo {
		ks = append(ks, boxRepo)
	}
	return ks
}

// count is the number of visible boxes.
func (b boxes) count() int { return len(b.kinds()) }

// step returns the visible box after (delta 1) or before (delta -1) focus, wrapping at either end.
func (b *boxes) step(delta int) boxKind {
	ks := b.kinds()
	i := max(slices.Index(ks, b.focus), 0)
	return ks[(i+delta+len(ks))%len(ks)]
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

func (b boxes) len(k boxKind) int {
	switch k {
	case boxCRs:
		return len(b.crs)
	case boxIssues:
		return len(b.issues)
	case boxRepo:
		if b.showRepo {
			return 1
		}
		return 0
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

// selected returns the item under the focused box's cursor: a ChangeRequest, Issue, Run or the repo, or nil.
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
	case boxRepo:
		return b.repoRow
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
	case boxRepo:
		return "Repo"
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
	ks := b.kinds()
	hs := splitHeights(h, len(ks), slices.Index(ks, boxKind(focus)))
	panes := make([]string, 0, len(ks))
	for i, k := range ks {
		focused := k == boxKind(focus)
		accent := style.RepoAccents.Title(int(k), focused && active)
		title := accent.Render(fmt.Sprintf("[%d] %s", int(k)+1, boxTitle(k, term)))
		if b.loaded[k] && k != boxRepo {
			title += " " + style.Count.Render(fmt.Sprint(b.len(k)))
		}
		panes = append(panes, frameWith(style.RepoAccents.Border(int(k), focused && active), title, b.rows(k, w-4, hs[i]-2, focused, now), w, hs[i]))
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
	case k == boxRepo:
		base := lipgloss.NewStyle()
		if focused {
			base = style.Selected
		}
		return []string{tagRow(b.repoRow.String()+" ", style.RepoAccents.Text(int(k)), b.repoRow.Description, style.Faint, "", w, base)}
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
