package ui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up, Down, Left, Right, NextBox, PrevBox, PrevTab, NextTab,
	Top, Bottom, HalfDown, HalfUp, Jump, Refresh, Help, Quit, Interrupt, Close key.Binding
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
		Jump:      key.NewBinding(key.WithKeys("1", "2", "3", "4", "5"), key.WithHelp("1-5", "jump to box")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		Interrupt: key.NewBinding(key.WithKeys("ctrl+c")),
		Close:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	}
}

func (k keyMap) shortHelp(l level) []key.Binding {
	switch l {
	case levelBoxes:
		return []key.Binding{hint(k.Down, "j/k", "move"), hint(k.Jump, "1-5/tab", "box"), hint(k.Right, "l", "details"), hint(k.Left, "h", "back")}
	case levelDetails:
		return []key.Binding{hint(k.Down, "j/k", "scroll"), hint(k.NextTab, "[ ]", "tabs"), hint(k.Left, "h", "back")}
	default:
		return []key.Binding{hint(k.Down, "j/k", "repo"), hint(k.Right, "l", "enter"), k.Jump, k.Help}
	}
}

func (k keyMap) fullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Down, k.Right, k.Jump, k.NextBox},
		{k.NextTab, k.Top, k.HalfDown},
		{k.Refresh, k.Help, k.Quit, k.Close},
	}
}

func hint(b key.Binding, keys, desc string) key.Binding {
	b.SetHelp(keys, desc)
	return b
}
