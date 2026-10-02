package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	testTag     = "v1.3.0"
	archivePath = "/deadstyle/lazyforge/releases/download/v1.3.0/lazyforge_v1.3.0_linux_amd64.tar.gz"
	archiveName = "lazyforge_v1.3.0_linux_amd64.tar.gz"
	checksPath  = "/deadstyle/lazyforge/releases/download/v1.3.0/checksums.txt"
)

func newChecker(url string) update.Checker {
	return update.Checker{BaseURL: url, Client: &http.Client{Timeout: 5 * time.Second}, GOOS: "linux", GOARCH: "amd64"}
}

func TestLatest(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.3.0","body":"\n\n  Faster startup  \nmore detail"}`)
	}))
	defer srv.Close()

	rel, err := newChecker(srv.URL).Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v1.3.0" || rel.Summary != "Faster startup" {
		t.Errorf("got %+v", rel)
	}
	if gotPath != "/api/v1/repos/deadstyle/lazyforge/releases/latest" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestLatestEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.3.0","body":" \n\n"}`)
	}))
	defer srv.Close()

	rel, err := newChecker(srv.URL).Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Summary != "" {
		t.Errorf("Summary = %q, want empty", rel.Summary)
	}
}

func TestLatestNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A valid release body, so only the status code can make Latest fail.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.0","body":""}`))
	}))
	defer srv.Close()

	if _, err := newChecker(srv.URL).Latest(context.Background()); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestLatestContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := newChecker(srv.URL).Latest(ctx); err == nil {
		t.Fatal("want error when deadline passes")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, want a quick return", d)
	}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.0", "v1.3.0", true},
		{"v1.3.0", "v1.3.0", false},
		{"v1.4.0", "v1.3.0", false},
		{"dev", "v1.3.0", false},
		{"abc123", "v1.3.0", false},
		{"v1.2.0", "v2.0.0-rc1", false},
		{"v1.9.0", "v1.10.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.current+"->"+tt.latest, func(t *testing.T) {
			if got := update.Newer(tt.current, tt.latest); got != tt.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestManaged(t *testing.T) {
	for _, p := range []string{
		"/opt/homebrew/bin/lazyforge",
		"/usr/local/Cellar/lazyforge/1.0/bin/lazyforge",
		"/home/linuxbrew/.linuxbrew/bin/lazyforge",
		"/nix/store/abc-lazyforge/bin/lazyforge",
		"/usr/bin/lazyforge",
	} {
		if !update.Managed(p) {
			t.Errorf("Managed(%q) = false, want true", p)
		}
	}

	if update.Managed(filepath.Join(t.TempDir(), "lazyforge")) {
		t.Error("writable temp dir reported as managed")
	}
}

func TestManagedReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// TempDir cleanup cannot remove entries from a read-only dir.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if !update.Managed(filepath.Join(dir, "lazyforge")) {
		t.Error("read-only dir reported as not managed")
	}
}

func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// release serves archive (404 when nil) and checksums (404 when empty) at the ADR 0008 paths.
func release(t *testing.T, archive []byte, checksums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(archivePath, func(w http.ResponseWriter, _ *http.Request) {
		if archive == nil {
			http.NotFound(w, nil)
			return
		}
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(checksPath, func(w http.ResponseWriter, _ *http.Request) {
		if checksums == "" {
			http.NotFound(w, nil)
			return
		}
		_, _ = fmt.Fprint(w, checksums)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func installExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "lazyforge")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestApplyHappyPath(t *testing.T) {
	archive := tarGz(t, "lazyforge", "new")
	srv := release(t, archive, fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", sum(archive), archiveName, strings.Repeat("0", 64)))
	exe := installExe(t)

	if err := newChecker(srv.URL).Apply(context.Background(), update.Release{Tag: testTag}, exe); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Errorf("exe = %q, want new", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
		t.Errorf("exe.old = %q, want old", got)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
	}
	if names := dirNames(t, filepath.Dir(exe)); len(names) != 2 {
		t.Errorf("dir = %v, want only exe and exe.old", names)
	}
}

func TestApplyFailsLeaveExeUntouched(t *testing.T) {
	good := tarGz(t, "lazyforge", "new")
	noBinary := tarGz(t, "README", "x")
	tests := []struct {
		name      string
		archive   []byte
		checksums string
	}{
		{"checksum mismatch", good, strings.Repeat("0", 64) + "  " + archiveName + "\n"},
		{"missing checksum line", good, sum(good) + "  other.tar.gz\n"},
		{"missing archive", nil, sum(good) + "  " + archiveName + "\n"},
		{"missing checksums file", good, ""},
		{"no lazyforge entry", noBinary, sum(noBinary) + "  " + archiveName + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := release(t, tt.archive, tt.checksums)
			exe := installExe(t)

			if err := newChecker(srv.URL).Apply(context.Background(), update.Release{Tag: testTag}, exe); err == nil {
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

func TestApplySwapFailureCleansUp(t *testing.T) {
	archive := tarGz(t, "lazyforge", "new")
	srv := release(t, archive, sum(archive)+"  "+archiveName+"\n")
	exe := installExe(t)
	// A non-empty directory at exe.old makes the first rename fail after the new binary is extracted.
	blocker := filepath.Join(exe+".old", "keep")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := newChecker(srv.URL).Apply(context.Background(), update.Release{Tag: testTag}, exe); err == nil {
		t.Fatal("want error")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("exe = %q, want old", got)
	}
	if names := dirNames(t, filepath.Dir(exe)); len(names) != 2 {
		t.Errorf("dir = %v, want only exe and the exe.old blocker", names)
	}
}

func TestRollback(t *testing.T) {
	exe := installExe(t)
	if err := os.WriteFile(exe+".old", []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := update.Rollback(exe); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "previous" {
		t.Errorf("exe = %q, want previous", got)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Errorf("exe.old still present: %v", err)
	}
}

func TestCleanupOld(t *testing.T) {
	exe := installExe(t)

	update.CleanupOld(exe) // no-op without exe.old
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("exe changed: %q", got)
	}

	if err := os.WriteFile(exe+".old", []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	update.CleanupOld(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Errorf("exe.old still present: %v", err)
	}
}

func TestAsk(t *testing.T) {
	rel := update.Release{Tag: "v1.3.0"}
	tests := []struct {
		in   string
		want bool
	}{
		{"\n", true},
		{"", false}, // EOF: no input is not consent
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{" YES \n", true},
		{"n\n", false},
		{"no\n", false},
		{"x\n", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			var out bytes.Buffer
			if got := update.Ask(strings.NewReader(tt.in), &out, "v1.2.0", rel); got != tt.want {
				t.Errorf("Ask = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAskPrompt(t *testing.T) {
	const prompt = "lazyforge v1.3.0 is available (you have v1.2.0). Update now? [Y/n] "

	var out bytes.Buffer
	update.Ask(strings.NewReader("n\n"), &out, "v1.2.0", update.Release{Tag: "v1.3.0", Summary: "Faster startup"})
	if want := "Faster startup\n" + prompt; !strings.Contains(out.String(), want) {
		t.Errorf("output = %q, want it to contain %q", out.String(), want)
	}

	out.Reset()
	update.Ask(strings.NewReader("n\n"), &out, "v1.2.0", update.Release{Tag: "v1.3.0"})
	if out.String() != prompt {
		t.Errorf("output = %q, want %q", out.String(), prompt)
	}
}
