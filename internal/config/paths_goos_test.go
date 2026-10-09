package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsByGOOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Windows keys off %AppData%, so a value that differs from the Unix-style
	// fallback shows the choice of directory, not just the home directory.
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		goos, wantConfig, wantState string
	}{
		{
			"linux",
			filepath.Join(home, ".config", "lazyforge", "config.toml"),
			filepath.Join(home, ".local", "state", "lazyforge", "state.toml"),
		},
		{
			"darwin",
			filepath.Join(userConfig, "lazyforge", "config.toml"),
			filepath.Join(userConfig, "lazyforge", "state.toml"),
		},
		{
			"windows",
			filepath.Join(userConfig, "lazyforge", "config.toml"),
			filepath.Join(userConfig, "lazyforge", "state.toml"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			p, err := defaultPaths(tt.goos)
			if err != nil {
				t.Fatal(err)
			}
			if p.Config != tt.wantConfig {
				t.Errorf("Config = %q, want %q", p.Config, tt.wantConfig)
			}
			if p.State != tt.wantState {
				t.Errorf("State = %q, want %q", p.State, tt.wantState)
			}
		})
	}
}

func TestDefaultPathsXDGOverrideWinsOnWindows(t *testing.T) {
	cfg, state := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", state)

	p, err := defaultPaths("windows")
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
