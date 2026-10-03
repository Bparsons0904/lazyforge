package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAppDepsErrors(t *testing.T) {
	const secret = "s3cr3t-token-value"
	tests := []struct {
		name     string
		config   string // empty means the config file does not exist
		host     string
		wantSubs []string
		notSubs  []string
	}{
		{
			name: "unknown --host names the host",
			config: `[hosts.a]
type = "forgejo"
url = "https://a.invalid"
token = "x"
`,
			host:     "nope",
			wantSubs: []string{"nope"},
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
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			d, err := appDeps(context.Background(), writeConfig(t, tt.config), tt.host, true)
			if err == nil {
				t.Fatalf("appDeps = %+v; want error", d)
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

func TestAppDepsNoConfigIsFresh(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	d, err := appDeps(context.Background(), filepath.Join(t.TempDir(), "absent.toml"), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Fresh || !d.Config.Update.Check || d.Host != "" || d.Forge != nil {
		t.Errorf("deps = %+v; want Fresh, Update.Check, no host", d)
	}
}

func TestAppDepsTwoHostsNeedPicker(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := writeConfig(t, `[hosts.a]
type = "forgejo"
url = "https://a.invalid"
token = "x"
[hosts.b]
type = "gitea"
url = "https://b.invalid"
token = "x"
`)
	d, err := appDeps(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "" || d.Forge != nil || d.Fresh || len(d.Config.Hosts) != 2 {
		t.Errorf("deps = %+v; want picker (no host, no forge)", d)
	}
}

func TestAppDepsSingleHostConnects(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"1.22.0"}`)) })
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"login":"bob"}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	path := writeConfig(t, `[hosts.f]
type = "forgejo"
url = "`+srv.URL+`"
token = "x"
`)
	d, err := appDeps(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "f" || d.Forge == nil || d.Forge.Info().User != "bob" {
		t.Errorf("deps = %+v; want host f connected as bob", d)
	}
	if d.ConfigPath != path || d.StatePath == "" || d.Connect == nil || d.Probe == nil {
		t.Errorf("deps = %+v; want paths and funcs wired", d)
	}
}

func TestConnectGithubSkipsTokenCmd(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	_, err := connect(context.Background(), config.Host{Type: "github", TokenCmd: "touch " + marker})
	if err == nil || !strings.Contains(err.Error(), `type "github" has no adapter yet`) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("token_cmd ran for an unsupported type")
	}
}
