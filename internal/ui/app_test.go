package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// appExec runs cmd and returns its messages, dropping commands that don't return quickly (the refresh tick).
func appExec(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, appExec(c)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func appRun(t *testing.T, a App, msgs ...tea.Msg) App {
	t.Helper()
	for len(msgs) > 0 {
		next, cmd := a.Update(msgs[0])
		a, msgs = next.(App), append(msgs[1:], appExec(cmd)...)
	}
	return a
}

func appPress(t *testing.T, a App, keys ...string) App {
	t.Helper()
	for _, k := range keys {
		a = appRun(t, a, keyMsg(k))
	}
	return a
}

type connectSpy struct{ calls []config.Host }

func (s *connectSpy) connect(_ context.Context, h config.Host) (forge.Forge, error) {
	s.calls = append(s.calls, h)
	if h.URL == "bad" {
		return nil, errors.New("connection refused")
	}
	return newDemo(), nil
}

func twoHosts() config.Config {
	return config.Config{Update: config.Update{Check: true}, Hosts: map[string]config.Host{
		"b": {Type: "forgejo", URL: "https://b.example", Token: "t"},
		"a": {Type: "forgejo", URL: "https://a.example", Token: "t"},
		"c": {Type: "forgejo", URL: "bad", Token: "t"},
	}}
}

func testApp(t *testing.T, d Deps) (App, *connectSpy) {
	t.Helper()
	spy := &connectSpy{}
	dir := t.TempDir()
	d.ConfigPath, d.StatePath, d.Connect = filepath.Join(dir, "config.toml"), filepath.Join(dir, "state.toml"), spy.connect
	a := NewApp(context.Background(), d)
	a = appRun(t, a, tea.WindowSizeMsg{Width: 100, Height: 30})
	return a, spy
}

func TestNewPickerCursor(t *testing.T) {
	c := twoHosts()
	c.DefaultHost = "c"
	for _, tc := range []struct {
		last string
		want int
	}{{"b", 1}, {"gone", 2}, {"", 2}} {
		if got := newPicker(c, tc.last).cursor; got != tc.want {
			t.Errorf("last %q: cursor %d, want %d", tc.last, got, tc.want)
		}
	}
	c.DefaultHost = ""
	if got := newPicker(c, "gone").cursor; got != 0 {
		t.Errorf("no default: cursor %d", got)
	}
}

func TestStartScreen(t *testing.T) {
	if a, _ := testApp(t, Deps{Fresh: true}); a.screen != screenOnboarding {
		t.Errorf("fresh: screen %v", a.screen)
	}
	if a, _ := testApp(t, Deps{Config: twoHosts()}); a.screen != screenPicker {
		t.Errorf("no host: screen %v", a.screen)
	}
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	a = appRun(t, a, appExec(a.Init())...)
	if a.screen != screenSession || !a.session.repos.loaded {
		t.Errorf("host: screen %v, repos loaded %v", a.screen, a.session.repos.loaded)
	}
}

func TestPickConnectsAndRemembers(t *testing.T) {
	a, spy := testApp(t, Deps{Config: twoHosts()})
	if err := config.SaveState(a.deps.StatePath, config.State{SkippedVersion: "v9"}); err != nil {
		t.Fatal(err)
	}
	a = appPress(t, a, "j", "l") // rows a, b, c, + Add host
	if len(spy.calls) != 1 || spy.calls[0].URL != "https://b.example" {
		t.Fatalf("connect calls %+v", spy.calls)
	}
	if a.screen != screenSession || a.host != "b" || !a.session.repos.loaded {
		t.Fatalf("screen %v host %q", a.screen, a.host)
	}
	s, err := config.LoadState(a.deps.StatePath)
	if err != nil || s.LastHost != "b" || s.SkippedVersion != "v9" {
		t.Fatalf("state %+v %v", s, err)
	}
	a = appPress(t, a, "h", "l") // back to the picker, pick b again
	if len(spy.calls) != 1 || a.screen != screenSession {
		t.Fatalf("same host reconnected: %d calls, screen %v", len(spy.calls), a.screen)
	}
	a = appPress(t, a, "h", "j", "l") // c fails
	if a.screen != screenPicker || a.host != "b" || !a.statusErr {
		t.Fatalf("failed connect: screen %v host %q status %q", a.screen, a.host, a.status)
	}
	a = appPress(t, a, "j", "l")
	if a.screen != screenOnboarding {
		t.Fatalf("+ Add host: screen %v", a.screen)
	}
}

func TestSessionKeyInterception(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	a = appRun(t, a, appExec(a.Init())...)
	a = appPress(t, a, "?", "h", "S")
	if a.screen != screenSession {
		t.Fatalf("keys leaked past help: screen %v", a.screen)
	}
	a = appPress(t, a, "esc", "j", "l", "S")
	if a.screen != screenSettings || a.settingsTo != screenSession {
		t.Fatalf("S from boxes: screen %v", a.screen)
	}
	a = appPress(t, a, "esc")
	if a.screen != screenSession || a.session.level != levelBoxes {
		t.Fatalf("close: screen %v level %v", a.screen, a.session.level)
	}
}

func TestStampDropsStaleSession(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	stale := appExec(stamp(a.gen, func() tea.Msg { return reposLoadedMsg{repos: []domain.Repo{{RepoRef: homelab}}} }))
	a = appPress(t, a, "h", "j", "l") // switch to b
	a = appRun(t, a, stale...)
	if len(a.session.repos.repos) == 1 {
		t.Fatal("stale reposLoadedMsg reached the new session")
	}
	if got := appExec(stamp(1, func() tea.Msg { return editorDoneMsg{} })); len(got) != 1 {
		t.Fatalf("non-session msg: %#v", got)
	} else if _, ok := got[0].(editorDoneMsg); !ok {
		t.Fatalf("non-session msg was wrapped: %#v", got[0])
	}
}

func TestLiveGreenCI(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	if a.session.svc.RequiresGreenCI(homelab) {
		t.Fatal("green CI on before the change")
	}
	next := a.cfg().Clone()
	h := next.Hosts["a"]
	h.RequireGreenCI = true
	next.Hosts["a"] = h
	a.apply(next)
	if !a.session.svc.RequiresGreenCI(homelab) {
		t.Fatal("change not seen by the live session")
	}
}

func TestSaves(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	first, second := a.cfg().Clone(), a.cfg().Clone()
	second.DefaultHost = "a"
	c1, c2 := a.save(first), a.save(second)
	a = appRun(t, a, appExec(c2)...)
	a = appRun(t, a, appExec(c1)...)
	got, err := config.Load(a.deps.ConfigPath)
	if err != nil || got.DefaultHost != "a" {
		t.Fatalf("file %+v %v", got, err)
	}
	if fi, _ := os.Stat(a.deps.ConfigPath); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	a.sv.path = t.TempDir() // a directory: the rename fails
	a = appRun(t, a, appExec(a.save(first))...)
	if !a.statusErr {
		t.Fatal("save error not shown")
	}
}

func TestOnboardDone(t *testing.T) {
	a, _ := testApp(t, Deps{Fresh: true})
	h := config.Host{Type: "forgejo", URL: "https://n.example", Token: "t"}
	a = appRun(t, a, onboardDoneMsg{name: "n", host: h, f: newDemo()})
	if got, err := config.Load(a.deps.ConfigPath); err != nil || got.Hosts["n"].URL != h.URL {
		t.Fatalf("file %+v %v", got, err)
	}
	if a.screen != screenSession || a.host != "n" || !a.session.repos.loaded || a.fresh {
		t.Fatalf("screen %v host %q", a.screen, a.host)
	}

	on := true
	c := twoHosts()
	c.DefaultHost = "a"
	a2 := c.Hosts["a"]
	a2.RequireGreenCI, a2.Repos = true, map[string]config.RepoSettings{"o/r": {RequireGreenCI: &on}}
	c.Hosts["a"] = a2
	a, _ = testApp(t, Deps{Config: c})
	a.openSettings()
	a.openOnboarding(a.ctx, onboardStart{edit: "a"})
	a = appRun(t, a, onboardDoneMsg{name: "z", replaces: "a", host: h})
	got := a.cfg()
	if _, ok := got.Hosts["a"]; ok || got.DefaultHost != "z" || !got.Hosts["z"].RequireGreenCI || got.Hosts["z"].Repos["o/r"].RequireGreenCI == nil {
		t.Fatalf("rename: %+v", got)
	}
	if a.screen != screenSettings {
		t.Fatalf("edit: screen %v", a.screen)
	}

	a, _ = testApp(t, Deps{Config: twoHosts()})
	a.sv.path = t.TempDir()
	a.openOnboarding(a.ctx, onboardStart{})
	a = appRun(t, a, onboardDoneMsg{name: "n", host: h, f: newDemo()})
	if _, ok := a.cfg().Hosts["n"]; ok || a.screen != screenOnboarding {
		t.Fatalf("failed save applied: screen %v", a.screen)
	}
}

// appKey sends one key without running the command it returns, so a test can deliver that result late.
func appKey(a App, k string) (App, tea.Cmd) {
	next, cmd := a.Update(keyMsg(k))
	return next.(App), cmd
}

func hostsCfg() config.Config {
	return config.Config{DefaultHost: "mid", Hosts: map[string]config.Host{
		"zeta":  {Type: "forgejo", URL: "https://zeta.test", Token: "t"},
		"alpha": {Type: "forgejo", URL: "https://alpha.test", Token: "t"},
		"mid":   {Type: "forgejo", URL: "https://mid.test", Token: "t"},
	}}
}

func TestPickerListsHostsSortedWithAddLast(t *testing.T) {
	a, _ := testApp(t, Deps{Config: hostsCfg()})
	v := strip(a.View().Content)
	last := -1
	for _, want := range []string{"https://alpha.test", "https://mid.test", "https://zeta.test", "+ Add host"} {
		i := strings.Index(v, want)
		if i < 0 || i < last {
			t.Fatalf("%q out of order or missing (at %d, previous %d):\n%s", want, i, last, v)
		}
		last = i
	}
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "(default)") != strings.Contains(l, "mid.test") {
			t.Errorf("default marker on the wrong row: %q", l)
		}
	}
	if !strings.Contains(v, "Hosts") {
		t.Errorf("breadcrumb lacks Hosts:\n%s", v)
	}

	empty, _ := testApp(t, Deps{Config: config.Config{}})
	ev := strip(empty.View().Content)
	if !strings.Contains(ev, "+ Add host") || strings.Contains(ev, "https://") {
		t.Errorf("empty picker:\n%s", ev)
	}
	empty = appPress(t, empty, "l")
	if empty.screen != screenOnboarding || empty.onboard.step != stepType {
		t.Errorf("add from empty picker: screen %v step %d", empty.screen, empty.onboard.step)
	}
}

func TestPickerCursorStartsOnLastThenDefaultThenFirst(t *testing.T) {
	for _, tc := range []struct {
		name, last, def string
		want            int
	}{
		{"last host wins", "zeta", "mid", 2},
		{"default when last unknown", "gone", "mid", 1},
		{"default when last empty", "", "zeta", 2},
		{"first row otherwise", "gone", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := hostsCfg()
			c.DefaultHost = tc.def
			a, _ := testApp(t, Deps{Config: c, LastHost: tc.last})
			if a.picker.cursor != tc.want {
				t.Fatalf("cursor %d, want %d", a.picker.cursor, tc.want)
			}
		})
	}
}

func TestPickerFromSessionReseedsOnCurrentHost(t *testing.T) {
	c := twoHosts()
	c.DefaultHost = "a"
	a, _ := testApp(t, Deps{Config: c, Host: "b", Forge: newDemo(), LastHost: "c"})
	a = appRun(t, a, appExec(a.Init())...)
	a = appPress(t, a, "h")
	if a.screen != screenPicker || a.picker.cursor != 1 {
		t.Fatalf("screen %v cursor %d, want the picker on row 1 (host b)", a.screen, a.picker.cursor)
	}
}

func TestConnectFailureNamesHostAndInnermostCause(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	a.deps.Connect = func(context.Context, config.Host) (forge.Forge, error) {
		return nil, fmt.Errorf("connect: %w", fmt.Errorf("dial: %w", errors.New("no route to host")))
	}
	a = appPress(t, a, "G", "k", "l") // host c
	if a.screen != screenPicker || a.status != "Can't connect to c: no route to host" || !a.statusErr {
		t.Fatalf("screen %v status %q err %v", a.screen, a.status, a.statusErr)
	}
}

// pendingConnectToB leaves the app with a connect to host b still in flight and returns its result message.
func pendingConnectToB(t *testing.T, a App) (App, tea.Msg) {
	t.Helper()
	a, _ = appKey(a, "j")
	a, cmd := appKey(a, "l")
	if a.connecting != "b" || cmd == nil {
		t.Fatalf("no connect in flight: connecting %q", a.connecting)
	}
	msgs := appExec(cmd)
	if len(msgs) != 1 {
		t.Fatalf("connect produced %v", msgs)
	}
	return a, msgs[0]
}

func TestStaleConnectIgnoredAfterPickingCurrentHost(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	a = appRun(t, a, appExec(a.Init())...)
	a = appPress(t, a, "h")
	a, late := pendingConnectToB(t, a)
	a = appPress(t, a, "k", "l") // back to a: same host, no reconnect
	if a.screen != screenSession || a.host != "a" {
		t.Fatalf("screen %v host %q", a.screen, a.host)
	}
	gen := a.gen
	a = appRun(t, a, late)
	if a.screen != screenSession || a.host != "a" || a.gen != gen {
		t.Fatalf("late connect applied: screen %v host %q gen %d->%d", a.screen, a.host, gen, a.gen)
	}
}

func TestConnectResultForRemovedHostIgnored(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	a, late := pendingConnectToB(t, a)
	a.apply(removeHost(a.cfg(), "b"))
	a = appRun(t, a, late)
	if a.screen != screenPicker || a.host != "" {
		t.Fatalf("screen %v host %q", a.screen, a.host)
	}
}

func TestAddHostDropsPendingConnect(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	a, late := pendingConnectToB(t, a)
	a = appPress(t, a, "G", "l")
	if a.screen != screenOnboarding {
		t.Fatalf("screen %v", a.screen)
	}
	a = appRun(t, a, late)
	if a.screen != screenOnboarding || a.host != "" {
		t.Fatalf("late connect applied: screen %v host %q", a.screen, a.host)
	}
}

func TestConnectSucceedingInSettingsStaysInSettings(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	a, late := pendingConnectToB(t, a)
	a = appPress(t, a, "S")
	if a.screen != screenSettings {
		t.Fatalf("screen %v", a.screen)
	}
	a = appRun(t, a, late)
	if a.screen != screenSettings || a.host != "b" || a.settingsTo != screenSession {
		t.Fatalf("screen %v host %q returns to %v", a.screen, a.host, a.settingsTo)
	}
	a = appPress(t, a, "esc")
	if a.screen != screenSession || a.host != "b" {
		t.Fatalf("after close: screen %v host %q", a.screen, a.host)
	}
}

func TestRemoveRefusedWhenHostBecomesActiveDuringConfirm(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts()})
	a, late := pendingConnectToB(t, a)
	a = appPress(t, a, "S", "j", "l", "j", "j", "enter") // host b, Remove, confirm prompt open
	a = appRun(t, a, late)
	if a.host != "b" {
		t.Fatalf("host %q", a.host)
	}
	a = appPress(t, a, "y")
	if _, ok := a.cfg().Hosts["b"]; !ok {
		t.Fatal("connected host was removed")
	}
	if v := strip(a.View().Content); !strings.Contains(v, "Switch to another host first") {
		t.Fatalf("no refusal shown:\n%s", v)
	}
}

func TestLiveGreenCIThroughSettings(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	a = appRun(t, a, appExec(a.Init())...)
	other := domain.RepoRef{Owner: "x", Name: "y"}
	if a.session.svc.RequiresGreenCI(homelab) {
		t.Fatal("green CI on before any change")
	}
	rowIdx := func(kind settingsRowKind, repo string) int {
		for i, r := range settingsRows(a.settingsView()) {
			if r.kind == kind && r.host == "a" && r.repo == repo {
				return i
			}
		}
		t.Fatalf("no row %v %q", kind, repo)
		return 0
	}
	move := func(to int) {
		for a.settings.cursor < to {
			a = appPress(t, a, "j")
		}
		for a.settings.cursor > to {
			a = appPress(t, a, "k")
		}
	}
	saved := func() config.Host {
		c, err := config.Load(a.deps.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		return c.Hosts["a"]
	}

	a = appPress(t, a, "S")
	move(rowIdx(rowRepoCI, homelab.String()))
	a = appPress(t, a, "enter") // inherit -> on
	if !a.session.svc.RequiresGreenCI(homelab) || a.session.svc.RequiresGreenCI(other) {
		t.Fatal("per-repo override not live (host level is off)")
	}
	if on := saved().Repos[homelab.String()].RequireGreenCI; on == nil || !*on {
		t.Fatalf("override not saved: %+v", saved().Repos)
	}

	a = appPress(t, a, "enter") // on -> off
	move(rowIdx(rowHostCI, ""))
	a = appPress(t, a, "enter")
	if !a.session.svc.RequiresGreenCI(other) || !saved().RequireGreenCI {
		t.Fatal("host toggle not live or not saved")
	}
	if a.session.svc.RequiresGreenCI(homelab) {
		t.Fatal("repo override off should beat host on")
	}
}
