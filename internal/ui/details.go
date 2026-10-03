package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type details struct {
	vp    viewport.Model
	tab   int
	shown string // the item and tab on screen; a change scrolls back to the top
}

// tabs lists the details tabs that have content for item; later tickets append to it.
func tabs(any) []string { return []string{"Overview"} }

// cycleTab moves delta tabs along, wrapping at either end.
func (d *details) cycleTab(item any, delta int) {
	n := len(tabs(item))
	d.tab = ((d.tab+delta)%n + n) % n
}

func (d *details) sync(item any, repo domain.RepoRef, w, h int, now time.Time) {
	d.syncText(fmt.Sprintf("%v %s", repo, itemID(item)), overview(item, repo, now), w, h)
}

// syncText shows text in the pane; a changed id scrolls back to the top.
func (d *details) syncText(id, text string, w, h int) {
	d.tab = min(d.tab, len(tabs(nil))-1)
	cw, ch := max(w-4, 1), max(h-2, 0)
	d.vp.SetWidth(cw)
	d.vp.SetHeight(ch)
	d.vp.SetContent(lipgloss.NewStyle().Width(cw).Render(text))
	if id = fmt.Sprintf("%s %d", id, d.tab); id != d.shown {
		d.vp.GotoTop()
		d.shown = id
	}
}

func (d details) view(item any, w, h int, active bool) string {
	names := tabs(item)
	parts := make([]string, len(names))
	for i, t := range names {
		if i == d.tab {
			parts[i] = style.CurrentTab.Render(t)
		} else {
			parts[i] = style.Tab.Render(t)
		}
	}
	title := strings.Join(parts, style.Tab.Render(" │ "))
	return frame(title, strings.Split(d.vp.View(), "\n"), w, h, active)
}

func itemID(item any) string {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return fmt.Sprintf("cr %d", it.Number)
	case domain.Issue:
		return fmt.Sprintf("issue %d", it.Number)
	case domain.Run:
		return fmt.Sprintf("run %d", it.ID)
	default:
		return ""
	}
}

// itemCrumb is the breadcrumb's last segment for item, or "" when nothing is selected.
func itemCrumb(item any) string {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return fmt.Sprintf("#%d", it.Number)
	case domain.Issue:
		return fmt.Sprintf("#%d", it.Number)
	case domain.Run:
		return fmt.Sprintf("%s #%d", it.Workflow, it.Number)
	default:
		return ""
	}
}

func overview(item any, repo domain.RepoRef, now time.Time) string {
	var lines []string
	switch it := item.(type) {
	case domain.ChangeRequest:
		ci := ciIcon(it.CI, lipgloss.NewStyle()) + " " + style.Text.Render(ciText(it.CI))
		if len(it.Labels) > 0 {
			ci += style.Faint.Render(" · " + strings.Join(it.Labels, " "))
		}
		lines = []string{
			style.Heading.Render(it.Title),
			style.Faint.Render(fmt.Sprintf("%s #%d · %s · %s ago", repo, it.Number, it.Author, age(now, it.UpdatedAt))),
			ci,
			style.Faint.Render(it.SourceBranch + " → " + it.TargetBranch),
			"",
			style.Text.Render(it.Body),
		}
	case domain.Issue:
		lines = []string{
			style.Heading.Render(it.Title),
			style.Faint.Render(fmt.Sprintf("%s #%d · %s · %s ago", repo, it.Number, it.Author, age(now, it.UpdatedAt))),
		}
		if len(it.Labels) > 0 {
			lines = append(lines, style.Faint.Render(strings.Join(it.Labels, " ")))
		}
		lines = append(lines, "", style.Text.Render(it.Body))
	case domain.Run:
		commit := it.Commit[:min(7, len(it.Commit))]
		lines = []string{
			ciIcon(it.Status, lipgloss.NewStyle()) + " " + style.Heading.Render(fmt.Sprintf("%s #%d", it.Workflow, it.Number)),
			style.Faint.Render(fmt.Sprintf("%s · %s · %s @ %s", repo, it.Event, it.Branch, commit)),
			style.Faint.Render(fmt.Sprintf("%s ago · took %s", age(now, it.StartedAt), it.Duration.Round(time.Second))),
		}
	default:
		lines = []string{style.Faint.Render("Nothing here.")}
	}
	return strings.Join(lines, "\n")
}
