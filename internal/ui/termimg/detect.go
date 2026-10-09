package termimg

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
)

// Probes is the environment and tmux state that detection reads.
type Probes struct {
	InTmux      bool                                      // TMUX is set
	Passthrough func(ctx context.Context) (string, error) // `tmux show -gv allow-passthrough`
	ClientTerm  func(ctx context.Context) (string, error) // `tmux display -p '#{client_termtype}'`
}

// SystemProbes reads TMUX from the environment and runs two read-only tmux commands.
func SystemProbes() Probes {
	return Probes{
		InTmux: os.Getenv("TMUX") != "",
		Passthrough: func(ctx context.Context) (string, error) {
			return tmuxOutput(ctx, "show", "-gv", "allow-passthrough")
		},
		ClientTerm: func(ctx context.Context) (string, error) {
			return tmuxOutput(ctx, "display", "-p", "#{client_termtype}")
		},
	}
}

func tmuxOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// DetectedMsg reports the finished detection; it is sent exactly once per Detector.
type DetectedMsg struct{ Support Support }

// Detector runs once per program and emits one DetectedMsg; the zero value detects nothing.
type Detector struct {
	probes  Probes
	timeout time.Duration
	tmux    bool
	state   detectState

	name        string
	graphics    bool
	da1         bool
	profile     colorprofile.Profile
	haveProfile bool
	cell        uv.Size
}

type detectState int

const (
	stateIdle detectState = iota
	stateQuerying
	stateProbing
	stateTmuxQuerying
	stateDone
)

const (
	defaultCellW = 8
	defaultCellH = 16
)

// DA1 goes last: almost every terminal answers it, so its reply ends the wait for the others.
const outsideQuery = "\x1b[>q\x1b_Gi=1,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[16t\x1b[c"

const tmuxQuery = "\x1b[16t\x1b[c"

type probedMsg struct {
	passthrough, clientTerm string
	err                     error
}

type timeoutMsg struct{}

// NewDetector returns a detector that reports within timeout even when the terminal never answers.
func NewDetector(p Probes, timeout time.Duration) Detector {
	d := Detector{probes: p, timeout: timeout, tmux: p.InTmux, state: stateQuerying}
	if p.InTmux {
		d.state = stateProbing
	}
	return d
}

// Start returns the commands that begin detection. It doesn't change d; Update does.
func (d Detector) Start() tea.Cmd {
	switch d.state {
	case stateQuerying:
		return tea.Batch(tea.Raw(outsideQuery), d.timer())
	case stateProbing:
		return tea.Batch(d.probe(), d.timer())
	}
	return nil
}

// Update takes the terminal replies detection needs and returns a command yielding DetectedMsg once it has its answer.
func (d Detector) Update(msg tea.Msg) (Detector, tea.Cmd) {
	if d.state == stateIdle || d.state == stateDone {
		return d, nil
	}
	switch msg := msg.(type) {
	case probedMsg:
		if d.state != stateProbing {
			return d, nil
		}
		return d.probed(msg)
	case timeoutMsg:
		if d.state == stateProbing {
			return d.finish(false)
		}
		return d.finish(d.ok())
	case tea.ColorProfileMsg:
		d.profile, d.haveProfile = msg.Profile, true
	case tea.TerminalVersionMsg:
		if d.state == stateQuerying {
			d.name = msg.Name
		}
	case uv.KittyGraphicsEvent:
		if d.state == stateQuerying && !d.da1 {
			d.graphics = msg.Options.ID == 1 && string(msg.Payload) == "OK"
		}
	case uv.CellSizeEvent:
		d.cell = uv.Size(msg)
	case uv.PrimaryDeviceAttributesEvent:
		if d.state == stateQuerying || d.state == stateTmuxQuerying {
			d.da1 = true
		}
	default:
		return d, nil
	}
	if d.da1 && d.haveProfile {
		return d.finish(d.ok())
	}
	return d, nil
}

// probe runs in a command so that Update never waits on tmux.
func (d Detector) probe() tea.Cmd {
	probes, timeout := d.probes, d.timeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		pass, err := probes.Passthrough(ctx)
		if err != nil {
			return probedMsg{err: err}
		}
		term, err := probes.ClientTerm(ctx)
		return probedMsg{passthrough: pass, clientTerm: term, err: err}
	}
}

func (d Detector) timer() tea.Cmd {
	return tea.Tick(d.timeout, func(time.Time) tea.Msg { return timeoutMsg{} })
}

// probed starts the query only when both probes pass; any other result ends detection.
func (d Detector) probed(m probedMsg) (Detector, tea.Cmd) {
	if m.err != nil || (m.passthrough != "on" && m.passthrough != "all") || !allowlisted(m.clientTerm) {
		return d.finish(false)
	}
	d.state = stateTmuxQuerying
	return d, tea.Raw(tmuxQuery)
}

// finish ends detection with the verdict ok and returns the command that reports it.
func (d Detector) finish(ok bool) (Detector, tea.Cmd) {
	s := Support{OK: ok, Tmux: d.tmux, CellW: defaultCellW, CellH: defaultCellH}
	if d.cell.Width > 0 && d.cell.Height > 0 {
		s.CellW, s.CellH = d.cell.Width, d.cell.Height
	}
	d.state = stateDone
	return d, func() tea.Msg { return DetectedMsg{Support: s} }
}

// ok is the verdict on the evidence so far. Inside tmux the probes vouch for the terminal, so only the profile counts.
func (d Detector) ok() bool {
	if d.tmux {
		return d.profileOK()
	}
	return d.graphics && allowlisted(d.name) && d.profileOK()
}

// colorprofile orders profiles from Unknown up to TrueColor, so >= means ANSI256 or better.
func (d Detector) profileOK() bool {
	return d.haveProfile && d.profile >= colorprofile.ANSI256
}

// The graphics answer alone is not enough: WezTerm answers it too, so the name must match.
func allowlisted(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "kitty") || strings.Contains(name, "ghostty")
}
