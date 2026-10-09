package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

// imgFlat runs cmd and returns its messages in order, expanding batches and sequences. A command still running after
// 500ms, such as the session's refresh tick, is dropped, as appExec drops it.
func imgFlat(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return imgExpand(msg)
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

// imgExpand returns the messages of one command result, running the commands a batch or sequence holds.
func imgExpand(msg tea.Msg) []tea.Msg {
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, imgFlat(c)...)
		}
		return out
	}
	if cmds, ok := sequenceCmds(msg); ok {
		var out []tea.Msg
		for _, c := range cmds {
			out = append(out, imgFlat(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// imgRun delivers msgs to a and every message their commands produce, the way a running program does.
func imgRun(t *testing.T, a App, msgs ...tea.Msg) App {
	for steps := 0; len(msgs) > 0; steps++ {
		if steps > maxSettleSteps {
			t.Fatal("messages did not settle: a command keeps producing messages")
		}
		next, cmd := a.Update(msgs[0])
		a, msgs = next.(App), append(msgs[1:], imgFlat(cmd)...)
	}
	return a
}

// imgPress sends each key to a and delivers what it produces.
func imgPress(t *testing.T, a App, keys ...string) App {
	for _, k := range keys {
		a = imgRun(t, a, keyMsg(k))
	}
	return a
}

// isImageLoad reports whether msg is an image load, stamped by App or not.
func isImageLoad(msg tea.Msg) bool {
	if s, ok := msg.(stampedMsg); ok {
		msg = s.msg
	}
	_, ok := msg.(imageLoadedMsg)
	return ok
}

// imgRunQuiet is imgRun that leaves image loads pending: it never feeds a load back, so a test decides when it lands.
func imgRunQuiet(t *testing.T, a App, msgs ...tea.Msg) App {
	for steps := 0; len(msgs) > 0; steps++ {
		if steps > maxSettleSteps {
			t.Fatal("messages did not settle: a command keeps producing messages")
		}
		next, cmd := a.Update(msgs[0])
		a, msgs = next.(App), msgs[1:]
		for _, out := range imgFlat(cmd) {
			if !isImageLoad(out) {
				msgs = append(msgs, out)
			}
		}
	}
	return a
}

// imgPressQuiet sends each key to a with imgRunQuiet.
func imgPressQuiet(t *testing.T, a App, keys ...string) App {
	for _, k := range keys {
		a = imgRunQuiet(t, a, keyMsg(k))
	}
	return a
}

// appLoads counts the image loads among msgs, whether or not App stamped them with the session's generation.
func appLoads(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if s, ok := msg.(stampedMsg); ok {
			msg = s.msg
		}
		if _, ok := msg.(imageLoadedMsg); ok {
			n++
		}
	}
	return n
}

// shotApp returns an App whose session is host a over f, with images allowed by the config, sized 120×40 and with
// the PR's details open. Host b connects to a fake holding the same PR, so its image has the same key.
func shotApp(t *testing.T, f *forgetest.Fake) App {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		Images: config.Images{Show: true},
		Hosts: map[string]config.Host{
			"a": {Type: "forgejo", URL: "https://h", Token: "t"},
			"b": {Type: "forgejo", URL: "https://b.example", Token: "t"},
		},
	}
	connect := func(context.Context, config.Host) (forge.Forge, error) {
		return shotFake(shotBody, forgetest.DemoPNG), nil
	}
	a := NewApp(context.Background(), Deps{
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state.toml"),
		Config:     cfg,
		Host:       "a",
		Forge:      f,
		Connect:    connect,
	})
	a = imgRun(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
	a = imgRun(t, a, imgFlat(a.Init())...)
	return imgPress(t, a, "j", "l", "l")
}

// shotPlacedApp returns shotApp with the PR's image drawn, as a detected terminal leaves it.
func shotPlacedApp(t *testing.T) App {
	t.Helper()
	a := shotApp(t, shotFake(shotBody, forgetest.DemoPNG))
	return imgRun(t, a, termimg.DetectedMsg{Support: testSupport})
}

func TestDetectedTerminalDrawsTheDetailsImage(t *testing.T) {
	a := shotPlacedApp(t)
	e := needShotEntry(t, a.session)
	if e == nil || e.state != imageReady {
		t.Fatalf("entry = %+v, want ready", e)
	}
	wantCells(t, a.View().Content, e.id, e.cols, e.rows)
}

func TestDetectedTerminalLoadsOnce(t *testing.T) {
	a := shotApp(t, shotFake(shotBody, forgetest.DemoPNG))
	_, cmd := a.Update(termimg.DetectedMsg{Support: testSupport})
	msgs := imgFlat(cmd)
	if n := appLoads(msgs); n != 1 || len(msgs) != 1 {
		t.Fatalf("detection sent %d messages with %d loads, want the one load", len(msgs), n)
	}
}

func TestSettingsToggleReleasesAndRedraws(t *testing.T) {
	a := shotPlacedApp(t)
	e := needShotEntry(t, a.session)
	id, cols, rows := e.id, e.cols, e.rows

	a = imgPress(t, a, "S", "j", "j", "j", "j", "j")
	next, cmd := a.Update(keyMsg("enter"))
	a = next.(App)
	msgs := imgFlat(cmd)
	if a.cfg().Images.Show {
		t.Fatal("the toggle left images on")
	}
	if got := rawsOf(msgs); !slices.Equal(got, []string{termimg.Delete(id, false)}) {
		t.Fatalf("toggle off sent %q, want the Delete of %d", got, id)
	}
	a = imgRun(t, a, msgs...)
	a = imgPress(t, a, "esc")
	view := a.View().Content
	if strings.Contains(view, termimg.Cells(id, cols, rows)[0]) {
		t.Fatal("placeholders still show with images off")
	}
	if !strings.Contains(strip(view), shotLink) {
		t.Fatalf("the link is missing with images off:\n%s", strip(view))
	}

	a = imgPress(t, a, "S", "j", "j", "j", "j", "j")
	next, cmd = a.Update(keyMsg("enter"))
	a = next.(App)
	msgs = imgFlat(cmd)
	if !a.cfg().Images.Show {
		t.Fatal("the toggle did not turn images back on")
	}
	if n := appLoads(msgs); n != 1 {
		t.Fatalf("toggle on started %d loads, want 1", n)
	}
	if got := rawsOf(msgs); len(got) != 0 {
		t.Fatalf("toggle on sent %q, want no Delete", got)
	}
}

func TestQuitKeepsReleasingHeldImages(t *testing.T) {
	a := shotPlacedApp(t)
	e := needShotEntry(t, a.session)
	want := termimg.Delete(e.id, false)
	if got := a.ReleaseImages(); got != want {
		t.Fatalf("ReleaseImages = %q, want %q", got, want)
	}
	for _, k := range []string{"q", "ctrl+c"} {
		next, cmd := a.Update(keyMsg(k))
		if cmd == nil {
			t.Fatalf("%s returned no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", k)
		}
		if got := next.(App).ReleaseImages(); got != want {
			t.Errorf("after %s, ReleaseImages = %q, want %q", k, got, want)
		}
	}
}

func TestPickingTheLiveHostKeepsImagesHeld(t *testing.T) {
	a := shotPlacedApp(t)
	id := needShotEntry(t, a.session).id
	a = imgPress(t, a, "h", "h", "h")
	if a.screen != screenPicker {
		t.Fatalf("screen %v, want the picker", a.screen)
	}
	next, cmd := a.Update(keyMsg("l"))
	a = next.(App)
	var raws []string
	for _, msg := range imgFlat(cmd) {
		next, cmd := a.Update(msg)
		a = next.(App)
		raws = append(raws, rawsOf(imgFlat(cmd))...)
	}
	if len(raws) != 0 {
		t.Fatalf("picking the live host sent %q", raws)
	}
	if a.screen != screenSession {
		t.Fatalf("screen %v after picking the live host, want the session", a.screen)
	}
	if got := a.ReleaseImages(); got != termimg.Delete(id, false) {
		t.Fatalf("ReleaseImages = %q, want the held image", got)
	}
}

func TestReleaseIsEmptyWithoutASession(t *testing.T) {
	dir := t.TempDir()
	a := NewApp(context.Background(), Deps{ConfigPath: filepath.Join(dir, "config.toml"), StatePath: filepath.Join(dir, "state.toml")})
	if got := a.ReleaseImages(); got != "" {
		t.Fatalf("ReleaseImages without a session = %q", got)
	}
}

func TestSessionSwitchDeletesHeldImagesFirst(t *testing.T) {
	a := shotPlacedApp(t)
	e := needShotEntry(t, a.session)
	id, cols, rows := e.id, e.cols, e.rows
	oldSet, oldGen := a.session.details.img, a.gen

	a = imgPress(t, a, "h", "h", "h", "j")
	next, cmd := a.Update(keyMsg("l"))
	a = next.(App)
	conn := imgFlat(cmd)
	if len(conn) != 1 {
		t.Fatalf("connecting sent %d messages, want the connect result", len(conn))
	}
	next, cmd = a.Update(conn[0])
	a = next.(App)
	msgs := imgFlat(cmd)
	if len(msgs) == 0 {
		t.Fatal("switching hosts sent nothing")
	}
	if raw, ok := msgs[0].(tea.RawMsg); !ok || raw.Msg != termimg.Delete(id, false) {
		t.Fatalf("first message after the switch = %#v, want the Delete of %d", msgs[0], id)
	}
	if a.host != "b" {
		t.Fatalf("host = %q after the switch, want b", a.host)
	}

	img := decodePNG(t, forgetest.DemoPNG)
	next, cmd = a.Update(imageLoadedMsg{set: oldSet, key: shotKey, img: img})
	a = next.(App)
	if got := rawsOf(imgFlat(cmd)); len(got) != 0 {
		t.Fatalf("a late load from the old session sent %q", got)
	}
	next, cmd = a.Update(stampedMsg{gen: oldGen, msg: imageLoadedMsg{set: oldSet, key: shotKey, img: img}})
	a = next.(App)
	if got := rawsOf(imgFlat(cmd)); len(got) != 0 {
		t.Fatalf("a stamped late load from the old session sent %q", got)
	}
	next, cmd = a.Update(imagePlacedMsg{set: oldSet, key: shotKey, id: id, cols: cols, rows: rows})
	a = next.(App)
	if got := rawsOf(imgFlat(cmd)); len(got) != 0 {
		t.Fatalf("a late placed message from the old session sent %q", got)
	}
	// Host b's PR is the same one, so its image has the same key. Open it without running the fetch.
	a = imgRunQuiet(t, a, msgs...)
	a = imgPressQuiet(t, a, "j", "l", "l")
	bSet := a.session.details.img
	if e := entryAt(a.session, shotKey); e == nil || e.state != imageLoading {
		t.Fatalf("host b entry = %+v, want its own fetch pending", e)
	}

	// Host a's fetch for that key must not reach host b's pending entry.
	next, cmd = a.Update(imageLoadedMsg{set: oldSet, key: shotKey, img: img})
	a = next.(App)
	if got := rawsOf(imgFlat(cmd)); len(got) != 0 {
		t.Fatalf("host a's fetch reached host b's pending entry and sent %q", got)
	}

	// Host b's own fetch lands and starts its Transmit, so it holds an ID while still unplaced.
	next, cmd = a.Update(imageLoadedMsg{set: bSet, key: shotKey, img: img})
	a = next.(App)
	imgFlat(cmd)
	if b := needShotEntry(t, a.session); b.id == 0 || b.state == imageReady {
		t.Fatalf("host b's Transmit did not start: %+v", b)
	}

	// Host a's placement, carrying the ID host b now holds, must not mark host b's image ready.
	held := needShotEntry(t, a.session).id
	next, cmd = a.Update(imagePlacedMsg{set: oldSet, key: shotKey, id: held, cols: cols, rows: rows})
	a = next.(App)
	if got := rawsOf(imgFlat(cmd)); len(got) != 0 {
		t.Fatalf("host a's placement sent %q into host b", got)
	}
	if e := needShotEntry(t, a.session); e.state == imageReady {
		t.Fatal("host a's placement marked host b's image ready")
	}
}
