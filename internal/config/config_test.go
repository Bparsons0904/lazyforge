package config_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

func writeFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFile(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadExampleRoundTrip(t *testing.T) {
	c, err := config.Load("testdata/example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultHost != "homelab" || len(c.Hosts) != 2 {
		t.Fatalf("unexpected config: %+v", c)
	}
	h := c.Hosts["homelab"]
	if h.Type != "forgejo" || h.URL != "https://git.bobparsons.dev" || !h.RequireGreenCI {
		t.Fatalf("homelab host wrong: %+v", h)
	}
	if r := h.Repos["deadstyle/lazyforge"]; r.RequireGreenCI == nil || *r.RequireGreenCI {
		t.Fatalf("repo override wrong: %+v", r)
	}

	out := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := config.Save(out, c); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip differs:\n got %+v\nwant %+v", got, c)
	}
}

func TestUpdateCheckDefault(t *testing.T) {
	minimal := "[hosts.a]\ntype = \"github\"\ntoken_cmd = \"x\"\n"
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"absent section", minimal, true},
		{"absent key", "[update]\n" + minimal, true},
		{"explicit false", "[update]\ncheck = false\n" + minimal, false},
		{"explicit true", "[update]\ncheck = true\n" + minimal, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := config.Load(writeFile(t, tt.body, 0o600))
			if err != nil {
				t.Fatal(err)
			}
			if c.Update.Check != tt.want {
				t.Fatalf("Update.Check = %v, want %v", c.Update.Check, tt.want)
			}
		})
	}
}

func TestImagesShowDefault(t *testing.T) {
	minimal := "[hosts.a]\ntype = \"github\"\ntoken_cmd = \"x\"\n"
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"absent section", minimal, true},
		{"absent key", "[images]\n" + minimal, true},
		{"explicit false", "[images]\nshow = false\n" + minimal, false},
		{"explicit true", "[images]\nshow = true\n" + minimal, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := config.Load(writeFile(t, tt.body, 0o600))
			if err != nil {
				t.Fatal(err)
			}
			if c.Images.Show != tt.want {
				t.Fatalf("Images.Show = %v, want %v", c.Images.Show, tt.want)
			}
		})
	}
	if !config.Defaults().Images.Show {
		t.Fatal("Defaults leaves images off")
	}
}

func TestImagesShowRoundTrip(t *testing.T) {
	c := config.Defaults()
	c.Images.Show = false
	out := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(out, c); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Images.Show {
		t.Fatal("images off did not survive a save and load")
	}
}

func TestSplashShowDefault(t *testing.T) {
	minimal := "[hosts.a]\ntype = \"github\"\ntoken_cmd = \"x\"\n"
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"absent section", minimal, true},
		{"absent key", "[splash]\n" + minimal, true},
		{"explicit false", "[splash]\nshow = false\n" + minimal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := config.Load(writeFile(t, tt.body, 0o600))
			if err != nil {
				t.Fatal(err)
			}
			if c.Splash.Show != tt.want {
				t.Fatalf("Splash.Show = %v, want %v", c.Splash.Show, tt.want)
			}
			// A saved choice survives the next load, including off.
			out := filepath.Join(t.TempDir(), "config.toml")
			if err := config.Save(out, c); err != nil {
				t.Fatal(err)
			}
			got, err := config.Load(out)
			if err != nil {
				t.Fatal(err)
			}
			if got.Splash.Show != tt.want {
				t.Fatalf("after save, Splash.Show = %v, want %v", got.Splash.Show, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	ok := func() config.Host { return config.Host{Type: "forgejo", URL: "https://x", TokenCmd: "t"} }
	tests := []struct {
		name    string
		c       config.Config
		wantErr string // substring; empty means valid
	}{
		{"empty hosts valid", config.Config{}, ""},
		{"github without url valid", config.Config{Hosts: map[string]config.Host{"gh": {Type: "github", Token: "x"}}}, ""},
		{"gitlab without url valid", config.Config{Hosts: map[string]config.Host{"gl": {Type: "gitlab", TokenCmd: "x"}}}, ""},
		{"bad type", config.Config{Hosts: map[string]config.Host{"homelab": {Type: "svn", URL: "u", TokenCmd: "t"}}}, "hosts.homelab.type"},
		{"empty type", config.Config{Hosts: map[string]config.Host{"homelab": {URL: "u", TokenCmd: "t"}}}, "hosts.homelab.type"},
		{"forgejo no url", config.Config{Hosts: map[string]config.Host{"homelab": {Type: "forgejo", TokenCmd: "t"}}}, "hosts.homelab.url"},
		{"gitea no url", config.Config{Hosts: map[string]config.Host{"homelab": {Type: "gitea", TokenCmd: "t"}}}, "hosts.homelab.url"},
		{"both tokens", config.Config{Hosts: map[string]config.Host{"homelab": {Type: "forgejo", URL: "u", Token: "a", TokenCmd: "b"}}}, "hosts.homelab.token"},
		{"neither token", config.Config{Hosts: map[string]config.Host{"homelab": {Type: "forgejo", URL: "u"}}}, "hosts.homelab.token"},
		{"unknown default", config.Config{DefaultHost: "ghost", Hosts: map[string]config.Host{"a": ok()}}, "default_host"},
		{"default with no hosts", config.Config{DefaultHost: "ghost"}, "default_host"},
		{"known default valid", config.Config{DefaultHost: "a", Hosts: map[string]config.Host{"a": ok()}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to name %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRunsValidate(t *testing.T) {
	p := writeFile(t, "[hosts.homelab]\ntype = \"forgejo\"\ntoken_cmd = \"x\"\n", 0o600)
	_, err := config.Load(p)
	if err == nil || !strings.Contains(err.Error(), "hosts.homelab.url") {
		t.Fatalf("err = %v, want hosts.homelab.url", err)
	}
}

func TestLoadTokenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	const secret = "s3cr3t-fake-token"
	withToken := "[hosts.a]\ntype = \"github\"\ntoken = \"" + secret + "\"\n"
	withCmd := "[hosts.a]\ntype = \"github\"\ntoken_cmd = \"x\"\n"

	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660} {
		p := writeFile(t, withToken, mode)
		_, err := config.Load(p)
		if err == nil {
			t.Fatalf("mode %o: Load succeeded, want refusal", mode)
		}
		if !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "0600") {
			t.Errorf("mode %o: error %q should name path and 0600", mode, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("mode %o: error leaks token", mode)
		}
	}

	if _, err := config.Load(writeFile(t, withToken, 0o600)); err != nil {
		t.Errorf("0600 with token: %v", err)
	}
	if _, err := config.Load(writeFile(t, withCmd, 0o644)); err != nil {
		t.Errorf("0644 without token: %v", err)
	}
}

func TestSave(t *testing.T) {
	c := config.Config{Hosts: map[string]config.Host{"a": {Type: "github", Token: "fake"}}}

	t.Run("mode 0600 and parent created 0700", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "new", "lazyforge")
		p := filepath.Join(dir, "config.toml")
		if err := config.Save(p, c); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %o, want 600", fi.Mode().Perm())
		}
		di, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if di.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %o, want 700", di.Mode().Perm())
		}
	})

	t.Run("replaces existing, no temp files left", func(t *testing.T) {
		p := writeFile(t, "garbage", 0o644)
		if err := config.Save(p, c); err != nil {
			t.Fatal(err)
		}
		got, err := config.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hosts["a"].Token != "fake" {
			t.Errorf("existing file not replaced: %+v", got)
		}
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("replaced file mode = %o, want 600", fi.Mode().Perm())
		}
		ents, err := os.ReadDir(filepath.Dir(p))
		if err != nil {
			t.Fatal(err)
		}
		if len(ents) != 1 {
			t.Errorf("directory has %d entries, want only the config", len(ents))
		}
	})
}

func TestState(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file is zero state", func(t *testing.T) {
		s, err := config.LoadState(filepath.Join(dir, "none.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if !s.LastUpdateCheck.IsZero() || s.SkippedVersion != "" || s.LastHost != "" {
			t.Fatalf("want zero state, got %+v", s)
		}
	})

	t.Run("round trip, creates parent, leaves no temp files", func(t *testing.T) {
		p := filepath.Join(dir, "sub", "state.toml")
		want := config.State{
			LastUpdateCheck: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
			SkippedVersion:  "v1.2.3",
			LastHost:        "homelab",
		}
		if err := config.SaveState(p, want); err != nil {
			t.Fatal(err)
		}
		got, err := config.LoadState(p)
		if err != nil {
			t.Fatal(err)
		}
		if !got.LastUpdateCheck.Equal(want.LastUpdateCheck) || got.SkippedVersion != want.SkippedVersion || got.LastHost != want.LastHost {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		ents, _ := os.ReadDir(filepath.Dir(p))
		if len(ents) != 1 {
			t.Errorf("directory has %d entries, want 1", len(ents))
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		p := filepath.Join(dir, "state.toml")
		for _, h := range []string{"one", "two"} {
			if err := config.SaveState(p, config.State{LastHost: h}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := config.LoadState(p)
		if err != nil || got.LastHost != "two" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

func TestSelectHost(t *testing.T) {
	h := config.Host{Type: "github", TokenCmd: "x"}
	one := map[string]config.Host{"a": h}
	two := map[string]config.Host{"a": h, "b": h}
	tests := []struct {
		name     string
		c        config.Config
		flag     string
		want     string
		wantErr  error
		wantName string // substring expected in a non-sentinel error
	}{
		{name: "flag wins", c: config.Config{DefaultHost: "a", Hosts: two}, flag: "b", want: "b"},
		{name: "unknown flag", c: config.Config{Hosts: two}, flag: "zzz", wantName: "zzz"},
		{name: "unknown flag with one host", c: config.Config{Hosts: one}, flag: "zzz", wantName: "zzz"},
		{name: "single host", c: config.Config{Hosts: one}, want: "a"},
		{name: "single host beats default", c: config.Config{DefaultHost: "ghost", Hosts: one}, want: "a"},
		{name: "default host", c: config.Config{DefaultHost: "b", Hosts: two}, want: "b"},
		{name: "no default", c: config.Config{Hosts: two}, wantErr: config.ErrNeedPicker},
		{name: "zero hosts", c: config.Config{}, wantErr: config.ErrNeedPicker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.SelectHost(tt.c, tt.flag)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantName != "":
				if err == nil || errors.Is(err, config.ErrNeedPicker) || !strings.Contains(err.Error(), tt.wantName) {
					t.Fatalf("err = %v, want non-picker error naming %q", err, tt.wantName)
				}
			default:
				if err != nil || got != tt.want {
					t.Fatalf("got %q, %v; want %q", got, err, tt.want)
				}
			}
		})
	}
}

func TestRequiresGreenCI(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		h    config.Host
		repo string
		want bool
	}{
		{"nothing set", config.Host{}, "o/r", false},
		{"host true", config.Host{RequireGreenCI: true}, "o/r", true},
		{"override true beats host false", config.Host{Repos: map[string]config.RepoSettings{"o/r": {RequireGreenCI: &yes}}}, "o/r", true},
		{"override false beats host true", config.Host{RequireGreenCI: true, Repos: map[string]config.RepoSettings{"o/r": {RequireGreenCI: &no}}}, "o/r", false},
		{"nil override inherits", config.Host{RequireGreenCI: true, Repos: map[string]config.RepoSettings{"o/r": {}}}, "o/r", true},
		{"other repo inherits", config.Host{RequireGreenCI: true, Repos: map[string]config.RepoSettings{"o/x": {RequireGreenCI: &no}}}, "o/r", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.h.RequiresGreenCI(tt.repo); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveToken(t *testing.T) {
	ctx := context.Background()

	t.Run("cmd output trimmed", func(t *testing.T) {
		got, err := config.ResolveToken(ctx, config.Host{TokenCmd: `printf "  tok\n"`})
		if err != nil || got != "tok" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("literal token returned as is", func(t *testing.T) {
		got, err := config.ResolveToken(ctx, config.Host{Token: " raw "})
		if err != nil || got != " raw " {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	t.Run("neither is an error", func(t *testing.T) {
		if _, err := config.ResolveToken(ctx, config.Host{}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("empty output is an error", func(t *testing.T) {
		if _, err := config.ResolveToken(ctx, config.Host{TokenCmd: `printf "  \n"`}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("non-zero exit reports status but not output", func(t *testing.T) {
		cmd := `echo FAKESECRET-out; echo FAKESECRET-err >&2; exit 3`
		_, err := config.ResolveToken(ctx, config.Host{TokenCmd: cmd})
		if err == nil {
			t.Fatal("want error")
		}
		if !strings.Contains(err.Error(), "3") {
			t.Errorf("error %q should include exit status 3", err)
		}
		if strings.Contains(err.Error(), "FAKESECRET") {
			t.Errorf("error %q leaks command output", err)
		}
	})

	t.Run("context cancellation stops the command", func(t *testing.T) {
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := config.ResolveToken(cctx, config.Host{TokenCmd: "sleep 5"})
		if err == nil {
			t.Fatal("want error")
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("took %v, command was not stopped", d)
		}
	})
}

func TestHostShowsRenovate(t *testing.T) {
	on, off := true, false
	for name, tc := range map[string]struct {
		h    config.Host
		want bool
	}{
		"no user, no setting":  {config.Host{}, false},
		"user, no setting":     {config.Host{RenovateUser: "bot"}, true},
		"user, switched off":   {config.Host{RenovateUser: "bot", Renovate: &off}, false},
		"no user, switched on": {config.Host{Renovate: &on}, true},
	} {
		if got := tc.h.ShowsRenovate(); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
