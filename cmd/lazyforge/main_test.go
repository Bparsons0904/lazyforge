package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
