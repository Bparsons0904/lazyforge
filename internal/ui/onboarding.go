package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type onboardStart struct {
	welcome bool        // first run: begin at step 1; otherwise step 2 (add) or step 3 (edit)
	edit    string      // name of the host being edited; "" when adding
	host    config.Host // prefill when editing
	taken   func(name string) bool
}

type onboardDoneMsg struct {
	name, replaces string // replaces is the old name on edit
	host           config.Host
	f              forge.Forge
}

type onboardCancelMsg struct{}

type onboardStep int

const (
	stepWelcome onboardStep = iota + 1
	stepType
	stepURL
	stepSignIn
	stepTest
	stepRenovate
	stepName
)

var forgeRows = []struct {
	kind forge.Kind
	name string
}{{forge.KindForgejo, "Forgejo"}, {forge.KindGitea, "Gitea"}, {forge.KindGitHub, "GitHub"}, {forge.KindGitLab, "GitLab"}}

const selectableForges = 2 // the leading forgeRows that have an adapter; the rest are "coming soon"

var hostNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type probedMsg struct {
	url  string
	kind forge.Kind
	err  error
}

type signedInMsg struct {
	host    config.Host
	f       forge.Forge
	svc     *core.Service
	login   string
	repos   int
	listing bool // err came from listing repos, after the sign-in itself succeeded
	err     error
}

type renovateSuggestedMsg struct {
	user string
	err  error
}

// onboarding runs steps 1-7; each async result is checked against the inputs it was started for.
type onboarding struct {
	ctx     context.Context
	connect func(context.Context, config.Host) (forge.Forge, error)
	probe   func(context.Context, string) (forge.Kind, error)
	start   onboardStart
	first   onboardStep

	step    onboardStep
	cursor  int
	kind    forge.Kind
	url     string // the probed address; set once step 3 passes
	cmdMode bool
	cancel  context.CancelFunc
	busy    bool
	err     string
	note    string

	addr, token, tokenCmd, renovate, name textinput.Model

	tested   config.Host
	f        forge.Forge
	svc      *core.Service
	signedIn string

	renovateDone, renovateTouched bool
}

func newOnboarding(ctx context.Context, connect func(context.Context, config.Host) (forge.Forge, error),
	probe func(context.Context, string) (forge.Kind, error), s onboardStart,
) onboarding {
	o := onboarding{
		ctx: ctx, connect: connect, probe: probe, start: s, kind: forge.KindForgejo,
		addr: newInput("git.example.com"), token: newInput("token"), tokenCmd: newInput("pass show forgejo/token"),
		renovate: newInput("renovate-bot"), name: newInput("name"),
	}
	o.token.EchoMode = textinput.EchoPassword
	switch {
	case s.welcome:
		o.first = stepWelcome
	case s.edit != "":
		o.first = stepURL
		o.kind = forge.Kind(s.host.Type)
		o.addr.SetValue(s.host.URL)
		o.cmdMode = s.host.TokenCmd != ""
		o.token.SetValue(s.host.Token)
		o.tokenCmd.SetValue(s.host.TokenCmd)
		o.renovate.SetValue(s.host.RenovateUser)
		o.name.SetValue(s.edit)
		o.renovateDone = true
	default:
		o.first = stepType
	}
	o.step = o.first
	return o
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	s := in.Styles()
	s.Cursor.Blink = false
	s.Focused.Text, s.Focused.Placeholder, s.Focused.Prompt = style.Text, style.Faint, style.HintKey
	in.SetStyles(s)
	in.Placeholder = placeholder
	in.Focus()
	return in
}

func (o onboarding) update(msg tea.Msg, k keyMap) (onboarding, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return o.key(msg, k)
	case tea.WindowSizeMsg:
		o.resize(msg.Width)
	case probedMsg:
		return o.probed(msg)
	case signedInMsg:
		o.signedInResult(msg)
		return o, nil
	case renovateSuggestedMsg:
		o.suggested(msg)
		return o, nil
	}
	return o.updateInput(msg)
}

func (o *onboarding) resize(width int) {
	for _, in := range []*textinput.Model{&o.addr, &o.token, &o.tokenCmd, &o.renovate, &o.name} {
		in.SetWidth(max(width-8, 10))
	}
}

func (o onboarding) key(msg tea.KeyPressMsg, k keyMap) (onboarding, tea.Cmd) {
	switch {
	case key.Matches(msg, k.Interrupt):
		o.stop()
		return o, tea.Quit
	case key.Matches(msg, k.Close):
		if o.step == o.first {
			o.stop()
			return o, func() tea.Msg { return onboardCancelMsg{} }
		}
		return o.enter(o.step - 1)
	}
	switch o.step {
	case stepWelcome:
		if key.Matches(msg, k.Enter) {
			return o.enter(stepType)
		}
		return o, nil
	case stepType:
		switch {
		case key.Matches(msg, k.Down):
			o.cursor = min(o.cursor+1, selectableForges-1)
		case key.Matches(msg, k.Up):
			o.cursor = max(o.cursor-1, 0)
		case key.Matches(msg, k.Right):
			o.kind = forgeRows[o.cursor].kind
			return o.enter(stepURL)
		}
		return o, nil
	case stepTest:
		switch {
		case !key.Matches(msg, k.Enter) || o.busy:
			return o, nil
		case o.f != nil:
			return o.enter(stepRenovate)
		}
		return o.test()
	}
	switch {
	case key.Matches(msg, k.Enter):
		return o.submit()
	case o.step == stepSignIn && key.Matches(msg, k.NextBox):
		o.cmdMode, o.err = !o.cmdMode, ""
		return o, nil
	}
	return o.updateInput(msg)
}

// enter moves to step s, cancelling the current step's in-flight work and starting s's own.
func (o onboarding) enter(s onboardStep) (onboarding, tea.Cmd) {
	o.stop()
	o.step, o.err, o.note = s, "", ""
	switch s {
	case stepTest:
		if o.f != nil && sameHost(o.tested, o.draft()) {
			return o, nil
		}
		return o.test()
	case stepRenovate:
		if o.renovateDone {
			return o, nil
		}
		ctx, svc := o.begin(), o.svc
		return o, func() tea.Msg {
			user, err := svc.SuggestRenovateUser(ctx)
			return renovateSuggestedMsg{user: user, err: err}
		}
	case stepName:
		if o.name.Value() == "" {
			o.name.SetValue(suggestName(o.url, o.taken))
		}
	}
	return o, nil
}

func (o onboarding) submit() (onboarding, tea.Cmd) {
	if o.busy && o.step == stepName {
		return o, nil
	}
	switch o.step {
	case stepURL:
		u, ok := normalizeURL(o.addr.Value())
		if !ok {
			o.err = "Enter a server address like https://git.example.com"
			return o, nil
		}
		ctx, probe := o.begin(), o.probe
		o.err = ""
		return o, func() tea.Msg {
			kind, err := probe(ctx, u)
			return probedMsg{url: u, kind: kind, err: err}
		}
	case stepSignIn:
		if strings.TrimSpace(o.secret().Value()) == "" {
			o.err = "Paste a token first"
			if o.cmdMode {
				o.err = "Enter a command first"
			}
			return o, nil
		}
		return o.enter(stepTest)
	case stepRenovate:
		return o.enter(stepName)
	}
	name := strings.TrimSpace(o.name.Value())
	switch {
	case name == "":
		o.err = "Enter a name"
	case !hostNameRE.MatchString(name):
		o.err = "Use only letters, digits, '.', '_' and '-'"
	case o.taken(name):
		o.err = fmt.Sprintf("There's already a host named %s", name)
	default:
		o.err, o.busy = "", true
		done := onboardDoneMsg{name: name, replaces: o.start.edit, host: o.draft(), f: o.f}
		done.host.RenovateUser = strings.TrimSpace(o.renovate.Value())
		return o, func() tea.Msg { return done }
	}
	return o, nil
}

func (o onboarding) test() (onboarding, tea.Cmd) {
	h, ctx, connect := o.draft(), o.begin(), o.connect
	o.tested, o.f, o.svc, o.err, o.signedIn = h, nil, nil, "", ""
	o.renovateDone = o.start.edit != ""
	return o, func() tea.Msg {
		f, err := connect(ctx, h)
		if err != nil {
			return signedInMsg{host: h, err: err}
		}
		svc := core.New(f, core.Options{})
		repos, err := svc.Repos(ctx)
		if err != nil {
			return signedInMsg{host: h, listing: true, err: err}
		}
		return signedInMsg{host: h, f: f, svc: svc, login: f.Info().User, repos: len(repos)}
	}
}

func (o onboarding) probed(msg probedMsg) (onboarding, tea.Cmd) {
	if u, _ := normalizeURL(o.addr.Value()); o.step != stepURL || !o.busy || u != msg.url || errors.Is(msg.err, context.Canceled) {
		return o, nil
	}
	o.busy = false
	switch {
	case errors.Is(msg.err, forge.ErrNotFound):
		o.err = "No Forgejo or Gitea API at that address"
	case msg.err != nil:
		o.err = "Can't reach the server: " + msg.err.Error()
	case msg.kind != "" && msg.kind != o.kind:
		o.err = fmt.Sprintf("That server runs %s, not %s", kindName(msg.kind), kindName(o.kind))
	default:
		o.url = msg.url
		return o.enter(stepSignIn)
	}
	return o, nil
}

func (o *onboarding) signedInResult(msg signedInMsg) {
	if o.step != stepTest || !sameHost(msg.host, o.draft()) || errors.Is(msg.err, context.Canceled) {
		return
	}
	o.busy = false
	if msg.err != nil {
		o.err = signInError(msg.err, msg.listing)
		return
	}
	o.f, o.svc = msg.f, msg.svc
	o.signedIn = fmt.Sprintf("Signed in as %s · %d repositories", msg.login, msg.repos)
}

func (o *onboarding) suggested(msg renovateSuggestedMsg) {
	if o.step != stepRenovate || !o.busy || errors.Is(msg.err, context.Canceled) {
		return
	}
	o.busy, o.renovateDone = false, true
	switch {
	case msg.err != nil:
		o.note = "Couldn't look for Renovate PRs: " + msg.err.Error()
	case msg.user == "":
		o.note = "No Renovate PRs found"
	case !o.renovateTouched:
		o.renovate.SetValue(msg.user)
	}
}

// signInError says why the connection test failed; listing means sign-in worked but listing repos didn't.
func signInError(err error, listing bool) string {
	var oe *net.OpError
	var ue *url.Error
	switch {
	case errors.Is(err, forge.ErrUnauthorized) && listing:
		return "Signed in, but the token can't list repositories: it needs write:repository."
	case errors.Is(err, forge.ErrUnauthorized):
		return "The token was rejected. Check it's valid and has read:user."
	case errors.As(err, &oe), errors.As(err, &ue) && ue.Timeout():
		return "Can't reach the server: " + err.Error()
	}
	return err.Error() // token_cmd errors never carry the command's output
}

func (o onboarding) updateInput(msg tea.Msg) (onboarding, tea.Cmd) {
	in := o.input()
	if in == nil {
		return o, nil
	}
	before := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if in.Value() != before {
		o.err = ""
		o.renovateTouched = o.renovateTouched || o.step == stepRenovate
		if o.step == stepURL && o.busy {
			o.stop()
		}
	}
	return o, cmd
}

func (o *onboarding) input() *textinput.Model {
	switch o.step {
	case stepURL:
		return &o.addr
	case stepSignIn:
		return o.secret()
	case stepRenovate:
		return &o.renovate
	case stepName:
		return &o.name
	}
	return nil
}

func (o *onboarding) secret() *textinput.Model {
	if o.cmdMode {
		return &o.tokenCmd
	}
	return &o.token
}

// begin cancels any in-flight step work and returns the context for the next.
func (o *onboarding) begin() context.Context {
	o.stop()
	ctx, cancel := context.WithCancel(o.ctx)
	o.cancel, o.busy = cancel, true
	return ctx
}

func (o *onboarding) stop() {
	if o.cancel != nil {
		o.cancel()
	}
	o.cancel, o.busy = nil, false
}

// draft is the host the current inputs describe; only the active sign-in mode's field is set.
func (o onboarding) draft() config.Host {
	h := config.Host{Type: string(o.kind), URL: o.url}
	if o.cmdMode {
		h.TokenCmd = strings.TrimSpace(o.tokenCmd.Value())
	} else {
		h.Token = strings.TrimSpace(o.token.Value())
	}
	return h
}

// taken allows the edited host to keep its own name.
func (o onboarding) taken(name string) bool {
	return name != o.start.edit && o.start.taken != nil && o.start.taken(name)
}

func (o *onboarding) saveFailed(err error) {
	o.busy, o.err = false, "Couldn't save: "+err.Error()
}

func sameHost(a, b config.Host) bool {
	return a.Type == b.Type && a.URL == b.URL && a.Token == b.Token && a.TokenCmd == b.TokenCmd
}

func kindName(k forge.Kind) string {
	for _, r := range forgeRows {
		if r.kind == k {
			return r.name
		}
	}
	return string(k)
}

// normalizeURL trims s and defaults the scheme to https; ok is false without a host.
func normalizeURL(s string) (string, bool) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", false
	}
	return s, true
}

// suggestName takes the first label of the host after a git./www./code. prefix, adding -2, -3… while taken.
func suggestName(rawURL string, taken func(string) bool) string {
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Hostname()
	}
	for _, p := range []string{"git.", "www.", "code."} {
		if h, ok := strings.CutPrefix(host, p); ok {
			host = h
			break
		}
	}
	base, _, _ := strings.Cut(host, ".")
	name := base
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func (o onboarding) view(w, h int) string {
	var title string
	var lines []string
	switch o.step {
	case stepWelcome:
		title = "Welcome"
		lines = []string{
			"lazyforge is a keyboard-driven terminal UI for your git forge:",
			"pull requests, issues, CI runs and Renovate updates, a key away.",
			"",
			"Let's connect it to your server.",
		}
	case stepType:
		title = "Forge type"
		lines = []string{"Which forge does your server run?", ""}
		for i, r := range forgeRows {
			if i >= selectableForges {
				lines = append(lines, row(r.name, style.Faint.Render("coming soon"), w-4, style.Faint))
				continue
			}
			base := lipgloss.NewStyle()
			if i == o.cursor {
				base = style.Selected
			}
			lines = append(lines, row(r.name, "", w-4, base))
		}
	case stepURL:
		title = "Server address"
		lines = []string{fmt.Sprintf("Address of your %s server", kindName(o.kind)), "", o.addr.View()}
	case stepSignIn:
		title = "Sign in"
		token, command := "● Paste a token", "○ Command that prints one"
		if o.cmdMode {
			token, command = "○ Paste a token", "● Command that prints one"
		}
		lines = []string{
			"Sign in to " + o.url, "", token + "   " + command, "", o.secret().View(), "",
			"Create a token at " + o.url + "/user/settings/applications with:",
			style.HintKey.Render("  read:user") + style.Faint.Render("          sign in"),
			style.HintKey.Render("  write:repository") + style.Faint.Render("   repos, PRs, CI status and runs, merge, approve, close PRs"),
			style.HintKey.Render("  write:issue") + style.Faint.Render("        issues, comments, Renovate dashboard ticks"),
		}
	case stepTest:
		title = "Connection test"
		lines = []string{"Signing in to " + o.url, ""}
		if o.signedIn != "" {
			lines = append(lines, style.Text.Render(o.signedIn))
		}
	case stepRenovate:
		title = "Renovate"
		lines = []string{"Renovate bot username, used to find its PRs. Leave it blank to skip.", "", o.renovate.View()}
	case stepName:
		title = "Name"
		lines = []string{"A short name for this host", "", o.name.View()}
	}
	lines = append(lines, "")
	switch {
	case o.err != "":
		lines = append(lines, strings.Split(style.StatusErr.Width(max(w-4, 1)).Render(o.err), "\n")...)
	case o.busy:
		lines = append(lines, style.StatusInfo.Render(o.busyText()))
	case o.note != "":
		lines = append(lines, style.Faint.Render(o.note))
	}
	return frame(style.ActiveTitle.Render(title), lines, w, h, true)
}

func (o onboarding) busyText() string {
	switch o.step {
	case stepTest:
		return "Signing in…"
	case stepRenovate:
		return "Looking for Renovate PRs…"
	case stepName:
		return "Saving…"
	}
	return "Checking…"
}

func (o onboarding) hints(k keyMap) []key.Binding {
	back := hint(k.Close, "esc", "back")
	if o.step == o.first {
		back = hint(k.Close, "esc", "cancel")
	}
	next := "continue"
	switch o.step {
	case stepType:
		return []key.Binding{hint(k.Down, "j/k", "move"), hint(k.Enter, "enter", "select"), back}
	case stepURL:
		next = "check"
	case stepSignIn:
		return []key.Binding{hint(k.Enter, "enter", next), hint(k.NextBox, "tab", "token/command"), back}
	case stepTest:
		if o.busy {
			return []key.Binding{back}
		}
		if o.f == nil {
			next = "retry"
		}
	case stepName:
		next = "save"
	}
	return []key.Binding{hint(k.Enter, "enter", next), back}
}
