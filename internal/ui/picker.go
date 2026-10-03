package ui

import (
	"maps"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const addHostRow = "+ Add host"

// picker is level 0; its rows are the configured hosts by name, then addHostRow.
type picker struct {
	cursor   int
	pendingG bool
}

type connectedMsg struct {
	name string
	f    forge.Forge
	err  error
}

func hostNames(c config.Config) []string { return slices.Sorted(maps.Keys(c.Hosts)) }

func newPicker(c config.Config, last string) picker {
	names := hostNames(c)
	for _, want := range []string{last, c.DefaultHost} {
		if i := slices.Index(names, want); want != "" && i >= 0 {
			return picker{cursor: i}
		}
	}
	return picker{}
}

func (a *App) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	k, p, names := a.keys, &a.picker, hostNames(a.cfg())
	p.cursor = min(p.cursor, len(names))
	gg, swallowed := gPrefix(&p.pendingG, msg, k)
	if swallowed {
		return nil
	}
	switch {
	case key.Matches(msg, k.Down):
		p.cursor = min(p.cursor+1, len(names))
	case key.Matches(msg, k.Up):
		p.cursor = max(p.cursor-1, 0)
	case gg:
		p.cursor = 0
	case key.Matches(msg, k.Bottom):
		p.cursor = len(names)
	case key.Matches(msg, k.Settings):
		a.openSettings()
	case key.Matches(msg, k.Quit):
		return tea.Quit
	case key.Matches(msg, k.Right):
		if p.cursor == len(names) {
			a.openOnboarding(a.ctx, onboardStart{})
			return nil
		}
		return a.pick(names[p.cursor])
	}
	return nil
}

// pick returns to the live session for its own host and connects to any other.
// Either way the latest pick supersedes an earlier connect still in flight.
func (a *App) pick(name string) tea.Cmd {
	if name == a.host {
		a.connecting = ""
		a.status = ""
		a.screen = screenSession
		return nil
	}
	a.connecting = name
	a.setInfo("Connecting to " + name + "…")
	ctx, h, connect := a.ctx, a.cfg().Hosts[name], a.deps.Connect
	return func() tea.Msg {
		f, err := connect(ctx, h)
		return connectedMsg{name: name, f: f, err: err}
	}
}

func (a App) pickerView(w, h int) string {
	c := a.cfg()
	names := append(hostNames(c), addHostRow)
	cursor := min(a.picker.cursor, len(names)-1)
	inner := max(h-2, 0)
	first := max(cursor-inner+1, 0)
	var lines []string
	for i := first; i < len(names) && i < first+inner; i++ {
		base := lipgloss.NewStyle()
		if i == cursor {
			base = style.Selected
		}
		if i == len(names)-1 {
			lines = append(lines, addHostLine(w-4, base))
			continue
		}
		lines = append(lines, row(names[i], style.Faint.Inherit(base).Render(hostMeta(c, names[i])), w-4, base))
	}
	return frame(style.ActiveTitle.Render("Hosts"), lines, w, h, true)
}

func addHostLine(w int, base lipgloss.Style) string {
	return style.Virtual.Inherit(base).Render(fitLine(addHostRow, w))
}

func hostMeta(c config.Config, name string) string {
	meta := c.Hosts[name].URL
	if name == c.DefaultHost {
		meta += " (default)"
	}
	return meta
}

func (k keyMap) pickerHints() []key.Binding {
	return []key.Binding{hint(k.Down, "j/k", "move"), hint(k.Right, "l/enter", "connect"), k.Settings, k.Quit}
}
