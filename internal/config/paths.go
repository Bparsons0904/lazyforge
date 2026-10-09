package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Paths holds the full file paths of the config and state files.
type Paths struct{ Config, State string }

// DefaultPaths resolves the file locations. The XDG variables win when set;
// otherwise darwin and windows keep both files under the user config dir and
// other platforms use ~/.config and ~/.local/state.
func DefaultPaths() (Paths, error) {
	return defaultPaths(runtime.GOOS)
}

func defaultPaths(goos string) (Paths, error) {
	const dir = "lazyforge"
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home dir: %w", err)
	}
	var cfgBase, stateBase string
	if goos == "darwin" || goos == "windows" {
		cfgBase, err = os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("find config dir: %w", err)
		}
		stateBase = cfgBase
	} else {
		cfgBase = filepath.Join(home, ".config")
		stateBase = filepath.Join(home, ".local", "state")
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		cfgBase = x
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		stateBase = x
	}
	return Paths{
		Config: filepath.Join(cfgBase, dir, "config.toml"),
		State:  filepath.Join(stateBase, dir, "state.toml"),
	}, nil
}

// writeTOML encodes v and replaces path atomically: temp file in the same
// directory, then rename. The mode is always 0600 because the file may hold a
// token.
func writeTOML(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*") // CreateTemp makes the file 0600
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails harmlessly after a successful rename
	if err := toml.NewEncoder(tmp).Encode(v); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
