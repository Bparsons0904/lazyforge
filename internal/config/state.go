package config

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/BurntSushi/toml"
)

// State is what the app remembers between runs. Losing it is harmless.
type State struct {
	LastUpdateCheck time.Time `toml:"last_update_check,omitempty"`
	SkippedVersion  string    `toml:"skipped_version,omitempty"`
	LastHost        string    `toml:"last_host,omitempty"`
}

// LoadState reads the state file. A missing file yields the zero State.
func LoadState(path string) (State, error) {
	var s State
	if _, err := toml.DecodeFile(path, &s); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("load state %s: %w", path, err)
	}
	return s, nil
}

// SaveState writes s to path atomically.
func SaveState(path string, s State) error {
	return writeTOML(path, s)
}
