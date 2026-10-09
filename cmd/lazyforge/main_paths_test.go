package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

const twoHostConfig = `[hosts.a]
type = "forgejo"
url = "https://a.invalid"
token = "x"
[hosts.b]
type = "gitea"
url = "https://b.invalid"
token = "x"
`

func TestAppDepsExplicitConfigToleratesPathLookupFailure(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := config.DefaultPaths(); err == nil {
		t.Skip("default paths resolve without HOME on this platform")
	}
	d, err := appDeps(context.Background(), writeConfig(t, twoHostConfig), "", true)
	if err != nil {
		t.Fatalf("appDeps with --config and failing default paths: %v", err)
	}
	if d.StatePath != "" {
		t.Errorf("StatePath = %q; want empty when default paths fail", d.StatePath)
	}
	if d.LastHost != "" {
		t.Errorf("LastHost = %q; want empty", d.LastHost)
	}
	if len(d.Config.Hosts) != 2 {
		t.Errorf("config not loaded from --config: %+v", d.Config)
	}
}

func TestAppDepsWithoutConfigFlagErrorsWhenPathLookupFails(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := config.DefaultPaths(); err == nil {
		t.Skip("default paths resolve without HOME on this platform")
	}
	if d, err := appDeps(context.Background(), "", "", true); err == nil {
		t.Fatalf("appDeps = %+v; want error without --config", d)
	}
}

func TestAppDepsStatePathAndLastHostComeFromDefaultsEvenWithConfigFlag(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	statePath := filepath.Join(stateHome, "lazyforge", "state.toml")
	if err := config.SaveState(statePath, config.State{LastHost: "b"}); err != nil {
		t.Fatal(err)
	}
	d, err := appDeps(context.Background(), writeConfig(t, twoHostConfig), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if d.StatePath != statePath {
		t.Errorf("StatePath = %q; want %q", d.StatePath, statePath)
	}
	if d.LastHost != "b" {
		t.Errorf("LastHost = %q; want b", d.LastHost)
	}
}

func TestAppDepsUnreadableStateYieldsEmptyLastHost(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	statePath := filepath.Join(stateHome, "lazyforge", "state.toml")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("this is = = not toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := appDeps(context.Background(), writeConfig(t, twoHostConfig), "", true)
	if err != nil {
		t.Fatalf("corrupt state must not fail startup: %v", err)
	}
	if d.LastHost != "" {
		t.Errorf("LastHost = %q; want empty", d.LastHost)
	}
	if d.StatePath != statePath {
		t.Errorf("StatePath = %q; want %q", d.StatePath, statePath)
	}
}

func TestAppDepsDefaultConfigPathWithoutFlag(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfgPath := filepath.Join(cfgHome, "lazyforge", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(twoHostConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := appDeps(context.Background(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigPath != cfgPath || len(d.Config.Hosts) != 2 {
		t.Errorf("deps = %+v; want config loaded from %s", d, cfgPath)
	}
}
