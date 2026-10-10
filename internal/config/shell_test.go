package config

import (
	"slices"
	"testing"
)

func TestShellCommand(t *testing.T) {
	const tokenCmd = `pass show "work/forge token" | head -n1`
	tests := []struct {
		goos, name string
		args       []string
	}{
		{"linux", "sh", []string{"-c", tokenCmd}},
		{"darwin", "sh", []string{"-c", tokenCmd}},
		{"windows", "cmd", []string{"/C", tokenCmd}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := shellCommand(tt.goos, tokenCmd)
			if name != tt.name || !slices.Equal(args, tt.args) {
				t.Errorf("shellCommand(%q) = %q %q, want %q %q", tt.goos, name, args, tt.name, tt.args)
			}
		})
	}
}
