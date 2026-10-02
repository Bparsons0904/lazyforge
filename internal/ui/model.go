// Package ui holds the Bubble Tea models; it reaches the forge only through core.
package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type level int

const (
	levelRepos level = iota
	levelBoxes
	levelDetails
)

const leftPercent = 38

// Model is the root model for one session.
type Model struct {
	ctx    context.Context
	svc    *core.Service
	info   forge.HostInfo
	keys   keyMap
	help   help.Model
	now    func() time.Time
	tick   func() tea.Cmd  // overridable so tests skip the five-minute wait
	selCtx context.Context // bounds the selected repo's fetches; cancel ends them
	cancel context.CancelFunc

	openURL     func(string) error // overridable so tests never launch a browser
	dialog      *dialog
	mergeCancel context.CancelFunc // stops a running merge from starting more targets

	width, height int
	level         level
	showHelp      bool
	pendingG      bool
	status        string
	statusErr     bool

	repos   repoList
	boxes   boxes
	details details
}

// New returns the root model for one session over svc; ctx bounds every fetch the UI makes.
func New(ctx context.Context, svc *core.Service) Model {
	h := help.New()
	h.ShortSeparator = " · "
	h.Styles.ShortKey, h.Styles.ShortDesc, h.Styles.ShortSeparator = style.HintKey, style.HintText, style.HintText
	h.Styles.FullKey, h.Styles.FullDesc, h.Styles.FullSeparator = style.HelpKey, style.Text, style.HintText
	h.Styles.Ellipsis = style.HintText
	m := Model{ctx: ctx, svc: svc, info: svc.Info(), keys: defaultKeys(), help: h, now: time.Now, tick: tickEvery}
	m.openURL = func(u string) error { return openBrowser(ctx, u) }
	m.syncKeys()
	return m
}

// Init loads the repo list and starts the five-minute refresh tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadRepos(m.ctx, m.svc), m.tick())
}

// Update applies msg; it never blocks, and all I/O happens in the returned command.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmd = m.handleKey(msg)
	case refreshTickMsg:
		cmd = tea.Batch(m.refresh(), m.tick())
	case reposLoadedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			break
		}
		if m.repos.replace(msg.repos) {
			cmd = m.selectRepo(true)
		}
	case changeRequestsLoadedMsg:
		if !m.loadFailed(msg.key, msg.err) {
			m.boxes.crs, m.boxes.loaded[boxCRs] = msg.items, true
		}
	case issuesLoadedMsg:
		if !m.loadFailed(msg.key, msg.err) {
			m.boxes.issues, m.boxes.loaded[boxIssues] = msg.items, true
		}
	case runsLoadedMsg:
		if !m.loadFailed(msg.key, msg.err) {
			m.boxes.runs, m.boxes.loaded[boxRuns] = msg.items, true
		}
	case recheckedMsg:
		cmd = m.rechecked(msg)
	case mergeDoneMsg:
		cmd = m.mergeDone(msg)
	case actionDoneMsg:
		cmd = m.actionDone(msg)
	case editorDoneMsg:
		cmd = m.postComment(msg)
	}
	m.boxes.clampCursors()
	m.syncDetails()
	m.syncKeys()
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	m.status = ""
	if key.Matches(msg, k.Interrupt) {
		return tea.Quit
	}
	if m.dialog != nil {
		return m.dialogKey(msg)
	}
	if m.showHelp {
		if key.Matches(msg, k.Help, k.Close, k.Quit) {
			m.showHelp = false
		}
		return nil
	}
	if key.Matches(msg, k.Top) && !m.pendingG {
		m.pendingG = true
		return nil
	}
	gg := m.pendingG && key.Matches(msg, k.Top)
	m.pendingG = false
	switch {
	case key.Matches(msg, k.Quit):
		return tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = true
		return nil
	case key.Matches(msg, k.Refresh):
		return m.refresh()
	}
	if m.level != levelRepos {
		if cmd, ok := m.actionKey(msg); ok {
			return cmd
		}
	}
	switch m.level {
	case levelBoxes:
		m.boxesKey(msg, gg)
	case levelDetails:
		m.detailsKey(msg, gg)
	default:
		return m.reposKey(msg, gg)
	}
	return nil
}

func (m *Model) reposKey(msg tea.KeyPressMsg, gg bool) tea.Cmd {
	k, l := m.keys, &m.repos
	var moved bool
	switch {
	case key.Matches(msg, k.Down):
		moved = l.setCursor(l.cursor + 1)
	case key.Matches(msg, k.Up):
		moved = l.setCursor(l.cursor - 1)
	case gg:
		moved = l.setCursor(0)
	case key.Matches(msg, k.Bottom):
		moved = l.setCursor(len(l.repos))
	case key.Matches(msg, k.Right):
		m.enterBoxes(0)
	case key.Matches(msg, k.Jump):
		m.enterBoxes(jumpIndex(msg))
	}
	if moved {
		return m.selectRepo(true)
	}
	return nil
}

func (m *Model) enterBoxes(box int) {
	if _, ok := m.repos.selected(); !ok {
		if m.repos.loaded {
			m.setInfo(renovateRow + " isn't built yet")
		}
		return
	}
	if m.focusBox(box) {
		m.level = levelBoxes
	}
}

// focusBox focuses box i, or reports false with a status message when the repo has no such box.
func (m *Model) focusBox(i int) bool {
	if i < 0 || i >= m.boxes.count() {
		m.setInfo(fmt.Sprintf("No box [%d] here", i+1))
		return false
	}
	if boxKind(i) != m.boxes.focus {
		m.boxes.focus = boxKind(i)
		m.details.tab = 0
	}
	return true
}

func (m *Model) boxesKey(msg tea.KeyPressMsg, gg bool) {
	k, b := m.keys, &m.boxes
	switch {
	case key.Matches(msg, k.Down):
		b.setCursor(b.cursor[b.focus] + 1)
	case key.Matches(msg, k.Up):
		b.setCursor(b.cursor[b.focus] - 1)
	case gg:
		b.setCursor(0)
	case key.Matches(msg, k.Bottom):
		b.setCursor(b.len(b.focus) - 1)
	case key.Matches(msg, k.Jump):
		m.focusBox(jumpIndex(msg))
	case key.Matches(msg, k.NextBox):
		m.focusBox((int(b.focus) + 1) % b.count())
	case key.Matches(msg, k.PrevBox):
		m.focusBox((int(b.focus) + b.count() - 1) % b.count())
	case key.Matches(msg, k.Right):
		if b.selected() == nil {
			m.setInfo("This box is empty")
		} else {
			m.level = levelDetails
		}
	case key.Matches(msg, k.NextTab):
		m.details.cycleTab(b.selected(), 1)
	case key.Matches(msg, k.PrevTab):
		m.details.cycleTab(b.selected(), -1)
	case key.Matches(msg, k.Left):
		m.level = levelRepos
	}
}

func (m *Model) detailsKey(msg tea.KeyPressMsg, gg bool) {
	k, vp, b := m.keys, &m.details.vp, &m.boxes
	switch {
	case key.Matches(msg, k.Down):
		vp.ScrollDown(1)
	case key.Matches(msg, k.Up):
		vp.ScrollUp(1)
	case key.Matches(msg, k.HalfDown):
		vp.HalfPageDown()
	case key.Matches(msg, k.HalfUp):
		vp.HalfPageUp()
	case gg:
		vp.GotoTop()
	case key.Matches(msg, k.Bottom):
		vp.GotoBottom()
	case key.Matches(msg, k.NextTab):
		m.details.cycleTab(b.selected(), 1)
	case key.Matches(msg, k.PrevTab):
		m.details.cycleTab(b.selected(), -1)
	case key.Matches(msg, k.Jump):
		if m.focusBox(jumpIndex(msg)) {
			m.level = levelBoxes
		}
	case key.Matches(msg, k.NextBox):
		m.focusBox((int(b.focus) + 1) % b.count())
		m.level = levelBoxes
	case key.Matches(msg, k.PrevBox):
		m.focusBox((int(b.focus) + b.count() - 1) % b.count())
		m.level = levelBoxes
	case key.Matches(msg, k.Left):
		m.level = levelBoxes
	}
}

func jumpIndex(msg tea.KeyPressMsg) int {
	return int(msg.String()[0] - '1')
}

func (m *Model) setError(err error) { m.status, m.statusErr = err.Error(), true }
func (m *Model) setInfo(s string)   { m.status, m.statusErr = s, false }

func (m Model) layout() (bodyH, leftW, rightW int) {
	bodyH = max(m.height-2, 0)
	leftW = m.width * leftPercent / 100
	return bodyH, leftW, m.width - leftW
}

func (m *Model) syncDetails() {
	bodyH, _, rightW := m.layout()
	m.details.sync(m.boxes.selected(), m.boxes.repo, rightW, bodyH, m.now())
}

// View renders nothing until the first WindowSizeMsg, since layout depends on it.
func (m Model) View() tea.View {
	var v tea.View
	if m.width > 0 && m.height > 0 {
		v = tea.NewView(fitLines(strings.Split(m.header()+"\n"+m.body()+"\n"+m.statusBar(), "\n"), m.width, m.height))
	}
	v.AltScreen = true
	return v
}

func (m Model) body() string {
	bodyH, leftW, rightW := m.layout()
	if m.showHelp {
		h := m.help
		h.SetWidth(max(m.width-4, 1))
		lines := strings.Split(h.FullHelpView(m.keys.fullHelp()), "\n")
		box := frame(style.ActiveTitle.Render("Help"), lines, min(lipgloss.Width(strings.Join(lines, "\n"))+4, m.width), min(len(lines)+2, bodyH), true)
		return fitLines(strings.Split(lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, box), "\n"), m.width, bodyH)
	}
	if m.dialog != nil {
		return m.dialog.view(m.width, bodyH, m.info.ChangeRequestTerm, m.keys, m.help)
	}
	now := m.now()
	var left, right string
	switch m.level {
	case levelRepos:
		left = m.repos.view(leftW, bodyH, m.svc, m.info.ChangeRequestTerm, now)
		if _, ok := m.repos.selected(); ok {
			right = m.boxes.view(rightW, bodyH, -1, false, m.info.ChangeRequestTerm, now)
		} else if m.repos.loaded {
			right = frame(style.PaneTitle.Render(renovateRow), []string{style.Faint.Render(renovateRow + " isn't built yet.")}, rightW, bodyH, false)
		} else {
			right = frame("", nil, rightW, bodyH, false)
		}
	default:
		left = m.boxes.view(leftW, bodyH, int(m.boxes.focus), m.level == levelBoxes, m.info.ChangeRequestTerm, now)
		right = m.details.view(m.boxes.selected(), rightW, bodyH, m.level == levelDetails)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, fitLines(strings.Split(left, "\n"), leftW, bodyH), fitLines(strings.Split(right, "\n"), rightW, bodyH))
}

func (m Model) header() string {
	brand := style.Brand.Render("lazyforge")
	return brand + " " + m.breadcrumb(m.width-lipgloss.Width(brand)-1)
}

// breadcrumb fits the crumbs into w columns by eliding middle segments, keeping the host and the last one.
func (m Model) breadcrumb(w int) string {
	host := m.info.URL
	if u, err := url.Parse(m.info.URL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	crumbs := []string{host}
	if r, ok := m.repos.selected(); ok {
		crumbs = append(crumbs, r.String())
		if m.level != levelRepos {
			crumbs = append(crumbs, fmt.Sprintf("[%d] %s", m.boxes.focus+1, boxTitle(m.boxes.focus, m.info.ChangeRequestTerm)))
			if c := itemCrumb(m.boxes.selected()); c != "" {
				crumbs = append(crumbs, c)
			}
		}
	} else if m.repos.loaded {
		crumbs = append(crumbs, renovateRow)
	}
	const sep = " › "
	for drop := 0; len(crumbs)-drop >= 2; drop++ {
		shown := crumbs
		if drop > 0 {
			shown = append([]string{crumbs[0], "…"}, crumbs[1+drop:]...)
		}
		if lipgloss.Width(strings.Join(shown, sep)) > w {
			continue
		}
		last := len(shown) - 1
		parts := make([]string, len(shown))
		for i, c := range shown {
			parts[i] = style.Crumb.Render(c)
		}
		parts[last] = style.CurrentCrumb.Render(shown[last])
		return strings.Join(parts, style.CrumbSep.Render(sep))
	}
	return style.Crumb.Render(truncate(strings.Join(crumbs, sep), w))
}

func (m Model) statusBar() string {
	var badge string
	switch m.level {
	case levelBoxes:
		badge = style.ModeBoxes.Render("BOXES")
	case levelDetails:
		badge = style.ModeDetails.Render("DETAILS")
	default:
		badge = style.ModeRepos.Render("REPOS")
	}
	room := max(m.width-lipgloss.Width(badge)-1, 0)
	status := truncate(m.status, room)
	if m.statusErr {
		status = style.StatusErr.Render(status)
	} else {
		status = style.StatusInfo.Render(status)
	}
	h := m.help
	hw := max(room-lipgloss.Width(status)-1, 1)
	h.SetWidth(hw)
	// help adds an overflowing item anyway when its ellipsis doesn't fit, so clip to keep the status visible.
	bindings := m.keys.shortHelp(m.level)
	if m.dialog != nil {
		bindings = m.dialog.hints(m.keys)
	}
	hints := clip(h.ShortHelpView(bindings), hw)
	gap := max(room-lipgloss.Width(hints)-lipgloss.Width(status), 0)
	return badge + " " + hints + strings.Repeat(" ", gap) + status
}
