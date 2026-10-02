package config

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const tokenCmdTimeout = 10 * time.Second

// ResolveToken returns the host's token, running token_cmd when set. Errors
// never include the command's output or the token: either may be the secret.
func ResolveToken(ctx context.Context, h Host) (string, error) {
	if h.TokenCmd == "" {
		if h.Token == "" {
			return "", errors.New("host has neither token nor token_cmd")
		}
		return h.Token, nil
	}
	ctx, cancel := context.WithTimeout(ctx, tokenCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", h.TokenCmd)
	// A grandchild (for example `a; sleep 5`) can keep the stdout pipe open after sh is
	// killed; without a delay Output would block until it exits.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("token_cmd: %w", ctx.Err())
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("token_cmd failed: %s", ee.ProcessState)
		}
		return "", errors.New("token_cmd could not run")
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", errors.New("token_cmd printed nothing")
	}
	return tok, nil
}
