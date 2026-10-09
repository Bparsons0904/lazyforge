package termimg

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi/kitty"
)

var seamKittyOK = uv.KittyGraphicsEvent{Options: kitty.Options{ID: 1}, Payload: []byte("OK")}

// Each case is a sequence of replies; the verdict must depend on the evidence, not on the order it arrives,
// and detection must report exactly once.
var seamDetectCases = []struct {
	name    string
	inTmux  bool
	replies []tea.Msg
	want    bool
}{
	{
		name: "kitty answers",
		replies: []tea.Msg{
			tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
			tea.TerminalVersionMsg{Name: "kitty(0.36.4)"},
			seamKittyOK,
			uv.CellSizeEvent{Width: 10, Height: 21},
			uv.PrimaryDeviceAttributesEvent{62, 22},
		},
		want: true,
	},
	{
		name: "DA1 before the profile",
		replies: []tea.Msg{
			tea.TerminalVersionMsg{Name: "ghostty 1.1.3"},
			seamKittyOK,
			uv.PrimaryDeviceAttributesEvent{62, 22},
			tea.ColorProfileMsg{Profile: colorprofile.TrueColor},
		},
		want: true,
	},
	{
		name: "WezTerm answers the graphics query",
		replies: []tea.Msg{
			tea.TerminalVersionMsg{Name: "WezTerm 20240203"},
			seamKittyOK,
			tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
			uv.PrimaryDeviceAttributesEvent{62, 22},
		},
		want: false,
	},
	{
		name: "graphics reply after DA1",
		replies: []tea.Msg{
			tea.TerminalVersionMsg{Name: "kitty(0.36.4)"},
			uv.PrimaryDeviceAttributesEvent{62, 22},
			seamKittyOK,
			tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
		},
		want: false,
	},
	{
		name: "timeout before DA1",
		replies: []tea.Msg{
			tea.TerminalVersionMsg{Name: "kitty(0.36.4)"},
			seamKittyOK,
			tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
			timeoutMsg{},
		},
		want: true,
	},
	{
		name:   "tmux with passthrough and ghostty",
		inTmux: true,
		replies: []tea.Msg{
			probedMsg{passthrough: "on", clientTerm: "ghostty 1.1.3"},
			tea.ColorProfileMsg{Profile: colorprofile.ANSI256},
			uv.PrimaryDeviceAttributesEvent{62, 22},
		},
		want: true,
	},
	{
		name:   "tmux with passthrough off",
		inTmux: true,
		replies: []tea.Msg{
			probedMsg{passthrough: "off", clientTerm: "kitty"},
		},
		want: false,
	},
}

func TestSeamDetectorVerdict(t *testing.T) {
	for _, tc := range seamDetectCases {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDetector(Probes{InTmux: tc.inTmux}, time.Second)
			var reports []DetectedMsg
			for _, msg := range tc.replies {
				var cmd tea.Cmd
				d, cmd = d.Update(msg)
				if cmd == nil {
					continue
				}
				if m, ok := cmd().(DetectedMsg); ok {
					reports = append(reports, m)
				}
			}
			if len(reports) != 1 {
				t.Fatalf("got %d DetectedMsg, want 1", len(reports))
			}
			if got := reports[0].Support.OK; got != tc.want {
				t.Errorf("OK = %v, want %v", got, tc.want)
			}
			if _, cmd := d.Update(uv.PrimaryDeviceAttributesEvent{62}); cmd != nil {
				t.Error("a reply after the report returned a command")
			}
		})
	}
}
