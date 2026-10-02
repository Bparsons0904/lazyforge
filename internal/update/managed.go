package update

import (
	"os"
	"path/filepath"
	"strings"
)

var managedPrefixes = []string{"/opt/homebrew/", "/usr/local/Cellar/", "/home/linuxbrew/", "/nix/store/", "/usr/bin/"}

// Managed reports whether exe (symlink-resolved) is owned by a package manager or sits in a directory this user can't write.
func Managed(exe string) bool {
	for _, p := range managedPrefixes {
		if strings.HasPrefix(exe, p) {
			return true
		}
	}
	// Creating a file is the only reliable writability probe; mode bits miss ACLs and read-only mounts.
	f, err := os.CreateTemp(filepath.Dir(exe), ".lazyforge-probe-*")
	if err != nil {
		return true
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return false
}
