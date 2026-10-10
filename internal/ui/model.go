// Package ui holds the Bubble Tea models; it reaches the forge only through core.
package ui

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
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
	labels      *labelPicker
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
	star    starModel
}

// New returns the root model for one session over svc; ctx bounds every fetch the UI makes.
func New(ctx context.Context, svc *core.Service) Model {
	m := Model{ctx: ctx, svc: svc, info: svc.Info(), keys: defaultKeys(), help: newHelp(), now: time.Now, tick: tickEvery}
	m.repos.noStar = svc.HidesRenovate()
	m.openURL = func(u string) error { return openBrowser(ctx, u) }
	m.keys.setHosted(false) // only an App hosting the session handles S and the hosts key
	m.syncKeys()
	return m
}

// Init loads the repo list and starts the five-minute refresh tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadRepos(m.ctx, m.svc), m.tick())
}

func repoRefs(repos []domain.Repo) []domain.RepoRef {
	out := make([]domain.RepoRef, len(repos))
	for i, r := range repos {
		out[i] = r.RepoRef
	}
	return out
}

// Update applies msg; it never blocks, and all I/O happens in the returned command.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmd = m.handleKey(msg)
	case tea.PasteMsg:
		if m.labels != nil && m.labels.loaded && !m.labels.saving {
			m.labels.search, cmd = m.labels.search.Update(msg)
			m.labels.cursor = 0
		}
	case refreshTickMsg:
		cmd = tea.Batch(m.refresh(), m.tick())
	case reposLoadedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			break
		}
		if m.repos.replace(msg.repos) {
			cmd = m.selectRepo(true)
		} else if m.onStar() && (m.star.cov == nil || !slices.Equal(m.star.refs, repoRefs(m.repos.repos))) {
			// A changed repo list mid-refresh must keep refetching rather than fall back to the cache.
			cmd = m.startScan(!m.star.fresh)
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
	case releasesLoadedMsg:
		if !m.loadFailed(msg.key, msg.err) {
			m.boxes.releases, m.boxes.loaded[boxReleases] = msg.items, true
		}
	case readmeLoadedMsg:
		if !m.loadFailed(msg.key, msg.err) {
			m.boxes.readme = readmeState{ok: true, r: msg.readme}
		}
	case branchesLoadedMsg:
		cmd = m.branchesLoaded(msg)
	case commitsLoadedMsg:
		m.commitsLoaded(msg)
	case treeLoadedMsg:
		cmd = m.treeLoaded(msg)
	case previewLoadedMsg:
		m.previewLoaded(msg)
	case renovateScannedMsg:
		m.scanned(msg)
	case starRecheckedMsg:
		m.starRechecked(msg)
	case starMergeDoneMsg:
		m.starMergeDone(msg)
	case recheckedMsg:
		cmd = m.rechecked(msg)
	case mergeDoneMsg:
		cmd = m.mergeDone(msg)
	case actionDoneMsg:
		cmd = m.actionDone(msg)
	case labelsLoadedMsg:
		if m.labels == msg.picker {
			m.labels.load(msg)
		}
	case labelsSavedMsg:
		if m.labels == msg.picker {
			m.labels.saving = false
			m.labels.err = msg.err
			if msg.err == nil {
				m.labels = nil
				cmd = m.actionDone(actionDoneMsg{repo: msg.picker.item.Repo, item: msg.picker.item, verb: "Updated labels on"})
			}
		}
	case editorDoneMsg:
		cmd = m.postComment(msg)
	case imagesMsg:
		cmd = m.imagesChanged(msg)
	case imageLoadedMsg:
		cmd = m.imageLoaded(msg)
	case imagePlacedMsg:
		cmd = m.imagePlaced(msg)
	}
	m.boxes.clampCursors()
	if !m.filesActive() {
		m.details.filesFocus = false
	}
	m.syncDetails()
	m.syncKeys()
	return m, tea.Batch(cmd, m.syncImages())
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keys
	m.status = ""
	if key.Matches(msg, k.Interrupt) {
		return tea.Quit
	}
	if m.labels != nil {
		return m.labelsKey(msg)
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
	gg, swallowed := gPrefix(&m.pendingG, msg, k)
	if swallowed {
		return nil
	}
	switch {
	case key.Matches(msg, k.Quit):
		return tea.Quit
	case key.Matches(msg, k.Help):
		m.showHelp = true
		return nil
	case key.Matches(msg, k.Refresh):
		m.svc.ClearImages()
		m.details.img.forgetFailed()
		return m.refresh()
	}
	if m.level != levelRepos {
		if cmd, ok := m.actionKey(msg); ok {
			return cmd
		}
	} else if repo, ok := m.repos.selected(); ok && key.Matches(msg, k.Open) {
		cmd, _ := m.itemActionKey(msg, forge.ItemRef{Repo: repo.RepoRef}, repo)
		return cmd
	}
	switch {
	case m.level == levelBoxes && m.onStar():
		m.starBoxesKey(msg, gg)
	case m.level == levelDetails && m.onStar():
		m.starDetailsKey(msg, gg)
	case m.level == levelBoxes:
		m.boxesKey(msg, gg)
	case m.level == levelDetails:
		return m.detailsKey(msg, gg)
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
		moved = l.setCursor(l.last())
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
	if m.onStar() {
		if m.focusStar(box) {
			m.level = levelBoxes
		}
		return
	}
	if _, ok := m.repos.selected(); !ok {
		return
	}
	if m.focusBox(box) {
		m.level = levelBoxes
	}
}

// focusBox focuses box i, or reports false with a status message when the repo has no such box.
func (m *Model) focusBox(i int) bool {
	if !slices.Contains(m.boxes.kinds(), boxKind(i)) {
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
		m.focusBox(int(b.step(1)))
	case key.Matches(msg, k.PrevBox):
		m.focusBox(int(b.step(-1)))
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

// scrollKey scrolls the details pane; it reports whether msg was a scroll key.
func (m *Model) scrollKey(msg tea.KeyPressMsg, gg bool) bool {
	k, vp := m.keys, &m.details.vp
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
	default:
		return false
	}
	return true
}

func (m *Model) detailsKey(msg tea.KeyPressMsg, gg bool) tea.Cmd {
	if m.filesActive() {
		if cmd, ok := m.filesKey(msg, gg); ok {
			return cmd
		}
	}
	if m.branchesActive() {
		if cmd, ok := m.branchesKey(msg, gg); ok {
			return cmd
		}
	}
	if m.scrollKey(msg, gg) {
		return nil
	}
	k, b := m.keys, &m.boxes
	switch {
	case key.Matches(msg, k.NextTab):
		m.details.cycleTab(b.selected(), 1)
	case key.Matches(msg, k.PrevTab):
		m.details.cycleTab(b.selected(), -1)
	case key.Matches(msg, k.Jump):
		if m.focusBox(jumpIndex(msg)) {
			m.level = levelBoxes
		}
	case key.Matches(msg, k.NextBox):
		m.focusBox(int(b.step(1)))
		m.level = levelBoxes
	case key.Matches(msg, k.PrevBox):
		m.focusBox(int(b.step(-1)))
		m.level = levelBoxes
	case key.Matches(msg, k.Left):
		m.level = levelBoxes
	}
	return nil
}

// branchesActive reports whether the Branches tab takes the cursor keys: the Repo's details at the details level, with a branch to move over.
func (m Model) branchesActive() bool {
	_, isRepo := m.boxes.selected().(domain.Repo)
	return m.level == levelDetails && isRepo && m.details.tab == branchesTab && m.boxes.showBranches && len(m.boxes.branches.list) > 0
}

// branchesKey moves the branch cursor for the cursor keys and reports whether msg was one; a move loads the new branch's commits.
func (m *Model) branchesKey(msg tea.KeyPressMsg, gg bool) (tea.Cmd, bool) {
	k, d, n := m.keys, &m.details, len(m.boxes.branches.list)
	bodyH, _, _ := m.layout()
	page := max(branchRows(max(bodyH-2, 0))/2, 1)
	was := d.branchCur
	switch {
	case key.Matches(msg, k.Right) && n > 0:
		return m.browseBranch(), true
	case key.Matches(msg, k.Down):
		d.branchCur++
	case key.Matches(msg, k.Up):
		d.branchCur--
	case key.Matches(msg, k.HalfDown):
		d.branchCur += page
	case key.Matches(msg, k.HalfUp):
		d.branchCur -= page
	case gg:
		d.branchCur = 0
	case key.Matches(msg, k.Bottom):
		d.branchCur = n - 1
	default:
		return nil, false
	}
	d.branchCur = max(min(d.branchCur, n-1), 0)
	if d.branchCur == was {
		return nil, true
	}
	return m.landOn(), true
}

// branchesLoaded stores the branch list, keeps the cursor on the branch it was on, and loads the commits of the branch under it when that changed.
func (m *Model) branchesLoaded(msg branchesLoadedMsg) tea.Cmd {
	if m.loadFailed(msg.key, msg.err) {
		return nil
	}
	b, d := &m.boxes, &m.details
	prev := ""
	if d.branchCur < len(b.branches.list) {
		prev = b.branches.list[d.branchCur].Name
	}
	b.branches.ok, b.branches.list = true, msg.branches
	d.branchCur = max(min(d.branchCur, len(msg.branches)-1), 0)
	if i := slices.IndexFunc(msg.branches, func(x domain.Branch) bool { return x.Name == prev }); i >= 0 {
		d.branchCur = i
	}
	if len(msg.branches) == 0 || msg.branches[d.branchCur].Name == prev {
		return nil
	}
	return m.landOn()
}

// commitsLoaded stores a branch's commits, dropping a result for another repo or for a branch the list no longer has.
func (m *Model) commitsLoaded(msg commitsLoadedMsg) {
	if m.loadFailed(msg.key, msg.err) {
		return
	}
	if !slices.ContainsFunc(m.boxes.branches.list, func(x domain.Branch) bool { return x.Name == msg.key.Ref }) {
		return
	}
	m.boxes.branches.setCommits(msg.key.Ref, msg.commits)
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
	m.details.want = nil
	if m.onStar() {
		if m.level != levelRepos {
			m.syncStarDetails()
		}
		return
	}
	m.details.sync(m.boxes.selected(), m.boxes, m.level == levelDetails, rightW, bodyH, m.now())
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
	if m.labels != nil {
		return m.labels.view(m.width, bodyH)
	}
	if m.dialog != nil {
		return m.dialog.view(m.width, bodyH, m.info.ChangeRequestTerm, m.keys, m.help)
	}
	now := m.now()
	var left, right string
	switch {
	case m.level == levelRepos:
		left = m.repos.view(leftW, bodyH, m.svc, m.info.ChangeRequestTerm, now)
		if _, ok := m.repos.selected(); ok {
			right = m.boxes.view(rightW, bodyH, -1, false, m.info.ChangeRequestTerm, now)
		} else if m.repos.loaded {
			right = m.star.render(rightW, bodyH, -1, false, m.info.ChangeRequestTerm, now)
		} else {
			right = frame("", nil, rightW, bodyH, false)
		}
	case m.onStar():
		left = m.star.render(leftW, bodyH, int(m.star.focus), m.level == levelBoxes, m.info.ChangeRequestTerm, now)
		right = m.details.view(nil, rightW, bodyH, m.level == levelDetails)
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
			if c := m.filesCrumb(); c != "" {
				crumbs = append(crumbs, c)
			}
		}
	} else if m.repos.loaded {
		crumbs = append(crumbs, renovateRow)
		if m.level != levelRepos {
			crumbs = append(crumbs, fmt.Sprintf("[%d] %s", m.star.focus+1, starTitle(m.star.focus, m.info.ChangeRequestTerm)))
			if c := m.star.crumb(); c != "" {
				crumbs = append(crumbs, c)
			}
		}
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
	bindings := m.keys.shortHelp(m.level)
	if m.labels != nil {
		bindings = []key.Binding{hint(m.keys.Down, "↑/↓", "move"), key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")), hint(m.keys.Enter, "enter", "save"), hint(m.keys.Close, "esc", "cancel")}
	} else if m.dialog != nil {
		bindings = m.dialog.hints(m.keys)
	}
	return renderStatusBar(m.width, m.help, badge, m.status, m.statusErr, bindings)
}

// renderStatusBar lays out badge, hints and status on one line, giving the status priority when they don't fit.
func renderStatusBar(width int, h help.Model, badge, status string, isErr bool, bindings []key.Binding) string {
	room := max(width-lipgloss.Width(badge)-1, 0)
	status = truncate(status, room)
	if isErr {
		status = style.StatusErr.Render(status)
	} else {
		status = style.StatusInfo.Render(status)
	}
	hw := max(room-lipgloss.Width(status)-1, 1)
	h.SetWidth(hw)
	// help adds an overflowing item anyway when its ellipsis doesn't fit, so clip to keep the status visible.
	hints := clip(h.ShortHelpView(bindings), hw)
	gap := max(room-lipgloss.Width(hints)-lipgloss.Width(status), 0)
	return badge + " " + hints + strings.Repeat(" ", gap) + status
}
