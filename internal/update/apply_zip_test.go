package update_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/update"
)

func zipOf(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func windowsZipName(goarch string) string {
	return fmt.Sprintf("lazyforge_%s_windows_%s.zip", testTag, goarch)
}

func windowsZipPath(goarch string) string {
	return "/deadstyle/lazyforge/releases/download/" + testTag + "/" + windowsZipName(goarch)
}

func windowsChecker(url, goarch string) update.Checker {
	return update.Checker{Sources: []update.Source{update.Forgejo(url)}, Client: &http.Client{Timeout: 5 * time.Second}, GOOS: "windows", GOARCH: goarch}
}

// windowsRelease serves the zip at archivePath and the checksums file at the ADR 0008 path.
func windowsRelease(t *testing.T, archivePath string, archive []byte, checksums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(archivePath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(checksPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func windowsInstall(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "lazyforge.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestApplyWindowsZipSwapsBinary(t *testing.T) {
	for _, goarch := range []string{"amd64", "arm64"} {
		t.Run(goarch, func(t *testing.T) {
			archive := zipOf(t, "lazyforge.exe", "new")
			srv := windowsRelease(t, windowsZipPath(goarch), archive, sum(archive)+"  "+windowsZipName(goarch)+"\n")
			exe := windowsInstall(t)

			if err := windowsChecker(srv.URL, goarch).Apply(context.Background(), update.Release{Tag: testTag, Source: update.Forgejo(srv.URL)}, exe); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got, _ := os.ReadFile(exe); string(got) != "new" {
				t.Errorf("exe = %q, want new", got)
			}
			if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
				t.Errorf("exe.old = %q, want old", got)
			}
			if names := dirNames(t, filepath.Dir(exe)); len(names) != 2 {
				t.Errorf("dir = %v, want only exe and exe.old", names)
			}
		})
	}
}

func TestApplyWindowsZipFailsLeaveExeUntouched(t *testing.T) {
	good := zipOf(t, "lazyforge.exe", "new")
	tests := []struct {
		name      string
		checksums string
	}{
		{"checksum mismatch", strings.Repeat("0", 64) + "  " + windowsZipName("amd64") + "\n"},
		{"missing checksum line", sum(good) + "  lazyforge_v1.3.0_linux_amd64.tar.gz\n"},
		{"missing checksums file", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := windowsRelease(t, windowsZipPath("amd64"), good, tt.checksums)
			exe := windowsInstall(t)

			if err := windowsChecker(srv.URL, "amd64").Apply(context.Background(), update.Release{Tag: testTag, Source: update.Forgejo(srv.URL)}, exe); err == nil {
				t.Fatal("want error")
			}
			if got, _ := os.ReadFile(exe); string(got) != "old" {
				t.Errorf("exe = %q, want old", got)
			}
			if names := dirNames(t, filepath.Dir(exe)); len(names) != 1 {
				t.Errorf("dir = %v, want only exe", names)
			}
		})
	}
}

func TestApplyWindowsZipWithoutBinaryErrors(t *testing.T) {
	archive := zipOf(t, "README.txt", "x")
	srv := windowsRelease(t, windowsZipPath("amd64"), archive, sum(archive)+"  "+windowsZipName("amd64")+"\n")
	exe := windowsInstall(t)

	err := windowsChecker(srv.URL, "amd64").Apply(context.Background(), update.Release{Tag: testTag, Source: update.Forgejo(srv.URL)}, exe)
	if err == nil || !strings.Contains(err.Error(), "archive has no lazyforge binary") {
		t.Fatalf("Apply error = %v, want one containing %q", err, "archive has no lazyforge binary")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("exe = %q, want old", got)
	}
	if names := dirNames(t, filepath.Dir(exe)); len(names) != 1 {
		t.Errorf("dir = %v, want only exe", names)
	}
}
