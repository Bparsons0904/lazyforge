package update_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/update"
)

const (
	forgejoLatestPath = "/api/v1/repos/deadstyle/lazyforge/releases/latest"
	githubLatestPath  = "/repos/Bparsons0904/lazyforge/releases/latest"
	githubArchivePath = "/Bparsons0904/lazyforge/releases/download/" + testTag + "/" + archiveName
	githubChecksPath  = "/Bparsons0904/lazyforge/releases/download/" + testTag + "/checksums.txt"
	latestJSON        = `{"tag_name":"v1.3.0","body":""}`
)

// recorder keeps the path of every request a fake host receives, in order.
type recorder struct {
	mu    sync.Mutex
	paths []string
}

func (rec *recorder) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.paths = append(rec.paths, r.URL.Path)
		rec.mu.Unlock()
		h.ServeHTTP(w, r)
	})
}

func (rec *recorder) seen() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.paths...)
}

func serve(t *testing.T, h http.Handler) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(h))
	t.Cleanup(srv.Close)
	return srv, rec
}

// closedURL returns an address that refuses connections.
func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	return addr
}

// latestOn serves a single latest endpoint with a fixed status and body.
func latestOn(path string, status int, body string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	})
	return mux
}

// githubServer serves GitHub's layout: latestJSON and the release files.
// A nil archive or empty checksums answers 404 for that file.
func githubServer(t *testing.T, archive []byte, checksums string) (*httptest.Server, *recorder) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(githubLatestPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, latestJSON)
	})
	mux.HandleFunc(githubArchivePath, func(w http.ResponseWriter, r *http.Request) {
		if archive == nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(githubChecksPath, func(w http.ResponseWriter, r *http.Request) {
		if checksums == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, checksums)
	})
	return serve(t, mux)
}

func fallbackChecker(timeout time.Duration, sources ...update.Source) update.Checker {
	return update.Checker{Sources: sources, Client: &http.Client{Timeout: timeout}, GOOS: "linux", GOARCH: "amd64"}
}

func TestLatestFallsBackToGitHubWhenForgejoIsDown(t *testing.T) {
	archive := tarGz(t, "lazyforge", "new")
	gh, ghReq := githubServer(t, archive, sum(archive)+"  "+archiveName+"\n")
	checker := fallbackChecker(5*time.Second, update.Forgejo(closedURL(t)), update.GitHub(gh.URL, gh.URL))

	rel, err := checker.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != testTag || rel.Source.Name != "github" {
		t.Fatalf("Latest = %+v, want %s from github", rel, testTag)
	}
	afterLatest := len(ghReq.seen())

	exe := installExe(t)
	if err := checker.Apply(context.Background(), rel, exe); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Errorf("exe = %q, want new", got)
	}

	applyPaths := ghReq.seen()[afterLatest:]
	want := []string{githubArchivePath, githubChecksPath}
	slices.Sort(applyPaths)
	slices.Sort(want)
	if !slices.Equal(applyPaths, want) {
		t.Errorf("Apply requests = %v, want exactly %v", applyPaths, want)
	}
}

func TestLatestFallsBackToGitHubOnServerError(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			forgejo, _ := serve(t, latestOn(forgejoLatestPath, status, ""))
			gh, _ := githubServer(t, nil, "")
			checker := fallbackChecker(5*time.Second, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

			rel, err := checker.Latest(context.Background())
			if err != nil {
				t.Fatalf("Latest: %v", err)
			}
			if rel.Tag != testTag || rel.Source.Name != "github" {
				t.Errorf("Latest = %+v, want %s from github", rel, testTag)
			}
		})
	}
}

func TestLatestDoesNotFallBackOnNotFound(t *testing.T) {
	forgejo, _ := serve(t, latestOn(forgejoLatestPath, http.StatusNotFound, "no such repo"))
	gh, ghReq := githubServer(t, nil, "")
	checker := fallbackChecker(5*time.Second, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

	if _, err := checker.Latest(context.Background()); err == nil {
		t.Fatal("Latest: want error on 404")
	}
	if n := len(ghReq.seen()); n != 0 {
		t.Errorf("GitHub got %d requests, want 0: %v", n, ghReq.seen())
	}
}

func TestLatestDoesNotFallBackOnBadRelease(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid JSON", `{"tag_name":`},
		{"empty tag", `{"tag_name":"","body":"notes"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forgejo, _ := serve(t, latestOn(forgejoLatestPath, http.StatusOK, tt.body))
			gh, ghReq := githubServer(t, nil, "")
			checker := fallbackChecker(5*time.Second, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

			if _, err := checker.Latest(context.Background()); err == nil {
				t.Fatal("Latest: want error")
			}
			if n := len(ghReq.seen()); n != 0 {
				t.Errorf("GitHub got %d requests, want 0: %v", n, ghReq.seen())
			}
		})
	}
}

func TestLatestUsesForgejoWhenHealthy(t *testing.T) {
	forgejo, _ := serve(t, latestOn(forgejoLatestPath, http.StatusOK, latestJSON))
	gh, ghReq := githubServer(t, nil, "")
	checker := fallbackChecker(5*time.Second, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

	rel, err := checker.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != testTag || rel.Source.Name != "forgejo" {
		t.Errorf("Latest = %+v, want %s from forgejo", rel, testTag)
	}
	if n := len(ghReq.seen()); n != 0 {
		t.Errorf("GitHub got %d requests, want 0: %v", n, ghReq.seen())
	}
}

func TestLatestErrorNamesBothSources(t *testing.T) {
	tests := []struct {
		name         string
		forgejoURL   func(t *testing.T) string
		githubStatus int
	}{
		{"forgejo unreachable, github 500", closedURL, http.StatusInternalServerError},
		{"forgejo 503, github 403", func(t *testing.T) string {
			srv, _ := serve(t, latestOn(forgejoLatestPath, http.StatusServiceUnavailable, ""))
			return srv.URL
		}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh, _ := serve(t, latestOn(githubLatestPath, tt.githubStatus, ""))
			checker := fallbackChecker(5*time.Second, update.Forgejo(tt.forgejoURL(t)), update.GitHub(gh.URL, gh.URL))

			_, err := checker.Latest(context.Background())
			if err == nil {
				t.Fatal("Latest: want error when both sources fail")
			}
			msg := strings.ToLower(err.Error())
			for _, name := range []string{"forgejo", "github"} {
				if !strings.Contains(msg, name) {
					t.Errorf("error %q does not name source %s", err, name)
				}
			}
		})
	}
}

func TestLatestFallsBackWhenForgejoHangs(t *testing.T) {
	forgejo, _ := serve(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	gh, _ := githubServer(t, nil, "")
	checker := fallbackChecker(100*time.Millisecond, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

	rel, err := checker.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != testTag || rel.Source.Name != "github" {
		t.Errorf("Latest = %+v, want %s from github", rel, testTag)
	}
}

func TestLatestFallsBackWhenForgejoStallsBody(t *testing.T) {
	forgejo, _ := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	gh, _ := githubServer(t, nil, "")
	checker := fallbackChecker(100*time.Millisecond, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))

	rel, err := checker.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != testTag || rel.Source.Name != "github" {
		t.Errorf("Latest = %+v, want %s from github", rel, testTag)
	}
}

func TestApplyChecksumMismatchOnGitHubNeverContactsForgejo(t *testing.T) {
	archive := tarGz(t, "lazyforge", "new")
	gh, _ := githubServer(t, archive, strings.Repeat("0", 64)+"  "+archiveName+"\n")
	forgejo, forgejoReq := serve(t, latestOn(forgejoLatestPath, http.StatusOK, latestJSON))
	checker := fallbackChecker(5*time.Second, update.Forgejo(forgejo.URL), update.GitHub(gh.URL, gh.URL))
	rel := update.Release{Tag: testTag, Source: update.GitHub(gh.URL, gh.URL)}
	exe := installExe(t)

	err := checker.Apply(context.Background(), rel, exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Apply error = %v, want one containing %q", err, "checksum mismatch")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("exe = %q, want old", got)
	}
	if n := len(forgejoReq.seen()); n != 0 {
		t.Errorf("Forgejo got %d requests, want 0: %v", n, forgejoReq.seen())
	}
	if names := dirNames(t, filepath.Dir(exe)); len(names) != 1 {
		t.Errorf("dir = %v, want only exe", names)
	}
}

func TestSourceURLs(t *testing.T) {
	fj := update.Forgejo("https://git.example.test")
	gh := update.GitHub("https://api.example.test", "https://web.example.test")
	tests := []struct {
		name, got, want string
	}{
		{"forgejo name", fj.Name, "forgejo"},
		{"forgejo latest", fj.LatestURL, "https://git.example.test/api/v1/repos/deadstyle/lazyforge/releases/latest"},
		{"forgejo download base", fj.DownloadBase, "https://git.example.test/deadstyle/lazyforge/releases/download"},
		{"forgejo asset", fj.DownloadURL("v1.3.0", "checksums.txt"), "https://git.example.test/deadstyle/lazyforge/releases/download/v1.3.0/checksums.txt"},
		{"github name", gh.Name, "github"},
		{"github latest", gh.LatestURL, "https://api.example.test/repos/Bparsons0904/lazyforge/releases/latest"},
		{"github download base", gh.DownloadBase, "https://web.example.test/Bparsons0904/lazyforge/releases/download"},
		{"github asset", gh.DownloadURL("v1.3.0", archiveName), "https://web.example.test/Bparsons0904/lazyforge/releases/download/v1.3.0/" + archiveName},
		{"default github API", update.DefaultGitHubAPIURL, "https://api.github.com"},
		{"default github web", update.DefaultGitHubURL, "https://github.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestApplyWithZeroSourceFailsWithoutRequests(t *testing.T) {
	archive := tarGz(t, "lazyforge", "new")
	mux := http.NewServeMux()
	mux.HandleFunc(archivePath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(checksPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, sum(archive)+"  "+archiveName+"\n")
	})
	srv, rec := serve(t, mux)
	checker := fallbackChecker(5*time.Second, update.Forgejo(srv.URL))
	exe := installExe(t)

	if err := checker.Apply(context.Background(), update.Release{Tag: testTag}, exe); err == nil {
		t.Fatal("Apply with zero Source: want error")
	}
	if n := len(rec.seen()); n != 0 {
		t.Errorf("Apply with zero Source made %d requests, want 0: %v", n, rec.seen())
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("exe = %q, want old", got)
	}
}
