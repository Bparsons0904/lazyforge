package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRealServiceErrors(t *testing.T) {
	const secret = "s3cr3t-token-value"
	tests := []struct {
		name     string
		config   string // empty means the config file does not exist
		host     string
		wantSubs []string
		notSubs  []string
	}{
		{
			name:     "no config",
			wantSubs: []string{"no config yet", "onboarding"},
		},
		{
			name: "two hosts without --host",
			config: `[hosts.a]
type = "forgejo"
url = "https://a.invalid"
token = "x"
[hosts.b]
type = "gitea"
url = "https://b.invalid"
token = "x"
`,
			wantSubs: []string{"pick a host with --host", "a, b"},
		},
		{
			name: "github has no adapter",
			config: `[hosts.gh]
type = "github"
token = "x"
`,
			wantSubs: []string{`host "gh"`, `type "github" has no adapter yet`},
		},
		{
			name: "token_cmd failure hides output",
			config: `[hosts.f]
type = "forgejo"
url = "https://f.invalid"
token_cmd = "echo ` + secret + `; exit 3"
`,
			wantSubs: []string{`host "f"`, "token_cmd failed"},
			notSubs:  []string{secret, "echo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "absent.toml")
			if tt.config != "" {
				path = writeConfig(t, tt.config)
			}
			svc, err := realService(context.Background(), path, tt.host, true)
			if err == nil || svc != nil {
				t.Fatalf("realService = %v, %v; want error", svc, err)
			}
			for _, s := range tt.wantSubs {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q lacks %q", err, s)
				}
			}
			for _, s := range tt.notSubs {
				if strings.Contains(err.Error(), s) {
					t.Errorf("error %q leaks %q", err, s)
				}
			}
		})
	}
}

// S1-13: the selected host's require_green_ci, with its per-repo override, reaches core.Options.
func TestRealServiceWiresGreenCI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"1.22.0"}`)) })
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"login":"bob"}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tests := []struct {
		name         string
		requireGreen string
		wantOn       bool
		wantOverride bool
	}{
		{"host on, repo override off", "true", true, false},
		{"host off, repo override on", "false", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, `[hosts.f]
type = "forgejo"
url = "`+srv.URL+`"
token = "x"
require_green_ci = `+tt.requireGreen+`
[hosts.f.repos."o/over"]
require_green_ci = `+fmt.Sprint(tt.wantOverride)+`
`)
			svc, err := realService(context.Background(), path, "", true)
			if err != nil {
				t.Fatal(err)
			}
			if got := svc.RequiresGreenCI(domain.RepoRef{Owner: "o", Name: "plain"}); got != tt.wantOn {
				t.Errorf("host setting: got %v, want %v", got, tt.wantOn)
			}
			if got := svc.RequiresGreenCI(domain.RepoRef{Owner: "o", Name: "over"}); got != tt.wantOverride {
				t.Errorf("repo override: got %v, want %v", got, tt.wantOverride)
			}
		})
	}
}
