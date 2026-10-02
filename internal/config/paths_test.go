package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

func TestDefaultPathsXDG(t *testing.T) {
	cfg, state := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", state)

	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cfg, "lazyforge", "config.toml"); p.Config != want {
		t.Errorf("Config = %q, want %q", p.Config, want)
	}
	if want := filepath.Join(state, "lazyforge", "state.toml"); p.State != want {
		t.Errorf("State = %q, want %q", p.State, want)
	}
}

func TestDefaultPathsFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	p, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	var wantCfg, wantState string
	if runtime.GOOS == "darwin" {
		dir, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		wantCfg = filepath.Join(dir, "lazyforge", "config.toml")
		wantState = filepath.Join(dir, "lazyforge", "state.toml")
	} else {
		wantCfg = filepath.Join(home, ".config", "lazyforge", "config.toml")
		wantState = filepath.Join(home, ".local", "state", "lazyforge", "state.toml")
	}
	if p.Config != wantCfg {
		t.Errorf("Config = %q, want %q", p.Config, wantCfg)
	}
	if p.State != wantState {
		t.Errorf("State = %q, want %q", p.State, wantState)
	}
}
