package ui

import (
	"slices"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	const rawURL = "https://git.example.invalid/owner/repo/pulls/7?tab=files&q=a+b"
	tests := []struct {
		goos, name string
		args       []string
	}{
		{"linux", "xdg-open", []string{rawURL}},
		{"darwin", "open", []string{rawURL}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}},
		{"freebsd", "xdg-open", []string{rawURL}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := browserCommand(tt.goos, rawURL)
			if name != tt.name || !slices.Equal(args, tt.args) {
				t.Errorf("browserCommand(%q) = %q %q, want %q %q", tt.goos, name, args, tt.name, tt.args)
			}
		})
	}
}

func TestDefaultEditor(t *testing.T) {
	tests := []struct{ goos, want string }{
		{"windows", "notepad"},
		{"linux", "vi"},
		{"darwin", "vi"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := defaultEditor(tt.goos); got != tt.want {
				t.Errorf("defaultEditor(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}
