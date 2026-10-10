package ui

import (
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type settingsView struct {
	cfg    config.Config
	active string           // session host; "" when none
	repos  []domain.RepoRef // active host's repos; nil when unknown
}

type intentKind int

const (
	intentNone intentKind = iota
	intentChange
	intentAdd
	intentEdit
	intentClose
)

type settingsIntent struct {
	kind intentKind
	cfg  config.Config // intentChange: the new config, already cloned
	host string        // intentEdit: host name
}

type settingsMode int

const (
	modeList settingsMode = iota
	modeMenu
	modeConfirm
	modeInput
)

var hostMenu = []string{"Edit", "Set as default", "Remove"}

type settingsRowKind int

const (
	rowHost settingsRowKind = iota
	rowAdd
	rowUpdate
	rowSplash
	rowImages
	rowHostRenovate
	rowHostWorkflow
	rowHostCI
	rowRepoCI
)

type settingsRow struct {
	kind       settingsRowKind
	host, repo string
}

// settings is the Settings screen; it never holds config, only cursor state, so every render reads the live one.
type settings struct {
	cursor, menuCur int
	mode            settingsMode
	pendingG        bool
	msg             string
	input           textinput.Model
	editing         settingsRow
}

func settingsRows(v settingsView) []settingsRow {
	names := hostNames(v.cfg)
	var rows []settingsRow
	for _, n := range names {
		rows = append(rows, settingsRow{kind: rowHost, host: n})
	}
	rows = append(rows, settingsRow{kind: rowAdd}, settingsRow{kind: rowUpdate}, settingsRow{kind: rowSplash}, settingsRow{kind: rowImages})
	for _, n := range names {
		rows = append(rows, settingsRow{kind: rowHostRenovate, host: n}, settingsRow{kind: rowHostWorkflow, host: n})
	}
	for _, n := range names {
		rows = append(rows, settingsRow{kind: rowHostCI, host: n})
		repos := slices.Sorted(maps.Keys(v.cfg.Hosts[n].Repos))
		if n == v.active {
			for _, r := range v.repos {
				repos = append(repos, r.String())
			}
			slices.Sort(repos)
			repos = slices.Compact(repos)
		}
		for _, r := range repos {
			rows = append(rows, settingsRow{kind: rowRepoCI, host: n, repo: r})
		}
	}
	return rows
}

func change(c config.Config) settingsIntent { return settingsIntent{kind: intentChange, cfg: c} }

func toggleUpdateCheck(c config.Config) config.Config {
	c = c.Clone()
	c.Update.Check = !c.Update.Check
	return c
}

func toggleSplash(c config.Config) config.Config {
	c = c.Clone()
	c.Splash.Show = !c.Splash.Show
	return c
}

func toggleImages(c config.Config) config.Config {
	c = c.Clone()
	c.Images.Show = !c.Images.Show
	return c
}

func toggleHostCI(c config.Config, host string) config.Config {
	c = c.Clone()
	h := c.Hosts[host]
	h.RequireGreenCI = !h.RequireGreenCI
	c.Hosts[host] = h
	return c
}

// cycleRepoCI steps a repo override inherit → on → off → inherit and drops the key at inherit.
func cycleRepoCI(c config.Config, host, repo string) config.Config {
	c = c.Clone()
	h := c.Hosts[host]
	cur := h.Repos[repo].RequireGreenCI
	switch {
	case cur == nil:
		if h.Repos == nil {
			h.Repos = map[string]config.RepoSettings{}
		}
		on := true
		h.Repos[repo] = config.RepoSettings{RequireGreenCI: &on}
	case *cur:
		off := false
		h.Repos[repo] = config.RepoSettings{RequireGreenCI: &off}
	default:
		delete(h.Repos, repo)
		if len(h.Repos) == 0 {
			h.Repos = nil
		}
	}
	c.Hosts[host] = h
	return c
}

func setDefaultHost(c config.Config, host string) config.Config {
	c = c.Clone()
	c.DefaultHost = host
	return c
}

func removeHost(c config.Config, host string) config.Config {
	c = c.Clone()
	delete(c.Hosts, host)
	if c.DefaultHost == host {
		c.DefaultHost = ""
	}
	return c
}

func (s settings) update(msg tea.KeyPressMsg, v settingsView, k keyMap) (settings, settingsIntent) {
	rows := settingsRows(v)
	s.cursor = max(min(s.cursor, len(rows)-1), 0)
	s.msg = ""
	cur := rows[s.cursor]
	switch s.mode {
	case modeMenu:
		return s.menuKey(msg, cur.host, v, k)
	case modeInput:
		return s.inputKey(msg, cur, v, k)
	case modeConfirm:
		switch {
		case key.Matches(msg, k.Confirm):
			s.mode = modeList
			// A picker connect can make this host active while the confirm is open.
			if cur.host == v.active {
				s.msg = "Switch to another host first"
				return s, settingsIntent{}
			}
			return s, change(removeHost(v.cfg, cur.host))
		case key.Matches(msg, k.Close):
			s.mode = modeList
		}
		return s, settingsIntent{}
	}

	gg, swallowed := gPrefix(&s.pendingG, msg, k)
	if swallowed {
		return s, settingsIntent{}
	}
	switch {
	case key.Matches(msg, k.Down):
		s.cursor = min(s.cursor+1, len(rows)-1)
	case key.Matches(msg, k.Up):
		s.cursor = max(s.cursor-1, 0)
	case gg:
		s.cursor = 0
	case key.Matches(msg, k.Bottom):
		s.cursor = len(rows) - 1
	case key.Matches(msg, k.Close, k.Left):
		return s, settingsIntent{kind: intentClose}
	case key.Matches(msg, k.Right, k.Mark):
		return s.activate(msg, cur, v, k)
	}
	return s, settingsIntent{}
}

// activate handles l/enter/space on a row; space doesn't open a host menu or the add flow.
func (s settings) activate(msg tea.KeyPressMsg, r settingsRow, v settingsView, k keyMap) (settings, settingsIntent) {
	toggle := key.Matches(msg, k.Enter, k.Mark)
	switch r.kind {
	case rowHost:
		if !key.Matches(msg, k.Mark) {
			s.mode, s.menuCur = modeMenu, 0
		}
	case rowAdd:
		if !key.Matches(msg, k.Mark) {
			return s, settingsIntent{kind: intentAdd}
		}
	case rowUpdate:
		if toggle {
			return s, change(toggleUpdateCheck(v.cfg))
		}
	case rowSplash:
		if toggle {
			return s, change(toggleSplash(v.cfg))
		}
	case rowImages:
		if toggle {
			return s, change(toggleImages(v.cfg))
		}
	case rowHostRenovate, rowHostWorkflow:
		if !key.Matches(msg, k.Mark) {
			return s.startEdit(r, v), settingsIntent{}
		}
	case rowHostCI:
		if toggle {
			return s, change(toggleHostCI(v.cfg, r.host))
		}
	case rowRepoCI:
		if toggle {
			return s, change(cycleRepoCI(v.cfg, r.host, r.repo))
		}
	}
	return s, settingsIntent{}
}

func (s settings) startEdit(r settingsRow, v settingsView) settings {
	h := v.cfg.Hosts[r.host]
	if r.kind == rowHostWorkflow {
		s.input = newInput("deadstyle/forgejo/renovate.yml")
		s.input.SetValue(h.RenovateWorkflow)
	} else {
		s.input = newInput("renovate-bot")
		s.input.SetValue(h.RenovateUser)
	}
	s.mode, s.editing = modeInput, r
	return s
}

func (s settings) inputKey(msg tea.KeyPressMsg, r settingsRow, v settingsView, k keyMap) (settings, settingsIntent) {
	switch {
	case key.Matches(msg, k.Close):
		s.mode = modeList
	case key.Matches(msg, k.Enter):
		return s.commit(r, v)
	default:
		s.input, _ = s.input.Update(msg)
	}
	return s, settingsIntent{}
}

// commit refuses a malformed workflow: the field stays open with the format hint.
func (s settings) commit(r settingsRow, v settingsView) (settings, settingsIntent) {
	val := strings.TrimSpace(s.input.Value())
	h := v.cfg.Hosts[r.host]
	if r.kind == rowHostWorkflow {
		if _, _, _, err := config.SplitWorkflow(val); val != "" && err != nil {
			s.msg = workflowFormatMsg
			return s, settingsIntent{}
		}
		h.RenovateWorkflow = val
	} else {
		if r.host == v.active && val != h.RenovateUser {
			s.msg = "The bot username applies the next time you connect"
		}
		h.RenovateUser = val
	}
	s.mode = modeList
	c := v.cfg.Clone()
	c.Hosts[r.host] = h
	return s, change(c)
}

func (s settings) menuKey(msg tea.KeyPressMsg, host string, v settingsView, k keyMap) (settings, settingsIntent) {
	switch {
	case key.Matches(msg, k.Down):
		s.menuCur = min(s.menuCur+1, len(hostMenu)-1)
	case key.Matches(msg, k.Up):
		s.menuCur = max(s.menuCur-1, 0)
	case key.Matches(msg, k.Close):
		s.mode = modeList
	case key.Matches(msg, k.Right):
		s.mode = modeList
		switch s.menuCur {
		case 0:
			return s, settingsIntent{kind: intentEdit, host: host}
		case 1:
			if v.cfg.DefaultHost != host {
				return s, change(setDefaultHost(v.cfg, host))
			}
		case 2:
			if host == v.active {
				s.msg = "Switch to another host first"
			} else {
				s.mode = modeConfirm
			}
		}
	}
	return s, settingsIntent{}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (s settings) view(w, h int, v settingsView) string {
	rows := settingsRows(v)
	cursor := max(min(s.cursor, len(rows)-1), 0)
	inner := max(h-2, 0)
	if s.msg != "" {
		inner--
	}

	var lines []string
	cursorLine := 0
	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, style.ActiveTitle.Render(title))
	}
	for i, r := range rows {
		switch {
		case r.kind == rowHost && i == 0:
			section("Hosts")
		case r.kind == rowUpdate:
			section("Updates")
		case r.kind == rowSplash:
			section("Splash screen")
		case r.kind == rowImages:
			section("Images")
		case r.kind == rowHostRenovate && rows[i-1].kind == rowImages:
			section("Renovate")
		case r.kind == rowHostCI && (i == 0 || rows[i-1].kind == rowImages || rows[i-1].kind == rowHostWorkflow):
			section("Merging")
		}
		base := lipgloss.NewStyle()
		if i == cursor {
			base = style.Selected
			cursorLine = len(lines)
		}
		lines = append(lines, s.rowLine(r, v, w-4, base))
		if i == cursor && r.kind == rowHost {
			lines = append(lines, s.inlineLines(r.host)...)
		}
	}

	first := max(min(cursorLine-inner+1, len(lines)-inner), 0)
	body := lines[first:min(first+inner, len(lines))]
	if s.msg != "" {
		body = append(slices.Clone(body), style.Faint.Render(s.msg))
	}
	return frame(style.ActiveTitle.Render("Settings"), body, w, h, true)
}

func (s settings) rowLine(r settingsRow, v settingsView, w int, base lipgloss.Style) string {
	faint := style.Faint.Inherit(base)
	switch r.kind {
	case rowHost:
		meta := hostMeta(v.cfg, r.host)
		if r.host == v.active {
			meta += " (connected)"
		}
		return row(r.host, faint.Render(meta), w, base)
	case rowAdd:
		return addHostLine(w, base)
	case rowUpdate:
		return row("Check for updates at startup", faint.Render(onOff(v.cfg.Update.Check)), w, base)
	case rowSplash:
		return row("Show the splash screen at startup", faint.Render(onOff(v.cfg.Splash.Show)), w, base)
	case rowImages:
		return row("Show images", faint.Render(onOff(v.cfg.Images.Show)), w, base)
	case rowHostRenovate, rowHostWorkflow:
		return s.renovateRow(r, v, w, base)
	case rowHostCI:
		return row(r.host+": Only merge when CI is green", faint.Render(onOff(v.cfg.Hosts[r.host].RequireGreenCI)), w, base)
	default:
		state := "inherit"
		if p := v.cfg.Hosts[r.host].Repos[r.repo].RequireGreenCI; p != nil {
			state = onOff(*p)
		}
		return row("  "+r.repo, faint.Render(state), w, base)
	}
}

func (s settings) renovateRow(r settingsRow, v settingsView, w int, base lipgloss.Style) string {
	h := v.cfg.Hosts[r.host]
	label, val, unset := r.host+": Renovate bot", h.RenovateUser, "not set"
	if r.kind == rowHostWorkflow {
		label, val, unset = r.host+": Renovate workflow (N)", h.RenovateWorkflow, "not set (N hidden)"
	}
	if s.mode == modeInput && s.editing == r {
		return row(label, s.input.View(), w, base)
	}
	if val == "" {
		val = unset
	}
	return row(label, style.Faint.Inherit(base).Render(val), w, base)
}

func (s settings) inlineLines(host string) []string {
	switch s.mode {
	case modeMenu:
		var out []string
		for i, item := range hostMenu {
			st := style.Text
			if i == s.menuCur {
				st = style.Selected
			}
			out = append(out, "    "+st.Render(item))
		}
		return out
	case modeConfirm:
		return []string{style.Faint.Render("    Remove " + host + "? y/enter confirm · esc cancel")}
	}
	return nil
}

func (s settings) hints(k keyMap) []key.Binding {
	switch s.mode {
	case modeMenu:
		return []key.Binding{hint(k.Down, "j/k", "move"), hint(k.Enter, "enter", "select"), hint(k.Close, "esc", "back")}
	case modeConfirm:
		return []key.Binding{k.Confirm, hint(k.Close, "esc", "cancel")}
	case modeInput:
		return []key.Binding{hint(k.Enter, "enter", "save"), hint(k.Close, "esc", "cancel")}
	}
	return []key.Binding{hint(k.Down, "j/k", "move"), hint(k.Right, "l/enter", "open"), hint(k.Mark, "space", "toggle"), hint(k.Close, "esc", "close")}
}
