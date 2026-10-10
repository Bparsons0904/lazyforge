package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

// Deps is what cmd hands the app; Connect and Probe keep adapters out of ui.
type Deps struct {
	ConfigPath, StatePath string
	Config                config.Config // config.Defaults() when Fresh
	Fresh                 bool          // no config file: start onboarding at step 1
	LastHost              string        // from state; picker cursor
	Host                  string        // selected host; "" shows the picker (or onboarding when Fresh)
	Forge                 forge.Forge   // connected forge for Host; nil when Host == ""
	Connect               func(context.Context, config.Host) (forge.Forge, error)
	Probe                 func(ctx context.Context, url string) (forge.Kind, error)
	Detect                termimg.Detector // terminal image detection, run once from Init; the zero value skips it
}

type appScreen int

const (
	screenSession appScreen = iota
	screenPicker
	screenSettings
	screenOnboarding
	screenSplash
)

// App is the root model: it owns the screen, the config and the one live session.
type App struct {
	ctx  context.Context
	deps Deps
	live *atomic.Pointer[config.Config] // read off the UI goroutine by core.Merge; replaced, never mutated
	sv   *saver
	keys keyMap
	help help.Model

	fresh      bool
	screen     appScreen
	settingsTo appScreen // where Settings returns on close
	onboardTo  appScreen // where onboarding returns on cancel
	splashTo   appScreen
	crumb      string // header for the onboarding screen
	size       *tea.WindowSizeMsg
	status     string
	statusErr  bool

	host       string // the session's host; "" when there is none
	session    Model
	endSession context.CancelFunc
	gen        int
	connecting string // the host a picker connect is for; other results are stale

	saveSeq     int
	doneSeq     int // the save carrying pendingDone; 0 when none is in flight
	pendingDone onboardDoneMsg
	pendingCfg  config.Config

	picker   picker
	onboard  onboarding
	settings settings
	splash   splash

	detect  termimg.Detector
	support termimg.Support
}

// NewApp starts on onboarding when Fresh, on d.Host's session when set, else on the picker,
// with the splash in front of it when the config shows one.
func NewApp(ctx context.Context, d Deps) App {
	live := new(atomic.Pointer[config.Config])
	c := d.Config
	live.Store(&c)
	a := App{ctx: ctx, deps: d, live: live, sv: &saver{path: d.ConfigPath}, keys: defaultKeys(), help: newHelp(), fresh: d.Fresh, detect: d.Detect}
	switch {
	case d.Fresh:
		a.openOnboarding(ctx, onboardStart{welcome: true})
	case d.Host != "":
		a.openSession(ctx, d.Host, d.Forge)
	default:
		a.screen, a.picker = screenPicker, newPicker(c, d.LastHost)
	}
	if c.Splash.Show {
		a.splashTo, a.screen, a.splash = a.screen, screenSplash, newSplash()
	}
	return a
}

// Init loads the startup session behind the splash, so skipping the splash early adds no wait.
func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	if a.screen == screenSplash {
		cmds = append(cmds, a.splash.start())
	}
	if a.host != "" {
		cmds = append(cmds, stamp(a.gen, a.session.Init()))
	}
	cmds = append(cmds, a.detect.Start())
	return tea.Batch(cmds...)
}

// Update never blocks; disk and network work runs in the returned command.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.size = &msg
	case tea.KeyPressMsg:
		return a, a.handleKey(msg)
	case tea.PasteMsg:
		if a.screen == screenSettings && a.settings.mode == modeInput {
			a.settings.input, _ = a.settings.input.Update(msg)
			return a, nil
		}
	case stampedMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		return a, a.toSession(msg.msg)
	case connectedMsg:
		if _, ok := a.cfg().Hosts[msg.name]; msg.name != a.connecting || !ok {
			return a, nil
		}
		a.connecting = ""
		if msg.err != nil {
			a.setErrorText(connectError(msg.name, msg.err))
			return a, nil
		}
		a.status = ""
		switch {
		case a.screen == screenPicker:
			a.screen = screenSession
		case a.screen == screenSettings && a.settingsTo == screenPicker:
			a.settingsTo = screenSession
		}
		return a, a.startSession(a.ctx, msg.name, msg.f)
	case configSavedMsg:
		return a, a.saved(msg)
	case onboardDoneMsg:
		return a, a.onboardDone(msg)
	case splashFrameMsg:
		if a.screen != screenSplash {
			return a, nil
		}
		a.splash.frame++
		return a, nextSplashFrame()
	case splashDoneMsg:
		a.leaveSplash()
		return a, nil
	case onboardCancelMsg:
		if a.fresh {
			return a, tea.Quit
		}
		a.screen = a.onboardTo
		return a, nil
	case termimg.DetectedMsg:
		a.support = msg.Support
		return a, a.toSession(a.imagesMsg())
	}
	// The rest are sizes, onboarding results, or session messages the runtime delivered unstamped
	// (editorDoneMsg from the $EDITOR callback); each side ignores the other's.
	var cmd tea.Cmd
	if a.screen == screenOnboarding {
		a.onboard, cmd = a.onboard.update(msg, a.keys)
	}
	// Terminal replies for detection arrive here too, unstamped, as the runtime forwards them.
	var detectCmd tea.Cmd
	a.detect, detectCmd = a.detect.Update(msg)
	return a, tea.Batch(cmd, detectCmd, a.toSession(msg))
}

func (a *App) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.keys
	switch a.screen {
	case screenSplash:
		if key.Matches(msg, k.Interrupt) {
			return tea.Quit
		}
		a.leaveSplash()
		return nil
	case screenSession:
		s := &a.session
		if s.dialog == nil && s.labels == nil && !s.showHelp {
			switch {
			case key.Matches(msg, k.Settings):
				a.openSettings()
				return nil
			case s.level == levelRepos && key.Matches(msg, k.Left):
				a.screen, a.picker = screenPicker, newPicker(a.cfg(), a.host)
				return nil
			}
		}
		return a.toSession(msg)
	case screenOnboarding:
		var cmd tea.Cmd
		a.onboard, cmd = a.onboard.update(msg, k)
		return cmd
	}
	a.status = ""
	if key.Matches(msg, k.Interrupt) {
		return tea.Quit
	}
	if a.screen == screenPicker {
		return a.pickerKey(msg)
	}
	var in settingsIntent
	a.settings, in = a.settings.update(msg, a.settingsView(), k)
	switch in.kind {
	case intentChange:
		a.apply(in.cfg)
		return tea.Batch(a.save(in.cfg), a.toSession(a.imagesMsg()))
	case intentAdd:
		a.openOnboarding(a.ctx, onboardStart{})
	case intentEdit:
		a.openOnboarding(a.ctx, onboardStart{edit: in.host, host: a.cfg().Hosts[in.host]})
	case intentClose:
		a.screen = a.settingsTo
	}
	return nil
}

// connectError names the host and keeps only the innermost cause, since the wrapped chain is too long for the status bar.
func connectError(name string, err error) string {
	for u := errors.Unwrap(err); u != nil; u = errors.Unwrap(err) {
		err = u
	}
	return fmt.Sprintf("Can't connect to %s: %v", name, err)
}

// leaveSplash is a no-op once the splash is gone, so a late timer can't move the user.
func (a *App) leaveSplash() {
	if a.screen != screenSplash {
		return
	}
	a.screen = a.splashTo
	// Onboarding only takes the window width while it is on screen.
	if a.screen == screenOnboarding && a.size != nil {
		a.onboard.resize(a.size.Width)
	}
}

func (a App) cfg() config.Config { return *a.live.Load() }

func (a *App) apply(c config.Config) { a.live.Store(&c) }

func (a *App) openSettings() {
	if a.screen != screenSettings {
		a.settingsTo = a.screen
	}
	a.screen, a.settings = screenSettings, settings{}
}

// openOnboarding fills in taken; s.welcome is set only for the first run.
func (a *App) openOnboarding(ctx context.Context, s onboardStart) {
	// A pending picker connect must not start a session behind onboarding.
	a.connecting = ""
	live := a.live
	s.taken = func(name string) bool { _, ok := live.Load().Hosts[name]; return ok }
	switch {
	case s.welcome:
		a.crumb = "Welcome"
	case s.edit != "":
		a.crumb = "Edit " + s.edit
	default:
		a.crumb = "Add host"
	}
	a.onboardTo, a.screen = a.screen, screenOnboarding
	a.onboard = newOnboarding(ctx, a.deps.Connect, a.deps.Probe, s)
	if a.size != nil {
		a.onboard.resize(a.size.Width)
	}
}

// onboardDone saves the new config and applies it only once the write succeeds.
func (a *App) onboardDone(msg onboardDoneMsg) tea.Cmd {
	next := a.cfg().Clone()
	if next.Hosts == nil {
		next.Hosts = map[string]config.Host{}
	}
	h := msg.host
	if msg.replaces != "" {
		old := next.Hosts[msg.replaces]
		h.RequireGreenCI, h.Repos = old.RequireGreenCI, old.Repos
		delete(next.Hosts, msg.replaces)
		if next.DefaultHost == msg.replaces {
			next.DefaultHost = msg.name
		}
	}
	next.Hosts[msg.name] = h
	cmd := a.save(next)
	a.doneSeq, a.pendingDone, a.pendingCfg = a.saveSeq, msg, next
	return cmd
}

func (a *App) saved(msg configSavedMsg) tea.Cmd {
	if msg.seq != a.doneSeq || a.doneSeq == 0 {
		if msg.err != nil {
			a.setError(msg.err)
		}
		return nil
	}
	a.doneSeq = 0
	if msg.err != nil {
		a.onboard.saveFailed(msg.err)
		return nil
	}
	done := a.pendingDone
	a.apply(a.pendingCfg)
	a.fresh = false
	if done.replaces == "" {
		a.screen = screenSession
		return a.startSession(a.ctx, done.name, done.f)
	}
	a.screen = screenSettings
	if done.replaces == a.host {
		return a.startSession(a.ctx, done.name, done.f)
	}
	return nil
}

// openSession replaces the live session with one for host name over f, cancelling the old one's work.
// It returns the terminal sequences that delete the old session's images, which the caller must write.
func (a *App) openSession(ctx context.Context, name string, f forge.Forge) string {
	var del string
	if a.host != "" {
		del = a.session.details.img.release()
	}
	if a.endSession != nil {
		a.endSession()
	}
	a.gen++
	sctx, cancel := context.WithCancel(ctx)
	live := a.live
	svc := core.New(f, core.Options{
		RequireGreenCI:   func(r domain.RepoRef) bool { return live.Load().Hosts[name].RequiresGreenCI(r.String()) },
		RenovateUser:     live.Load().Hosts[name].RenovateUser,
		HideRenovate:     !live.Load().Hosts[name].ShowsRenovate(),
		RenovateWorkflow: func() core.RenovateWorkflow { return renovateWorkflow(live.Load().Hosts[name].RenovateWorkflow) },
	})
	a.host, a.session, a.endSession = name, New(sctx, svc), cancel
	a.session.keys.setHosted(true)
	if a.size != nil {
		a.session.width, a.session.height = a.size.Width, a.size.Height
	}
	return del
}

// renovateWorkflow parses a host's renovate_workflow; zero when it is unset or malformed, which hides N.
func renovateWorkflow(s string) core.RenovateWorkflow {
	owner, name, file, err := config.SplitWorkflow(s)
	if err != nil {
		return core.RenovateWorkflow{}
	}
	return core.RenovateWorkflow{Repo: domain.RepoRef{Owner: owner, Name: name}, File: file}
}

// startSession opens a session that the user chose, so it also records name as the last host.
func (a *App) startSession(ctx context.Context, name string, f forge.Forge) tea.Cmd {
	del := a.openSession(ctx, name, f)
	// A fresh session holds no images, so applying the setting directly skips a full Update pass and yields no output.
	a.session.imagesChanged(a.imagesMsg())
	return tea.Sequence(rawCmd(del), tea.Batch(stamp(a.gen, a.session.Init()), rememberHost(a.deps.StatePath, name)))
}

// imagesMsg is the setting and terminal support the live session's images follow.
func (a App) imagesMsg() imagesMsg {
	return imagesMsg{support: a.support, show: a.cfg().Images.Show}
}

// ReleaseImages returns the terminal sequences that delete every image the live session holds; "" when none.
func (a App) ReleaseImages() string {
	if a.host == "" {
		return ""
	}
	return a.session.details.img.release()
}

// toSession delivers msg to the live session and stamps what it returns; a no-op without one.
func (a *App) toSession(msg tea.Msg) tea.Cmd {
	if a.host == "" {
		return nil
	}
	next, cmd := a.session.Update(msg)
	a.session = next.(Model)
	return stamp(a.gen, cmd)
}

func (a App) settingsView() settingsView {
	v := settingsView{cfg: a.cfg(), active: a.host}
	if a.host == "" {
		return v
	}
	if repos, _, ok := a.session.svc.PeekRepos(); ok {
		v.repos = make([]domain.RepoRef, len(repos))
		for i, r := range repos {
			v.repos[i] = r.RepoRef
		}
	}
	return v
}

// setError shows err where the user is looking: the session's status bar or the app's.
func (a *App) setError(err error) { a.setErrorText(err.Error()) }

func (a *App) setErrorText(s string) {
	if a.screen == screenSession && a.host != "" {
		a.session.status, a.session.statusErr = s, true
		return
	}
	a.status, a.statusErr = s, true
}

func (a *App) setInfo(s string) { a.status, a.statusErr = s, false }

// View renders nothing until the first WindowSizeMsg, since layout depends on it.
func (a App) View() tea.View {
	if a.screen == screenSplash {
		v := tea.NewView(a.splash.view(a.width(), a.height()))
		v.AltScreen = true
		return v
	}
	if a.screen == screenSession {
		return a.session.View()
	}
	var v tea.View
	if w, h := a.width(), a.height(); w > 0 && h > 0 {
		bodyH := max(h-2, 0)
		var body, crumb, badge string
		var hints []key.Binding
		switch a.screen {
		case screenPicker:
			body, crumb, badge, hints = a.pickerView(w, bodyH), "Hosts", style.ModeHosts.Render("HOSTS"), a.keys.pickerHints()
		case screenSettings:
			sv := a.settingsView()
			body, crumb, badge, hints = a.settings.view(w, bodyH, sv), "Settings", style.ModeSettings.Render("SETTINGS"), a.settings.hints(a.keys)
		default:
			body, crumb, badge, hints = a.onboard.view(w, bodyH), a.crumb, style.ModeSetup.Render("SETUP"), a.onboard.hints(a.keys)
		}
		header := style.Brand.Render("lazyforge") + style.CrumbSep.Render(" › ") + style.CurrentCrumb.Render(crumb)
		v = tea.NewView(fitLines(strings.Split(header+"\n"+body+"\n"+renderStatusBar(w, a.help, badge, a.status, a.statusErr, hints), "\n"), w, h))
	}
	v.AltScreen = true
	return v
}

func (a App) width() int {
	if a.size == nil {
		return 0
	}
	return a.size.Width
}

func (a App) height() int {
	if a.size == nil {
		return 0
	}
	return a.size.Height
}

func newHelp() help.Model {
	h := help.New()
	h.ShortSeparator = " · "
	h.Styles.ShortKey, h.Styles.ShortDesc, h.Styles.ShortSeparator = style.HintKey, style.HintText, style.HintText
	h.Styles.FullKey, h.Styles.FullDesc, h.Styles.FullSeparator = style.HelpKey, style.Text, style.HintText
	h.Styles.Ellipsis = style.HintText
	return h
}

// sessionMsg marks the messages a session's commands produce, so stamp can tie them to their session.
type sessionMsg interface{ fromSession() }

func (reposLoadedMsg) fromSession()          {}
func (changeRequestsLoadedMsg) fromSession() {}
func (issuesLoadedMsg) fromSession()         {}
func (runsLoadedMsg) fromSession()           {}
func (releasesLoadedMsg) fromSession()       {}
func (refreshTickMsg) fromSession()          {}
func (recheckedMsg) fromSession()            {}
func (mergeDoneMsg) fromSession()            {}
func (actionDoneMsg) fromSession()           {}
func (renovateRunMsg) fromSession()          {}
func (renovateScannedMsg) fromSession()      {}
func (starRecheckedMsg) fromSession()        {}
func (starMergeDoneMsg) fromSession()        {}
func (imageLoadedMsg) fromSession()          {}
func (imagePlacedMsg) fromSession()          {}

type stampedMsg struct {
	gen int
	msg tea.Msg
}

// stamp wraps cmd's session messages with gen, so App drops results from a session it has replaced.
// Two hosts can serve the same owner/name, so the session's own repo-key check can't tell them apart.
func stamp(gen int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(msg))
			for i, c := range msg {
				out[i] = stamp(gen, c)
			}
			return out
		case sessionMsg:
			return stampedMsg{gen: gen, msg: msg}
		default:
			return msg
		}
	}
}

type configSavedMsg struct {
	seq int
	err error
}

// saver serialises config writes so a slow older snapshot never lands over a newer one.
type saver struct {
	mu      sync.Mutex
	path    string
	written int
}

func (a *App) save(c config.Config) tea.Cmd {
	a.saveSeq++
	seq, s := a.saveSeq, a.sv
	return func() tea.Msg {
		s.mu.Lock()
		defer s.mu.Unlock()
		if seq < s.written {
			return configSavedMsg{seq: seq}
		}
		s.written = seq
		return configSavedMsg{seq: seq, err: config.Save(s.path, c)}
	}
}

// rememberHost records name as the last host; state is disposable, so failures are ignored.
func rememberHost(path, name string) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		s, err := config.LoadState(path)
		if err == nil {
			s.LastHost = name
			_ = config.SaveState(path, s)
		}
		return nil
	}
}
