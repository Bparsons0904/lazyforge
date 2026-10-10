package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type keyMap struct {
	Up, Down, Left, Right, NextBox, PrevBox, PrevTab, NextTab,
	Top, Bottom, HalfDown, HalfUp, Jump, Refresh, Help, Quit, Interrupt, Close,
	Merge, Approve, CloseItem, Comment, Labels, Open, Rerun, Mark, Confirm, Settings, Enter key.Binding
}

// defaultKeys gives help text only to the first binding of each pair, so the overlay lists the pair once.
func defaultKeys() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("k", "up")),
		Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "move")),
		Left:      key.NewBinding(key.WithKeys("h", "left")),
		Right:     key.NewBinding(key.WithKeys("l", "right", "enter"), key.WithHelp("h/l", "back/in")),
		NextBox:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab/S-tab", "box")),
		PrevBox:   key.NewBinding(key.WithKeys("shift+tab")),
		PrevTab:   key.NewBinding(key.WithKeys("[")),
		NextTab:   key.NewBinding(key.WithKeys("]"), key.WithHelp("[ ]", "tabs")),
		Top:       key.NewBinding(key.WithKeys("g"), key.WithHelp("gg/G", "top/bottom")),
		Bottom:    key.NewBinding(key.WithKeys("G")),
		HalfDown:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^d/^u", "half page")),
		HalfUp:    key.NewBinding(key.WithKeys("ctrl+u")),
		Jump:      key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6"), key.WithHelp("1-6", "jump to box")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c")),
		Close:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "dismiss")),
		Merge:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "merge")),
		Approve:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		CloseItem: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "close")),
		Labels:    key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "labels")),
		Comment:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "comment")),
		Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open")),
		Rerun:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "run page")),
		Mark:      key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark")),
		Confirm:   key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm")),
		Settings:  key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "settings")),
		Enter:     key.NewBinding(key.WithKeys("enter")), // enter alone, so text inputs still receive l and y
	}
}

func (k keyMap) shortHelp(l level) []key.Binding {
	switch l {
	case levelBoxes:
		return append([]key.Binding{hint(k.Down, "j/k", "move"), hint(k.Jump, "1-6/tab", "box"), hint(k.Right, "l", "details"), hint(k.Left, "h", "back")}, k.actions()...)
	case levelDetails:
		return append([]key.Binding{hint(k.Down, "j/k", "scroll"), hint(k.NextTab, "[ ]", "tabs"), hint(k.Left, "h", "back")}, k.actions()...)
	default:
		bs := []key.Binding{hint(k.Down, "j/k", "repo"), hint(k.Right, "l", "enter"), k.Jump, k.Open, k.Help}
		if k.hosted() {
			bs = append(bs, hint(k.Left, "h", "hosts"), k.Settings)
		}
		return bs
	}
}

func (k keyMap) fullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Down, k.Right, k.Jump, k.NextBox},
		{k.NextTab, k.Top, k.HalfDown},
		{k.Refresh, k.Settings, k.Help, k.Quit, k.Close},
		k.actions(),
	}
}

func (k keyMap) actions() []key.Binding {
	return []key.Binding{k.Merge, k.Mark, k.Approve, k.CloseItem, k.Comment, k.Open, k.Rerun, k.Labels}
}

// gPrefix runs the gg prefix: the first g is consumed (swallowed), a g right after it reports gg,
// and any other key clears the pending state.
func gPrefix(pending *bool, msg tea.KeyPressMsg, k keyMap) (gg, swallowed bool) {
	isG := key.Matches(msg, k.Top)
	if isG && !*pending {
		*pending = true
		return false, true
	}
	gg = *pending && isG
	*pending = false
	return gg, false
}

// hosted reports whether an App hosts this session, which is what enables the S and hosts keys.
func (k keyMap) hosted() bool { return k.Settings.Enabled() }

func (k *keyMap) setHosted(b bool) { k.Settings.SetEnabled(b) }

func hint(b key.Binding, keys, desc string) key.Binding {
	b.SetHelp(keys, desc)
	return b
}
