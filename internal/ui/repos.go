package ui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const renovateRow = "★ Renovate"

const staleAfter = 7 * 24 * time.Hour

type repoList struct {
	repos  []domain.Repo
	loaded bool
	cursor int // 0 is the ★ Renovate row; repo i sits at i+1
}

// selected returns the repo under the cursor; ok is false on the ★ Renovate row.
func (l repoList) selected() (domain.Repo, bool) {
	if l.cursor == 0 || l.cursor > len(l.repos) {
		return domain.Repo{}, false
	}
	return l.repos[l.cursor-1], true
}

// setCursor moves the cursor to i, stopping at either end; it reports whether the cursor moved.
func (l *repoList) setCursor(i int) bool {
	i = max(min(i, len(l.repos)), 0)
	moved := i != l.cursor
	l.cursor = i
	return moved
}

// replace swaps in a fresh repo list, keeping the cursor on the same repo when it's still there.
// It reports whether the selection changed.
func (l *repoList) replace(repos []domain.Repo) bool {
	prev, had := l.selected()
	l.repos, l.loaded = repos, true
	if !had {
		l.cursor = min(l.cursor, len(repos))
		return l.cursor != 0
	}
	for i, r := range repos {
		if r.RepoRef == prev.RepoRef {
			l.cursor = i + 1
			return false
		}
	}
	l.cursor = min(l.cursor, len(repos))
	return true
}

func (l repoList) view(w, h int, svc *core.Service, term string, now time.Time) string {
	if !l.loaded {
		return frame(style.ActiveTitle.Render("Repositories"), []string{style.Faint.Render("Loading…")}, w, h, true)
	}
	title := style.ActiveTitle.Render("Repositories") + " " + style.Count.Render(fmt.Sprint(len(l.repos)))
	// Measure the whole list so scrolling does not move the metadata columns.
	ageWidth, countWidth := 0, 0
	for _, r := range l.repos {
		ageWidth = max(ageWidth, lipgloss.Width(age(now, r.LastActivity)))
		if crs, _, ok := svc.PeekChangeRequests(r.RepoRef); ok && len(crs) > 0 {
			countWidth = max(countWidth, lipgloss.Width(fmt.Sprintf("%d %s", len(crs), term)))
		}
	}
	inner := max(h-2, 0)
	first := max(l.cursor-inner+1, 0)
	var lines []string
	for i := first; i <= len(l.repos) && i < first+inner; i++ {
		base := lipgloss.NewStyle()
		if i == l.cursor {
			base = style.Selected
		}
		if i == 0 {
			lines = append(lines, style.Virtual.Inherit(base).Render(fitLine(renovateRow, w-4)))
			continue
		}
		r := l.repos[i-1]
		count := ""
		meta := style.Faint.Inherit(base).Render(fitLine(age(now, r.LastActivity), ageWidth))
		if crs, _, ok := svc.PeekChangeRequests(r.RepoRef); ok && len(crs) > 0 {
			count = fmt.Sprintf("%d %s", len(crs), term)
		}
		if countWidth > 0 {
			meta = style.RepoOpenPRs.Inherit(base).Render(fitLine(count, countWidth)) +
				style.Faint.Inherit(base).Render("  ") + meta
		}
		name := style.Text
		if now.Sub(r.LastActivity) >= staleAfter {
			name = style.RepoStale
		}
		lines = append(lines, tagRow("", lipgloss.NewStyle(), r.Name, name, meta, w-4, base))
	}
	return frame(title, lines, w, h, true)
}
