//go:build !windows

package main

import "syscall"

// restart replaces the process with exe; it returns only when the exec fails.
func restart(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}
