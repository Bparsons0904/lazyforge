//go:build windows

package main

import "os"

// restart runs exe as a child and exits with its code, because Windows has no exec that replaces the process.
// It returns only when the child cannot be started.
func restart(exe string, args, env []string) error {
	p, err := os.StartProcess(exe, args, &os.ProcAttr{
		Env:   env,
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}
	code := 1
	if st, err := p.Wait(); err == nil {
		code = st.ExitCode()
	}
	os.Exit(code)
	return nil
}
