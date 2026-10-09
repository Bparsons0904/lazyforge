package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/update"
)

const noUpdateEnv = "LAZYFORGE_NO_UPDATE_CHECK"

// maybeUpdate offers a newer release and, if accepted, swaps the binary and re-execs into it.
// It returns only when the caller should carry on starting the current version.
func maybeUpdate(ctx context.Context, checkEnabled bool) {
	if version == "dev" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	// Runs before the skip checks: the re-exec'd process has the skip env set and must still clear the old binary.
	update.CleanupOld(exe)
	if !checkEnabled || os.Getenv(noUpdateEnv) == "1" || !stdinIsTerminal() {
		return
	}

	c := update.Checker{
		BaseURL: update.DefaultBaseURL,
		Client:  &http.Client{Timeout: 2 * time.Second},
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	rel, err := c.Latest(ctx)
	cancel()
	if err != nil || !update.Newer(version, rel.Tag) {
		return
	}
	if update.Managed(exe) {
		fmt.Fprintf(os.Stderr, "lazyforge %s is available (you have %s). Update with your package manager or install.sh.\n", rel.Tag, version)
		return
	}
	if !update.Ask(os.Stdin, os.Stderr, version, rel) {
		return
	}

	c.Client = &http.Client{Timeout: 2 * time.Minute}
	ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := c.Apply(ctx, rel, exe); err != nil {
		fmt.Fprintf(os.Stderr, "update failed: %v; starting %s\n", err, version)
		return
	}
	// The marker stops a loop if the new build still reports an older version.
	err = restart(exe, os.Args, append(os.Environ(), noUpdateEnv+"=1"))
	if rbErr := update.Rollback(exe); rbErr != nil {
		err = fmt.Errorf("%w (rollback: %w)", err, rbErr)
	}
	fmt.Fprintf(os.Stderr, "could not start %s: %v; starting %s\n", rel.Tag, err, version)
}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
