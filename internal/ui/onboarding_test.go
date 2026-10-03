package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// obEnv is a scripted forge server: probe answers by URL, connect returns f or connectErr.
type obEnv struct {
	probeKind  forge.Kind
	probeErr   error
	probed     []string
	connectErr error
	connects   []config.Host
	f          *forgetest.Fake
}

func newObEnv() *obEnv {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: domain.RepoRef{Owner: "bob", Name: "one"}})
	f.AddRepo(domain.Repo{RepoRef: domain.RepoRef{Owner: "bob", Name: "two"}})
	return &obEnv{probeKind: forge.KindForgejo, f: f}
}

func (e *obEnv) probe(_ context.Context, u string) (forge.Kind, error) {
	e.probed = append(e.probed, u)
	return e.probeKind, e.probeErr
}

func (e *obEnv) connect(_ context.Context, h config.Host) (forge.Forge, error) {
	e.connects = append(e.connects, h)
	if e.connectErr != nil {
		return nil, e.connectErr
	}
	return e.f, nil
}

func (e *obEnv) start(s onboardStart) onboarding {
	if s.taken == nil {
		s.taken = func(n string) bool { return n == "home" || n == "example" }
	}
	o := newOnboarding(context.Background(), e.connect, e.probe, s)
	o, _ = obRun(o, tea.WindowSizeMsg{Width: 100, Height: 30})
	return o
}

// obRun feeds msgs and every message their commands produce back into o, returning everything produced.
func obRun(o onboarding, msgs ...tea.Msg) (onboarding, []tea.Msg) {
	var out []tea.Msg
	for len(msgs) > 0 {
		var cmd tea.Cmd
		o, cmd = o.update(msgs[0], defaultKeys())
		got := appExec(cmd)
		out, msgs = append(out, got...), append(msgs[1:], got...)
	}
	return o, out
}

func obKeys(o onboarding, keys ...string) (onboarding, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var got []tea.Msg
		o, got = obRun(o, keyMsg(k))
		out = append(out, got...)
	}
	return o, out
}

func obType(o onboarding, s string) onboarding {
	for _, r := range s {
		o, _ = obKeys(o, string(r))
	}
	return o
}

func obView(o onboarding) string { return strip(o.view(100, 28)) }

func has[T any](msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(T); ok {
			return true
		}
	}
	return false
}

// toSignIn walks an add flow to step 4 at https://git.example.com.
func (e *obEnv) toSignIn(t *testing.T) onboarding {
	t.Helper()
	o, _ := obKeys(e.start(onboardStart{}), "enter")
	o, _ = obKeys(obType(o, "git.example.com"), "enter")
	if o.step != stepSignIn {
		t.Fatalf("not on sign-in: step %d err %q", o.step, o.err)
	}
	return o
}

func TestOnboardForgeType(t *testing.T) {
	e := newObEnv()
	o := e.start(onboardStart{})
	v := obView(o)
	for _, want := range []string{"GitHub", "GitLab", "coming soon"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	o, _ = obKeys(o, "j", "down", "j")
	if o.cursor != 1 {
		t.Fatalf("cursor %d landed past Gitea", o.cursor)
	}
	o, _ = obKeys(o, "enter")
	if o.step != stepURL || o.kind != forge.KindGitea {
		t.Fatalf("step %d kind %q", o.step, o.kind)
	}
	o, _ = obKeys(e.start(onboardStart{}), "enter")
	if o.kind != forge.KindForgejo {
		t.Fatalf("kind %q", o.kind)
	}
}

func TestOnboardAddress(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind forge.Kind
		err  error
		want string // "" means step 4 is reached
	}{
		{"match", forge.KindForgejo, nil, ""},
		{"unknown kind", "", nil, ""},
		{"mismatch", forge.KindGitea, nil, "That server runs Gitea, not Forgejo"},
		{"not found", "", fmt.Errorf("probe: %w", forge.ErrNotFound), "No Forgejo or Gitea API at that address"},
		{"network", "", errors.New("dial tcp: refused"), "Can't reach the server: dial tcp: refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.probeKind, e.probeErr = tc.kind, tc.err
			o, _ := obKeys(e.start(onboardStart{}), "enter")
			o, _ = obKeys(obType(o, "git.example.com"), "enter")
			if len(e.probed) != 1 || e.probed[0] != "https://git.example.com" {
				t.Fatalf("probed %v", e.probed)
			}
			if tc.want == "" {
				if o.step != stepSignIn || o.url != "https://git.example.com" {
					t.Fatalf("step %d url %q err %q", o.step, o.url, o.err)
				}
				return
			}
			if o.step != stepURL || !strings.Contains(obView(o), tc.want) {
				t.Fatalf("step %d, view lacks %q:\n%s", o.step, tc.want, obView(o))
			}
		})
	}
	e := newObEnv()
	o, _ := obKeys(e.start(onboardStart{}), "enter")
	o, _ = obKeys(obType(o, "not a url"), "enter")
	if len(e.probed) != 0 || !strings.Contains(obView(o), "Enter a server address like https://git.example.com") {
		t.Fatalf("probed %v err %q", e.probed, o.err)
	}
}

func TestOnboardSignIn(t *testing.T) {
	e := newObEnv()
	o := e.toSignIn(t)
	o, _ = obKeys(o, "enter")
	if o.step != stepSignIn || o.err == "" {
		t.Fatalf("empty token: step %d err %q", o.step, o.err)
	}
	o = obType(o, "tok-ZQX-9431")
	v := obView(o)
	if strings.Contains(v, "tok-ZQX-9431") || strings.Contains(v, "ZQX") {
		t.Fatalf("token shown:\n%s", v)
	}
	for _, want := range []string{"read:user", "write:repository", "write:issue", "https://git.example.com/user/settings/applications"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
}

func TestOnboardConnectionTest(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
	if !strings.Contains(obView(o), "Signed in as bob · 2 repositories") {
		t.Fatalf("view:\n%s", obView(o))
	}
	if h := e.connects[0]; h.Token != "tok" || h.TokenCmd != "" || h.URL != "https://git.example.com" || h.Type != "forgejo" {
		t.Fatalf("host %+v", h)
	}

	e = newObEnv()
	o = e.toSignIn(t)
	o, _ = obKeys(o, "tab")
	obKeys(obType(o, "pass show x"), "enter")
	if h := e.connects[0]; h.TokenCmd != "pass show x" || h.Token != "" {
		t.Fatalf("command mode host %+v", h)
	}

	for _, tc := range []struct {
		name    string
		connect error
		list    error
		want    string
	}{
		{"rejected", fmt.Errorf("connect: %w", forge.ErrUnauthorized), nil, "The token was rejected. Check it's valid and has read:user."},
		{"no repo scope", nil, forge.ErrUnauthorized, "Signed in, but the token can't list repositories: it needs write:repository."},
		{"unreachable", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}, nil, "Can't reach the server: dial tcp: refused"},
		{"other", errors.New("token_cmd exited 1"), nil, "token_cmd exited 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.connectErr = tc.connect
			if tc.list != nil {
				e.f.FailNext(tc.list)
			}
			o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
			if o.step != stepTest || !strings.Contains(obView(o), tc.want) {
				t.Fatalf("step %d, view lacks %q:\n%s", o.step, tc.want, obView(o))
			}
			e.connectErr = nil
			o, _ = obKeys(o, "enter")
			if len(e.connects) != 2 || o.step != stepTest || o.f == nil {
				t.Fatalf("retry: %d connects, step %d, err %q", len(e.connects), o.step, o.err)
			}
		})
	}
}

func TestOnboardStaleConnect(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
	o, _ = obRun(o, signedInMsg{host: config.Host{Type: "forgejo", URL: "https://git.example.com", Token: "old"}, err: forge.ErrUnauthorized})
	if o.err != "" || o.f == nil {
		t.Fatalf("stale result applied: err %q", o.err)
	}
}

func TestOnboardRenovate(t *testing.T) {
	e := newObEnv()
	e.f.AddChangeRequest(domain.RepoRef{Owner: "bob", Name: "one"}, domain.ChangeRequest{Number: 1, Author: "renovate-bot", SourceBranch: "renovate/go"})
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	if o.step != stepRenovate || o.renovate.Value() != "renovate-bot" {
		t.Fatalf("step %d suggestion %q", o.step, o.renovate.Value())
	}

	o, _ = obKeys(obType(e.toSignIn(t), "tok"), "enter")
	o, cmd := o.update(keyMsg("enter"), defaultKeys())
	late := appExec(cmd)
	o = obType(o, "me")
	o, _ = obRun(o, late...)
	if o.renovate.Value() != "me" {
		t.Fatalf("typed value lost: %q", o.renovate.Value())
	}

	e = newObEnv()
	o, _ = obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	if !strings.Contains(obView(o), "No Renovate PRs found") {
		t.Fatalf("view:\n%s", obView(o))
	}
	o, _ = obKeys(o, "enter")
	if o.step != stepName {
		t.Fatalf("blank not accepted: step %d", o.step)
	}
}

func TestSuggestName(t *testing.T) {
	none := func(string) bool { return false }
	for in, want := range map[string]string{
		"https://git.bobparsons.dev": "bobparsons",
		"https://codeberg.org":       "codeberg",
		"https://code.corp.io":       "corp",
		"http://localhost:3000":      "localhost",
	} {
		if got := suggestName(in, none); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	taken := func(n string) bool { return n == "bobparsons" || n == "bobparsons-2" }
	if got := suggestName("https://git.bobparsons.dev", taken); got != "bobparsons-3" {
		t.Errorf("taken: %q", got)
	}
}

func TestOnboardName(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	o = obType(o, "bot")
	o, _ = obKeys(o, "enter")
	if o.step != stepName || o.name.Value() != "example-2" {
		t.Fatalf("step %d suggestion %q", o.step, o.name.Value())
	}
	for _, bad := range []string{"bad name!", "home"} {
		o.name.SetValue(bad)
		var out []tea.Msg
		o, out = obKeys(o, "enter")
		if o.err == "" || has[onboardDoneMsg](out) {
			t.Errorf("%q accepted", bad)
		}
	}
	o.name.SetValue("work")
	o, out := obKeys(o, "enter")
	var done onboardDoneMsg
	for _, m := range out {
		if d, ok := m.(onboardDoneMsg); ok {
			done = d
		}
	}
	want := config.Host{Type: "forgejo", URL: "https://git.example.com", Token: "tok", RenovateUser: "bot"}
	if done.name != "work" || done.replaces != "" || !reflect.DeepEqual(done.host, want) || done.f == nil {
		t.Fatalf("done %+v", done)
	}
	o.saveFailed(errors.New("disk full"))
	if o.step != stepName || !strings.Contains(obView(o), "Couldn't save: disk full") {
		t.Fatalf("save failure:\n%s", obView(o))
	}
}

func TestOnboardEdit(t *testing.T) {
	e := newObEnv()
	o := e.start(onboardStart{edit: "home", host: config.Host{Type: "gitea", URL: "https://h.example", TokenCmd: "pass x", RenovateUser: "bot"}})
	if o.step != stepURL || o.addr.Value() != "https://h.example" || !o.cmdMode || o.tokenCmd.Value() != "pass x" || o.kind != forge.KindGitea {
		t.Fatalf("edit prefill: %+v", o.start)
	}
	e.probeKind = forge.KindGitea
	o, _ = obKeys(o, "enter", "enter", "enter", "enter")
	if o.step != stepName || o.renovate.Value() != "bot" || o.name.Value() != "home" {
		t.Fatalf("step %d renovate %q name %q", o.step, o.renovate.Value(), o.name.Value())
	}
	_, out := obKeys(o, "enter")
	if !has[onboardDoneMsg](out) {
		t.Fatalf("own name rejected: %v", out)
	}
}

func TestOnboardEscBack(t *testing.T) {
	e := newObEnv()
	o := e.toSignIn(t)
	for _, want := range []onboardStep{stepURL, stepType} {
		o, _ = obKeys(o, "esc")
		if o.step != want {
			t.Fatalf("step %d, want %d", o.step, want)
		}
	}
	if _, out := obKeys(o, "esc"); !has[onboardCancelMsg](out) {
		t.Fatal("esc on first step didn't cancel")
	}
	if _, out := obKeys(e.start(onboardStart{welcome: true}), "esc"); !has[onboardCancelMsg](out) {
		t.Fatal("esc on welcome didn't cancel")
	}
	if _, out := obKeys(e.start(onboardStart{welcome: true}), "ctrl+c"); !has[tea.QuitMsg](out) {
		t.Fatal("ctrl+c didn't quit")
	}
}

func TestOnboardInputsTakeLetters(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(e.start(onboardStart{}), "enter")
	o, out := obKeys(o, "q", "S", "h", "j", "y")
	if o.step != stepURL || o.addr.Value() != "qShjy" || len(out) != 0 {
		t.Fatalf("step %d value %q msgs %v", o.step, o.addr.Value(), out)
	}
}

func TestOnboardEndToEnd(t *testing.T) {
	e := newObEnv()
	a, _ := testApp(t, Deps{Fresh: true, Probe: e.probe})
	a = appPress(t, a, "enter", "enter")
	for _, r := range "git.example.com" {
		a = appPress(t, a, string(r))
	}
	a = appPress(t, a, "enter", "t", "o", "k", "enter", "enter", "enter", "enter")
	if a.screen != screenSession || a.host != "example" {
		t.Fatalf("screen %v host %q onboarding err %q", a.screen, a.host, a.onboard.err)
	}
	fi, err := os.Stat(a.deps.ConfigPath)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v %v", fi, err)
	}
	c, err := config.Load(a.deps.ConfigPath)
	if err != nil || c.Hosts["example"].Token != "tok" || c.Hosts["example"].URL != "https://git.example.com" {
		t.Fatalf("loaded %+v %v", c, err)
	}
}

func TestOnboardCancelledRenovateResultKeepsLiveScan(t *testing.T) {
	e := newObEnv()
	e.f.AddChangeRequest(domain.RepoRef{Owner: "bob", Name: "one"}, domain.ChangeRequest{Number: 1, Author: "renovate-bot", SourceBranch: "renovate/go"})
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
	o, cmd := o.update(keyMsg("enter"), defaultKeys()) // step 6, scan in flight
	live := appExec(cmd)
	if !strings.Contains(obView(o), "Looking for Renovate PRs…") {
		t.Fatalf("scan not running:\n%s", obView(o))
	}
	for _, err := range []error{context.Canceled, fmt.Errorf("scan: %w", context.Canceled)} {
		o, _ = obRun(o, renovateSuggestedMsg{err: err})
		if !strings.Contains(obView(o), "Looking for Renovate PRs…") || o.note != "" {
			t.Fatalf("cancelled result clobbered the scan: note %q\n%s", o.note, obView(o))
		}
	}
	o, _ = obRun(o, live...)
	if o.renovate.Value() != "renovate-bot" {
		t.Fatalf("live result lost: %q", o.renovate.Value())
	}
}

func TestOnboardLongErrorWrapsToFrameWidth(t *testing.T) {
	words := make([]string, 40)
	for i := range words {
		words[i] = fmt.Sprintf("cause%02d", i)
	}
	e := newObEnv()
	e.connectErr = errors.New(strings.Join(words, " "))
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
	const w = 120
	view := strip(o.view(w, 28))
	widest := 0
	for _, l := range strings.Split(view, "\n") {
		widest = max(widest, len([]rune(l)))
	}
	if widest > w {
		t.Errorf("a line is %d columns, frame is %d", widest, w)
	}
	if widest <= 80 {
		t.Errorf("widest line %d: error wrapped at 80 instead of the frame width", widest)
	}
	for _, word := range words {
		if !strings.Contains(view, word) {
			t.Fatalf("error text cut off, lacks %q:\n%s", word, view)
		}
	}
}
