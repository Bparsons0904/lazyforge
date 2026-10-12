package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// toGitLab picks GitLab at step 2 of an add flow.
func (e *obEnv) toGitLab(t *testing.T) onboarding {
	t.Helper()
	o, _ := obKeys(e.start(onboardStart{}), "j", "j", "j", "enter")
	if o.step != stepURL || o.kind != forge.KindGitLab {
		t.Fatalf("step %d kind %q", o.step, o.kind)
	}
	return o
}

func TestOnboardGitLabAddress(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitLab
	o := e.toGitLab(t)
	if o.addr.Value() != "https://gitlab.com" {
		t.Fatalf("prefill %q:\n%s", o.addr.Value(), obView(o))
	}
	o, _ = obKeys(o, "enter")
	if o.step != stepSignIn || o.url != "https://gitlab.com" || e.probed[0] != "https://gitlab.com" {
		t.Fatalf("step %d url %q probed %v err %q", o.step, o.url, e.probed, o.err)
	}

	// A self-managed address replaces the prefill.
	o = e.toGitLab(t)
	o.addr.SetValue("https://git.corp.io")
	o, _ = obKeys(o, "enter")
	if o.step != stepSignIn || o.url != "https://git.corp.io" {
		t.Fatalf("step %d url %q err %q", o.step, o.url, o.err)
	}

	// Another type never inherits GitLab's prefill.
	o, _ = obKeys(e.toGitLab(t), "esc", "k", "k", "k", "enter")
	if o.kind != forge.KindForgejo || o.addr.Value() != "" {
		t.Fatalf("kind %q addr %q", o.kind, o.addr.Value())
	}

	o = e.start(onboardStart{edit: "gl", host: config.Host{Type: "gitlab", URL: "https://git.corp.io", Token: "t"}})
	if o.addr.Value() != "https://git.corp.io" || o.kind != forge.KindGitLab {
		t.Fatalf("edit prefill: kind %q addr %q", o.kind, o.addr.Value())
	}
}

func TestOnboardGitLabProbeMessages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		gitlab bool // chose GitLab; otherwise Forgejo
		kind   forge.Kind
		err    error
		want   string
	}{
		{"forgejo server for gitlab", true, forge.KindForgejo, nil, "That server runs Forgejo, not GitLab"},
		{"github server for gitlab", true, forge.KindGitHub, nil, "That server runs GitHub, not GitLab"},
		{"gitlab server for forgejo", false, forge.KindGitLab, nil, "That server runs GitLab, not Forgejo"},
		{"no api", true, "", fmt.Errorf("probe: %w", forge.ErrNotFound), "No GitLab API at that address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.probeKind, e.probeErr = tc.kind, tc.err
			o, _ := obKeys(e.start(onboardStart{}), "enter")
			if tc.gitlab {
				o = e.toGitLab(t)
			}
			o.addr.SetValue("https://code.example.com")
			o, _ = obKeys(o, "enter")
			if o.step != stepURL || !strings.Contains(obView(o), tc.want) {
				t.Fatalf("step %d, view lacks %q:\n%s", o.step, tc.want, obView(o))
			}
		})
	}
}

func TestOnboardGitLabSignIn(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitLab
	o, _ := obKeys(e.toGitLab(t), "enter")
	v := obView(o)
	for _, want := range []string{"https://gitlab.com/-/user_settings/personal_access_tokens", "api"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	for _, not := range []string{"read_api", "read:user", "write:repository", "/user/settings/applications", "/settings/tokens", "fine-grained"} {
		if strings.Contains(v, not) {
			t.Errorf("view shows another forge's %q", not)
		}
	}
	if strings.Contains(v, "PRs") || !strings.Contains(v, "MRs") {
		t.Errorf("GitLab merge requests should read MRs:\n%s", v)
	}

	// A self-managed address makes the token page live under that address.
	o = e.toGitLab(t)
	o.addr.SetValue("https://git.corp.io")
	o, _ = obKeys(o, "enter")
	if v := obView(o); !strings.Contains(v, "https://git.corp.io/-/user_settings/personal_access_tokens") {
		t.Errorf("view lacks the self-managed token page:\n%s", v)
	}

	for _, tc := range []struct {
		name    string
		connect error
		list    error
		want    string
	}{
		{"rejected", fmt.Errorf("connect: %w", forge.ErrUnauthorized), nil, "api"},
		{"can't list", nil, forge.ErrUnauthorized, "read_api is not enough"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newObEnv()
			e.probeKind, e.connectErr = forge.KindGitLab, tc.connect
			if tc.list != nil {
				e.f.FailNext(tc.list)
			}
			o, _ := obKeys(e.toGitLab(t), "enter")
			o, _ = obKeys(obType(o, "tok"), "enter")
			v := strip(o.view(80, 28))
			flat := strings.Join(strings.Fields(strings.ReplaceAll(v, "│", "")), " ")
			if o.step != stepTest || !strings.Contains(flat, tc.want) {
				t.Fatalf("step %d, view lacks the GitLab scope message:\n%s", o.step, v)
			}
			if strings.Contains(flat, "read:user") || strings.Contains(flat, "write:repository") || strings.Contains(flat, "repo needs") {
				t.Errorf("message names another forge's scope:\n%s", v)
			}
			for _, line := range strings.Split(v, "\n") {
				if w := lipgloss.Width(line); w > 80 {
					t.Errorf("line is %d wide: %q", w, line)
				}
			}
			if h := e.connects[0]; h.Type != "gitlab" || h.URL != "https://gitlab.com" {
				t.Fatalf("host %+v", h)
			}
		})
	}
}

func TestOnboardGitLabSignInFits80x24(t *testing.T) {
	e := newObEnv()
	e.probeKind = forge.KindGitLab
	o, _ := obKeys(e.toGitLab(t), "enter")
	v := strip(o.view(80, 24))
	lines := strings.Split(v, "\n")
	if len(lines) > 24 || !strings.Contains(v, "personal_access_tokens") {
		t.Fatalf("%d lines:\n%s", len(lines), v)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("%d columns: %q", w, l)
		}
	}
}
