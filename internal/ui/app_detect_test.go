package ui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const detectQuery = "\x1b[>q\x1b_Gi=1,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[16t\x1b[c"

// notInTmux is a non-tmux probe set; detection never calls the tmux probes outside tmux.
func notInTmux() termimg.Probes {
	return termimg.Probes{
		Passthrough: func(context.Context) (string, error) { return "", nil },
		ClientTerm:  func(context.Context) (string, error) { return "", nil },
	}
}

// kittyReplies is what a kitty-like terminal answers outside tmux, in the order the App sees them.
func kittyReplies() []tea.Msg {
	return []tea.Msg{
		tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
		tea.TerminalVersionMsg{Name: "kitty(0.36.4)"},
		uv.KittyGraphicsEvent{Options: kitty.Options{ID: 1}, Payload: []byte("OK")},
		uv.CellSizeEvent{Width: 10, Height: 21},
		uv.PrimaryDeviceAttributesEvent{62, 22},
	}
}

func TestAppStoresDetectedSupport(t *testing.T) {
	a, _ := testApp(t, Deps{})
	want := termimg.Support{OK: true, CellW: 10, CellH: 21}
	a = appRun(t, a, termimg.DetectedMsg{Support: want})
	if a.support != want {
		t.Errorf("support = %+v, want %+v", a.support, want)
	}
}

func TestAppInitStartsDetection(t *testing.T) {
	det := termimg.NewDetector(notInTmux(), time.Minute)
	a, _ := testApp(t, Deps{Detect: det})
	var sentQuery bool
	for _, msg := range appExec(a.Init()) {
		if raw, ok := msg.(tea.RawMsg); ok && raw.Msg == detectQuery {
			sentQuery = true
		}
	}
	if !sentQuery {
		t.Fatal("Init did not send the detection query")
	}
	a = appRun(t, a, kittyReplies()...)
	if !a.support.OK {
		t.Errorf("support = %+v after kitty-like replies, want OK", a.support)
	}
}

func TestAppWithoutDetectSendsNoQuery(t *testing.T) {
	a, _ := testApp(t, Deps{})
	for _, msg := range appExec(a.Init()) {
		if raw, ok := msg.(tea.RawMsg); ok {
			t.Errorf("Init sent raw query %q with no detector configured", raw.Msg)
		}
	}
}
