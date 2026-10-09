package termimg_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const (
	xtversionQuery = "\x1b[>q\x1b_Gi=1,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[16t\x1b[c"
	tmuxQuery      = "\x1b[16t\x1b[c"
	longTimeout    = time.Minute
	shortTimeout   = 300 * time.Millisecond
)

// fakeTmux stands in for the two tmux probes and counts how often each one runs.
type fakeTmux struct {
	passthrough, client string
	passErr, clientErr  error
	passCalls           atomic.Int32
	clientCalls         atomic.Int32
}

func (f *fakeTmux) probes(inTmux bool) termimg.Probes {
	return termimg.Probes{
		InTmux: inTmux,
		Passthrough: func(context.Context) (string, error) {
			f.passCalls.Add(1)
			return f.passthrough, f.passErr
		},
		ClientTerm: func(context.Context) (string, error) {
			f.clientCalls.Add(1)
			return f.client, f.clientErr
		},
	}
}

func profileMsg(p colorprofile.Profile) tea.Msg { return tea.ColorProfileMsg{Profile: p} }

func nameMsg(name string) tea.Msg { return tea.TerminalVersionMsg{Name: name} }

func graphicsReply(id int, payload string) uv.KittyGraphicsEvent {
	return uv.KittyGraphicsEvent{Options: kitty.Options{ID: id}, Payload: []byte(payload)}
}

func cellSize(w, h int) uv.CellSizeEvent { return uv.CellSizeEvent{Width: w, Height: h} }

func da1() uv.PrimaryDeviceAttributesEvent { return uv.PrimaryDeviceAttributesEvent{62, 22} }

// kittyReplies is what a kitty-like terminal answers outside tmux, in the order the App sees them.
func kittyReplies(name string) []tea.Msg {
	return []tea.Msg{
		profileMsg(colorprofile.ANSI256),
		nameMsg(name),
		graphicsReply(1, "OK"),
		cellSize(10, 21),
		da1(),
	}
}

// startCmds runs the commands of cmd concurrently and returns a channel of the messages they produce.
func startCmds(cmd tea.Cmd) <-chan tea.Msg {
	out := make(chan tea.Msg, 16)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if msg != nil {
				out <- msg
			}
		}()
	}
	run(cmd)
	return out
}

func nextMsg(t *testing.T, ch <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message from the detector's commands")
	}
	return nil
}

// drain runs cmd on this goroutine and returns the messages it produces, expanding batches.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, drain(sub)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// feed sends msg to d and returns the new detector with the messages Update's command produces.
func feed(d termimg.Detector, msg tea.Msg) (termimg.Detector, []tea.Msg) {
	next, cmd := d.Update(msg)
	return next, drain(cmd)
}

// replay feeds msgs in order and returns the detector with every message the replies produced.
func replay(d termimg.Detector, msgs ...tea.Msg) (termimg.Detector, []tea.Msg) {
	var out []tea.Msg
	for _, m := range msgs {
		var got []tea.Msg
		d, got = feed(d, m)
		out = append(out, got...)
	}
	return d, out
}

// detected returns the Support of the one DetectedMsg in msgs, failing unless there is exactly one.
func detected(t *testing.T, msgs []tea.Msg) termimg.Support {
	t.Helper()
	var found []termimg.Support
	for _, m := range msgs {
		if d, ok := m.(termimg.DetectedMsg); ok {
			found = append(found, d.Support)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d DetectedMsg, want 1 (messages %#v)", len(found), msgs)
	}
	return found[0]
}

func noDetected(t *testing.T, msgs []tea.Msg) {
	t.Helper()
	for _, m := range msgs {
		if _, ok := m.(termimg.DetectedMsg); ok {
			t.Fatalf("detection finished early: %#v", m)
		}
	}
}

func noRawMsg(t *testing.T, msgs []tea.Msg) {
	t.Helper()
	for _, m := range msgs {
		if raw, ok := m.(tea.RawMsg); ok {
			t.Fatalf("detector sent a query %q, want none", raw.Msg)
		}
	}
}

// startOutside starts a detector outside tmux and checks that its first command is the one combined query.
func startOutside(t *testing.T, timeout time.Duration) (termimg.Detector, <-chan tea.Msg) {
	t.Helper()
	d := termimg.NewDetector((&fakeTmux{}).probes(false), timeout)
	ch := startCmds(d.Start())
	raw, ok := nextMsg(t, ch).(tea.RawMsg)
	if !ok || raw.Msg != xtversionQuery {
		t.Fatalf("first command = %#v, want the XTVERSION, graphics, cell size and DA1 query", raw)
	}
	return d, ch
}

func TestDetectOutsideTmuxKittyLikeTerminalIsOK(t *testing.T) {
	for _, name := range []string{"kitty(0.36.4)", "ghostty 1.1.3", "KITTY 0.36.4"} {
		t.Run(name, func(t *testing.T) {
			d, _ := startOutside(t, longTimeout)
			_, msgs := replay(d, kittyReplies(name)...)
			want := termimg.Support{OK: true, Tmux: false, CellW: 10, CellH: 21}
			if got := detected(t, msgs); got != want {
				t.Errorf("Support = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDetectOutsideTmuxNotOK(t *testing.T) {
	cases := []struct {
		name    string
		replies []tea.Msg
	}{
		{"WezTerm answers the graphics query", []tea.Msg{
			profileMsg(colorprofile.ANSI256), nameMsg("WezTerm 20240203"), graphicsReply(1, "OK"), cellSize(10, 21), da1(),
		}},
		{"graphics reply arrives after DA1", []tea.Msg{
			profileMsg(colorprofile.ANSI256), nameMsg("kitty(0.36.4)"), da1(), graphicsReply(1, "OK"),
		}},
		{"graphics payload is an error", []tea.Msg{
			profileMsg(colorprofile.ANSI256), nameMsg("kitty(0.36.4)"), graphicsReply(1, "EINVAL:bad format"), da1(),
		}},
		{"graphics reply is for another ID", []tea.Msg{
			profileMsg(colorprofile.ANSI256), nameMsg("kitty(0.36.4)"), graphicsReply(2, "OK"), da1(),
		}},
		{"profile Unknown", []tea.Msg{
			profileMsg(colorprofile.Unknown), nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"), da1(),
		}},
		{"profile NoTTY", []tea.Msg{
			profileMsg(colorprofile.NoTTY), nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"), da1(),
		}},
		{"profile ASCII", []tea.Msg{
			profileMsg(colorprofile.ASCII), nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"), da1(),
		}},
		{"profile ANSI", []tea.Msg{
			profileMsg(colorprofile.ANSI), nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"), da1(),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := startOutside(t, longTimeout)
			_, msgs := replay(d, tc.replies...)
			if got := detected(t, msgs); got.OK {
				t.Errorf("OK = true, want false (%+v)", got)
			}
		})
	}
}

func TestDetectOutsideTmuxProfileFloorIsANSI256(t *testing.T) {
	for _, p := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.TrueColor} {
		t.Run(p.String(), func(t *testing.T) {
			d, _ := startOutside(t, longTimeout)
			_, msgs := replay(d, profileMsg(p), nameMsg("kitty"), graphicsReply(1, "OK"), da1())
			if got := detected(t, msgs); !got.OK {
				t.Errorf("OK = false, want true (%+v)", got)
			}
		})
	}
}

func TestDetectOutsideTmuxDefaultCellSize(t *testing.T) {
	d, _ := startOutside(t, longTimeout)
	_, msgs := replay(d, profileMsg(colorprofile.ANSI256), nameMsg("kitty"), graphicsReply(1, "OK"), da1())
	want := termimg.Support{OK: true, Tmux: false, CellW: 8, CellH: 16}
	if got := detected(t, msgs); got != want {
		t.Errorf("Support = %+v, want %+v", got, want)
	}
}

func TestDetectOutsideTmuxTimeoutKeepsEvidence(t *testing.T) {
	d, ch := startOutside(t, shortTimeout)
	d, msgs := replay(d, profileMsg(colorprofile.ANSI256), nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"))
	noDetected(t, msgs)
	_, msgs = feed(d, nextMsg(t, ch))
	want := termimg.Support{OK: true, Tmux: false, CellW: 8, CellH: 16}
	if got := detected(t, msgs); got != want {
		t.Errorf("Support after timeout = %+v, want %+v", got, want)
	}
}

func TestDetectOutsideTmuxDA1WaitsForProfile(t *testing.T) {
	d, _ := startOutside(t, longTimeout)
	d, msgs := replay(d, nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK"), cellSize(10, 21), da1())
	noDetected(t, msgs)
	_, msgs = feed(d, profileMsg(colorprofile.ANSI256))
	want := termimg.Support{OK: true, Tmux: false, CellW: 10, CellH: 21}
	if got := detected(t, msgs); got != want {
		t.Errorf("Support = %+v, want %+v", got, want)
	}
}

func TestDetectOutsideTmuxTimeoutWithoutProfileIsNotOK(t *testing.T) {
	for _, withDA1 := range []bool{false, true} {
		name := "no DA1"
		if withDA1 {
			name = "DA1 seen"
		}
		t.Run(name, func(t *testing.T) {
			d, ch := startOutside(t, shortTimeout)
			replies := []tea.Msg{nameMsg("kitty(0.36.4)"), graphicsReply(1, "OK")}
			if withDA1 {
				replies = append(replies, da1())
			}
			d, msgs := replay(d, replies...)
			noDetected(t, msgs)
			_, msgs = feed(d, nextMsg(t, ch))
			if got := detected(t, msgs); got.OK {
				t.Errorf("OK = true, want false (%+v)", got)
			}
		})
	}
}

func TestDetectOutsideTmuxDetectsOnce(t *testing.T) {
	d, ch := startOutside(t, shortTimeout)
	d, msgs := replay(d, kittyReplies("kitty")...)
	detected(t, msgs)
	timeout := nextMsg(t, ch)
	for _, msg := range []tea.Msg{da1(), profileMsg(colorprofile.ANSI256), timeout} {
		if _, cmd := d.Update(msg); cmd != nil {
			t.Errorf("Update(%#v) after detection returned a command, want nil", msg)
		}
	}
}

func TestDetectOutsideTmuxUpdateWithoutStart(t *testing.T) {
	d := termimg.NewDetector((&fakeTmux{}).probes(false), longTimeout)
	_, msgs := replay(d, kittyReplies("ghostty 1.1.3")...)
	want := termimg.Support{OK: true, Tmux: false, CellW: 10, CellH: 21}
	if got := detected(t, msgs); got != want {
		t.Errorf("Support = %+v, want %+v", got, want)
	}
}

func TestDetectorIgnoresUnrelatedMessages(t *testing.T) {
	d := termimg.NewDetector((&fakeTmux{}).probes(false), longTimeout)
	if _, cmd := d.Update("unrelated"); cmd != nil {
		t.Error("Update of an unrelated message returned a command, want nil")
	}
}

func TestZeroDetectorIsInert(t *testing.T) {
	var d termimg.Detector
	if cmd := d.Start(); cmd != nil {
		t.Fatal("Start of the zero Detector returned a command, want nil")
	}
	// The detector is threaded through the replies so a reply the zero value recorded
	// would show up when a later one completes the answer.
	for _, msg := range append(kittyReplies("kitty"), "unrelated") {
		var cmd tea.Cmd
		d, cmd = d.Update(msg)
		if cmd != nil {
			t.Errorf("Update(%#v) of the zero Detector returned a command, want nil", msg)
		}
	}
}

func TestDetectInsideTmuxKittyLikeClientIsOK(t *testing.T) {
	cases := []struct{ passthrough, client string }{
		{"on", "xterm-kitty"},
		{"all", "xterm-ghostty"},
		{"on", "KITTY 0.36.4"},
	}
	for _, tc := range cases {
		t.Run(tc.passthrough+" "+tc.client, func(t *testing.T) {
			tmux := &fakeTmux{passthrough: tc.passthrough, client: tc.client}
			d := termimg.NewDetector(tmux.probes(true), longTimeout)
			ch := startCmds(d.Start())
			probe := nextMsg(t, ch)
			noRawMsg(t, []tea.Msg{probe})
			d, msgs := feed(d, probe)
			if len(msgs) != 1 {
				t.Fatalf("probe passed with %d messages, want the one query (%#v)", len(msgs), msgs)
			}
			if raw, ok := msgs[0].(tea.RawMsg); !ok || raw.Msg != tmuxQuery {
				t.Fatalf("probe passed and sent %#v, want the query %q", msgs[0], tmuxQuery)
			}
			_, msgs = replay(d, profileMsg(colorprofile.ANSI256), da1())
			want := termimg.Support{OK: true, Tmux: true, CellW: 8, CellH: 16}
			if got := detected(t, msgs); got != want {
				t.Errorf("Support = %+v, want %+v", got, want)
			}
			if n := tmux.passCalls.Load(); n != 1 {
				t.Errorf("Passthrough ran %d times, want 1", n)
			}
			if n := tmux.clientCalls.Load(); n != 1 {
				t.Errorf("ClientTerm ran %d times, want 1", n)
			}
		})
	}
}

func TestDetectInsideTmuxIgnoresTerminalReplies(t *testing.T) {
	tmux := &fakeTmux{passthrough: "on", client: "xterm-kitty"}
	d := termimg.NewDetector(tmux.probes(true), longTimeout)
	ch := startCmds(d.Start())
	d, _ = feed(d, nextMsg(t, ch))
	_, msgs := replay(d,
		nameMsg("WezTerm 20240203"),
		graphicsReply(2, "EINVAL:nope"),
		profileMsg(colorprofile.ANSI256),
		da1(),
	)
	want := termimg.Support{OK: true, Tmux: true, CellW: 8, CellH: 16}
	if got := detected(t, msgs); got != want {
		t.Errorf("Support = %+v, want %+v", got, want)
	}
}

func TestDetectInsideTmuxFailedProbeFinishesAtOnce(t *testing.T) {
	cases := []struct {
		name        string
		passthrough string
		client      string
		passErr     error
		clientErr   error
	}{
		{name: "passthrough off", passthrough: "off", client: "xterm-kitty"},
		{name: "passthrough empty", passthrough: "", client: "xterm-kitty"},
		{name: "client is not kitty", passthrough: "on", client: "xterm-256color"},
		{name: "passthrough errors", passthrough: "on", client: "xterm-kitty", passErr: errors.New("no server running")},
		{name: "client errors", passthrough: "on", client: "", clientErr: errors.New("no client attached")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmux := &fakeTmux{passthrough: tc.passthrough, client: tc.client, passErr: tc.passErr, clientErr: tc.clientErr}
			d := termimg.NewDetector(tmux.probes(true), longTimeout)
			ch := startCmds(d.Start())
			_, msgs := feed(d, nextMsg(t, ch))
			noRawMsg(t, msgs)
			want := termimg.Support{OK: false, Tmux: true, CellW: 8, CellH: 16}
			if got := detected(t, msgs); got != want {
				t.Errorf("Support = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDetectInsideTmuxTimeoutBeforeDA1(t *testing.T) {
	cases := []struct {
		name    string
		replies []tea.Msg
		want    bool
	}{
		{"ANSI256 seen", []tea.Msg{profileMsg(colorprofile.ANSI256)}, true},
		{"TrueColor seen", []tea.Msg{profileMsg(colorprofile.TrueColor)}, true},
		{"ANSI seen", []tea.Msg{profileMsg(colorprofile.ANSI)}, false},
		{"no profile", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmux := &fakeTmux{passthrough: "on", client: "xterm-kitty"}
			d := termimg.NewDetector(tmux.probes(true), shortTimeout)
			ch := startCmds(d.Start())
			d, _ = feed(d, nextMsg(t, ch))
			d, msgs := replay(d, tc.replies...)
			noDetected(t, msgs)
			_, msgs = feed(d, nextMsg(t, ch))
			want := termimg.Support{OK: tc.want, Tmux: true, CellW: 8, CellH: 16}
			if got := detected(t, msgs); got != want {
				t.Errorf("Support after timeout = %+v, want %+v", got, want)
			}
		})
	}
}
