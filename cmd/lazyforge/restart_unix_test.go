//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestRestartReturnsExecError(t *testing.T) {
	// A missing binary fails in exec before the test process is replaced.
	missing := filepath.Join(t.TempDir(), "missing-lazyforge")
	if err := restart(missing, []string{"lazyforge"}, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("restart error = %v, want one wrapping fs.ErrNotExist", err)
	}
}
