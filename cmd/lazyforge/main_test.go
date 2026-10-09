package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
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
			name: "gitlab has no adapter",
			config: `[hosts.gl]
type = "gitlab"
url = "https://gl.invalid"
token = "x"
`,
			wantSubs: []string{`host "gl"`, `type "gitlab" has no adapter yet`},
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

// serve answers each "METHOD /path" with its body; anything else is a 404.
func serve(t *testing.T, routes map[string]string) string {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, body := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

var (
	githubRoutes = map[string]string{
		"GET /api/v3/user": `{"login":"octo"}`,
		"GET /api/v3/meta": `{"verifiable_password_authentication":false,"installed_version":"3.17.4"}`,
	}
	forgejoRoutes = map[string]string{"GET /api/v1/version": `{"version":"16.0.5+gitea-1.22.0"}`}
)

func TestConnectRoutesGithub(t *testing.T) {
	url := serve(t, githubRoutes)
	f, err := connect(context.Background(), config.Host{Type: "github", URL: url, Token: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if info := f.Info(); info.Kind != forge.KindGitHub || info.User != "octo" || info.Version != "3.17.4" {
		t.Errorf("info = %+v; want GitHub as octo", info)
	}
}

func TestProbe(t *testing.T) {
	signIn := http.NewServeMux()
	signIn.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	locked := httptest.NewServer(signIn)
	t.Cleanup(locked.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	// errUnreachable stands for "any error except ErrNotFound": the exact dial error varies by OS.
	errUnreachable := errors.New("unreachable")
	tests := []struct {
		name    string
		url     string
		want    forge.Kind
		wantErr error // nil: no error; errUnreachable: any non-ErrNotFound error; else matched with errors.Is
	}{
		{"github", serve(t, githubRoutes), forge.KindGitHub, nil},
		{"forgejo", serve(t, forgejoRoutes), forge.KindForgejo, nil},
		{"gitea", serve(t, map[string]string{"GET /api/v1/version": `{"version":"1.22.0"}`}), forge.KindGitea, nil},
		{"sign-in only", locked.URL, "", nil},
		{"no forge API", serve(t, map[string]string{"GET /": "<html>hi</html>"}), "", forge.ErrNotFound},
		{"unreachable", closed.URL, "", errUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := probe(context.Background(), tt.url)
			switch {
			case got != tt.want:
				t.Errorf("kind = %q; want %q", got, tt.want)
			case tt.wantErr == nil && err != nil:
				t.Errorf("err = %v; want nil", err)
			case errors.Is(tt.wantErr, errUnreachable) && (err == nil || errors.Is(err, forge.ErrNotFound)):
				t.Errorf("err = %v; want a reachability error", err)
			case tt.wantErr != nil && !errors.Is(tt.wantErr, errUnreachable) && !errors.Is(err, tt.wantErr):
				t.Errorf("err = %v; want %v", err, tt.wantErr)
			}
		})
	}
}

// TestProbeConcurrent holds each adapter's probe endpoint open until the other has also been
// hit, so a serial probe never sees the second request and fails after two seconds.
func TestProbeConcurrent(t *testing.T) {
	var arrived sync.WaitGroup
	arrived.Add(2)
	both := make(chan struct{})
	go func() { arrived.Wait(); close(both) }()
	mux := http.NewServeMux()
	for _, pattern := range []string{"GET /api/v1/version", "GET /api/v3/meta"} {
		var once sync.Once
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			once.Do(arrived.Done)
			select {
			case <-both:
				_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
			case <-time.After(2 * time.Second):
				http.Error(w, "the other probe never arrived", http.StatusServiceUnavailable)
			case <-r.Context().Done():
			}
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	got, err := probe(context.Background(), srv.URL)
	if err != nil || got != forge.KindGitea {
		t.Fatalf("probe = %q, %v; want gitea with both probes in flight together", got, err)
	}
}

func TestConnectGitlabSkipsTokenCmd(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	_, err := connect(context.Background(), config.Host{Type: "gitlab", TokenCmd: "touch " + marker})
	if err == nil || !strings.Contains(err.Error(), `type "gitlab" has no adapter yet`) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("token_cmd ran for an unsupported type")
	}
}
