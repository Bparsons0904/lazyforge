package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// toGitHub picks GitHub at step 2 of an add flow.
func (e *obEnv) toGitHub(t *testing.T) onboarding {
	t.Helper()
	o, _ := obKeys(e.start(onboardStart{}), "j", "j", "enter")
	if o.step != stepURL || o.kind != forge.KindGitHub {
		t.Fatalf("step %d kind %q", o.step, o.kind)
	}
	return o
}

func TestOnboardGitHubAddress(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitHub
	o := e.toGitHub(t)
	if o.addr.Value() != "https://github.com" || !strings.Contains(obView(o), "Address of your GitHub server") {
		t.Fatalf("prefill %q:\n%s", o.addr.Value(), obView(o))
	}
	o, _ = obKeys(o, "enter")
	if o.step != stepSignIn || o.url != "https://github.com" || e.probed[0] != "https://github.com" {
		t.Fatalf("step %d url %q probed %v err %q", o.step, o.url, e.probed, o.err)
	}

	// Going back to a different type drops the untouched prefill, but never a typed address.
	o, _ = obKeys(e.toGitHub(t), "esc", "k", "k", "enter")
	if o.kind != forge.KindForgejo || o.addr.Value() != "" {
		t.Fatalf("kind %q addr %q", o.kind, o.addr.Value())
	}
	o, _ = obKeys(o, "esc", "j", "j", "enter")
	if o.addr.Value() != "https://github.com" {
		t.Fatalf("prefill not restored: %q", o.addr.Value())
	}
	o = obType(o, "/x")
	o, _ = obKeys(o, "esc", "k", "k", "enter")
	if o.addr.Value() != "https://github.com/x" {
		t.Fatalf("typed address lost: %q", o.addr.Value())
	}

	o = e.start(onboardStart{edit: "gh", host: config.Host{Type: "github", URL: "https://ghe.corp.io", Token: "t"}})
	if o.addr.Value() != "https://ghe.corp.io" || o.kind != forge.KindGitHub {
		t.Fatalf("edit prefill: kind %q addr %q", o.kind, o.addr.Value())
	}
}

func TestOnboardGitHubProbeMessages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		github bool // chose GitHub; otherwise Forgejo
		kind   forge.Kind
		err    error
		want   string
	}{
		{"forgejo server for github", true, forge.KindForgejo, nil, "That server runs Forgejo, not GitHub"},
		{"github server for forgejo", false, forge.KindGitHub, nil, "That server runs GitHub, not Forgejo"},
		{"no api", true, "", fmt.Errorf("probe: %w", forge.ErrNotFound), "No GitHub API at that address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.probeKind, e.probeErr = tc.kind, tc.err
			o, _ := obKeys(e.start(onboardStart{}), "enter")
			if tc.github {
				o = e.toGitHub(t)
			}
			o.addr.SetValue("https://code.example.com")
			o, _ = obKeys(o, "enter")
			if o.step != stepURL || !strings.Contains(obView(o), tc.want) {
				t.Fatalf("step %d, view lacks %q:\n%s", o.step, tc.want, obView(o))
			}
		})
	}
}

func TestOnboardGitHubSignIn(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitHub
	o, _ := obKeys(e.toGitHub(t), "enter")
	v := obView(o)
	for _, want := range []string{
		"https://github.com/settings/tokens", "repo", "fine-grained",
		"Contents", "Pull requests", "Issues", "Actions, Checks", "Commit statuses",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	for _, not := range []string{"read:user", "write:repository", "/user/settings/applications"} {
		if strings.Contains(v, not) {
			t.Errorf("view shows Forgejo's %q", not)
		}
	}

	for _, tc := range []struct {
		name    string
		connect error
		list    error
		want    string
	}{
		{"rejected", fmt.Errorf("connect: %w", forge.ErrUnauthorized), nil, "The token was rejected. Check it's valid and not expired."},
		{"can't list", nil, forge.ErrUnauthorized, "Signed in, but the token can't list repositories: a classic token needs repo; a fine-grained one needs Contents, Pull requests and Issues."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.probeKind, e.connectErr = forge.KindGitHub, tc.connect
			if tc.list != nil {
				e.f.FailNext(tc.list)
			}
			o, _ := obKeys(e.toGitHub(t), "enter")
			o, _ = obKeys(obType(o, "tok"), "enter")
			// The message is longer than the pane, so compare with borders and line breaks folded, and
			// check that wrapping kept every line inside 80 columns.
			v := strip(o.view(80, 28))
			if o.step != stepTest || !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(v, "│", "")), " "), tc.want) {
				t.Fatalf("step %d, view lacks %q:\n%s", o.step, tc.want, v)
			}
			for _, line := range strings.Split(v, "\n") {
				if w := lipgloss.Width(line); w > 80 {
					t.Errorf("line is %d wide: %q", w, line)
				}
			}
			if h := e.connects[0]; h.Type != "github" || h.URL != "https://github.com" {
				t.Fatalf("host %+v", h)
			}
		})
	}
}

func TestOnboardGitHubSignInFits80x24(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitHub
	o, _ := obKeys(e.toGitHub(t), "enter")
	o, _ = obRun(o, tea.WindowSizeMsg{Width: 80, Height: 24})
	v := strip(o.view(80, 24))
	lines := strings.Split(v, "\n")
	if len(lines) > 24 || !strings.Contains(v, "Commit statuses") {
		t.Fatalf("%d lines:\n%s", len(lines), v)
	}
	for _, l := range lines {
		if w := len([]rune(l)); w > 80 {
			t.Errorf("%d columns: %q", w, l)
		}
	}
}
