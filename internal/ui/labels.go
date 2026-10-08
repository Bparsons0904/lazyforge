package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type labelPicker struct {
	item           forge.ItemRef
	search         textinput.Model
	labels         []domain.Label
	selected       map[int64]bool
	cursor         int
	loaded, saving bool
	err            error
}

type labelsLoadedMsg struct {
	picker              *labelPicker
	available, selected []domain.Label
	err                 error
}

type labelsSavedMsg struct {
	picker *labelPicker
	err    error
}

func (labelsLoadedMsg) fromSession() {}
func (labelsSavedMsg) fromSession()  {}

func (m *Model) openLabels(item forge.ItemRef) tea.Cmd {
	p := &labelPicker{item: item, search: newInput("Filter labels…"), selected: map[int64]bool{}}
	m.labels = p
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		choices, selected, err := svc.Labels(ctx, item)
		return labelsLoadedMsg{p, choices, selected, err}
	}
}

func (p *labelPicker) load(msg labelsLoadedMsg) {
	p.err, p.loaded = msg.err, msg.err == nil
	if msg.err != nil {
		return
	}
	p.labels = slices.Clone(msg.available)
	for _, l := range msg.selected {
		p.selected[l.ID] = true
		if !slices.ContainsFunc(p.labels, func(c domain.Label) bool { return c.ID == l.ID }) {
			p.labels = append(p.labels, l)
		}
	}
	slices.SortStableFunc(p.labels, func(a, b domain.Label) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
}

func (p *labelPicker) filtered() []domain.Label {
	var out []domain.Label
	for _, l := range p.labels {
		if strings.Contains(strings.ToLower(l.Name), strings.ToLower(p.search.Value())) {
			out = append(out, l)
		}
	}
	return out
}

func (m *Model) labelsKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.labels
	if msg.String() == "esc" && !p.saving {
		m.labels = nil
		return nil
	}
	if !p.loaded || p.saving {
		return nil
	}
	choices := p.filtered()
	switch msg.String() {
	case "up":
		p.cursor = max(p.cursor-1, 0)
	case "down":
		p.cursor = min(p.cursor+1, max(len(choices)-1, 0))
	case "space":
		if len(choices) > 0 {
			id := choices[p.cursor].ID
			p.selected[id] = !p.selected[id]
		}
	case "enter":
		ids := make([]int64, 0)
		for _, l := range p.labels {
			if p.selected[l.ID] {
				ids = append(ids, l.ID)
			}
		}
		p.saving = true
		p.err = nil
		svc, ctx, item := m.svc, m.ctx, p.item
		return func() tea.Msg { return labelsSavedMsg{p, svc.SetLabels(ctx, item, ids)} }
	default:
		var cmd tea.Cmd
		p.search, cmd = p.search.Update(msg)
		p.cursor = 0
		return cmd
	}
	return nil
}

func (p *labelPicker) view(w, h int) string {
	bw := min(64, w)
	p.search.SetWidth(max(bw-6, 1))
	lines := []string{p.search.View(), ""}
	switch {
	case p.saving:
		lines = append(lines, style.Faint.Render("Saving…"))
	case !p.loaded && p.err == nil:
		lines = append(lines, style.Faint.Render("Loading labels…"))
	case p.loaded:
		choices := p.filtered()
		if len(choices) == 0 {
			lines = append(lines, style.Faint.Render("No matching labels"))
		}
		capacity := max(h-9, 1)
		first := max(p.cursor-capacity+1, 0)
		for i := first; i < len(choices) && i < first+capacity; i++ {
			l := choices[i]
			marker := "[ ] "
			if p.selected[l.ID] {
				marker = "[x] "
			}
			base := lipgloss.NewStyle()
			if i == p.cursor {
				base = style.Selected
			}
			lines = append(lines, style.Text.Inherit(base).Render(marker)+style.Label(l.Name, l.Color).Inherit(base).Render(fitLine(truncate(l.Name, bw-8), bw-8)))
		}
	}
	if p.err != nil {
		lines = append(lines, style.StatusErr.Render(truncate(p.err.Error(), bw-4)))
	}
	lines = append(lines, "", style.Faint.Render("↑/↓ move · space toggle · enter save · esc cancel"))
	box := frame(style.ActiveTitle.Render(fmt.Sprintf("Labels · %s #%d", p.item.Repo, p.item.Number)), lines, bw, min(len(lines)+2, h), true)
	return fitLines(strings.Split(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box), "\n"), w, h)
}
