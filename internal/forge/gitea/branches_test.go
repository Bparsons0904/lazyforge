package gitea_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

var _ forge.BranchReader = (*gitea.Forge)(nil)

var branchRef = domain.RepoRef{Owner: "o", Name: "r"}

// hit is one request a branch stub served.
type hit struct{ path, query string }

// hitLog records requests; handlers run on the server's goroutines, so access is locked.
type hitLog struct {
	mu   sync.Mutex
	hits []hit
}

func (l *hitLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits = append(l.hits, hit{path: r.URL.Path, query: r.URL.RawQuery})
}

func (l *hitLog) all() []hit {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.hits)
}

func (l *hitLog) hitsTo(path string) int {
	n := 0
	for _, h := range l.all() {
		if h.path == path {
			n++
		}
	}
	return n
}

// branchStub serves repo o/r with default branch main, the branches fixture paged at pageCap, and the given commits route.
func branchStub(t *testing.T, log *hitLog, commits http.HandlerFunc) *gitea.Forge {
	t.Helper()
	branches := loadList(t, "branches.json")
	return stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{owner}/{repo}": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			writeJSON(w, 200, obj{"name": r.PathValue("repo"), "default_branch": "main"})
		},
		"GET /api/v1/repos/{owner}/{repo}/branches": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			lo, hi, ok := pageBounds(r, len(branches))
			if !ok {
				writeRaw(w, 400, []byte(`{"message":"page required"}`))
				return
			}
			writeJSON(w, 200, append([]obj{}, branches[lo:hi]...))
		},
		"GET /api/v1/repos/{owner}/{repo}/commits": commits,
	})
}

// commitsRoute serves the commits fixture, or the error_404 body with status when status is not 200.
func commitsRoute(t *testing.T, log *hitLog, status int) http.HandlerFunc {
	t.Helper()
	commits := loadList(t, "branches_commits.json")
	return func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		if status != 200 {
			writeRaw(w, status, readFixture(t, "error_404.json"))
			return
		}
		writeJSON(w, 200, commits)
	}
}

func branchNamed(bs []domain.Branch, name string) (domain.Branch, bool) {
	for _, b := range bs {
		if b.Name == name {
			return b, true
		}
	}
	return domain.Branch{}, false
}

func TestListBranchesReadsRepoAndBranchList(t *testing.T) {
	log := &hitLog{}
	f := branchStub(t, log, commitsRoute(t, log, 200))

	if _, err := f.ListBranches(t.Context(), branchRef); err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if log.hitsTo("/api/v1/repos/o/r") != 1 {
		t.Errorf("repo lookup hits = %d, want 1 (default branch comes from /repos/o/r)", log.hitsTo("/api/v1/repos/o/r"))
	}
	if log.hitsTo("/api/v1/repos/o/r/branches") < 1 {
		t.Error("branch list was never requested")
	}
}

func TestListBranchesWalksEveryPage(t *testing.T) {
	log := &hitLog{}
	f := branchStub(t, log, commitsRoute(t, log, 200))

	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d branches, want all 3 across pages of %d", len(got), pageCap)
	}
	if n := log.hitsTo("/api/v1/repos/o/r/branches"); n < 2 {
		t.Errorf("branch list requested %d times, want at least 2 to cover 3 branches at pageCap %d", n, pageCap)
	}
}

func TestListBranchesMapsFieldsAndDefault(t *testing.T) {
	f := branchStub(t, &hitLog{}, commitsRoute(t, &hitLog{}, 200))
	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}

	main, ok := branchNamed(got, "main")
	if !ok {
		t.Fatalf("main missing from %+v", got)
	}
	if !main.Default {
		t.Errorf("main.Default = false, want true (it is default_branch)")
	}
	if main.Commit.SHA != "1111111111111111111111111111111111111111" {
		t.Errorf("main SHA = %q", main.Commit.SHA)
	}
	if main.Commit.Message != "Fix the build" {
		t.Errorf("main subject = %q, want the first line only", main.Commit.Message)
	}
	if main.Commit.Author != "Ada" {
		t.Errorf("main author = %q, want Ada", main.Commit.Author)
	}
	if want := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC); !main.Commit.Date.Equal(want) {
		t.Errorf("main date = %v, want %v", main.Commit.Date, want)
	}

	x, ok := branchNamed(got, "feature/x")
	if !ok {
		t.Fatalf("feature/x missing from %+v", got)
	}
	if x.Default {
		t.Error("feature/x.Default = true, want false")
	}
	if x.Commit.Message != "Add the x box" || x.Commit.Author != "Grace" {
		t.Errorf("feature/x commit = %q by %q", x.Commit.Message, x.Commit.Author)
	}

	defaults := 0
	for _, b := range got {
		if b.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("%d branches marked Default, want exactly 1", defaults)
	}
}

func TestListBranchesWebURL(t *testing.T) {
	f := branchStub(t, &hitLog{}, commitsRoute(t, &hitLog{}, 200))
	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}

	tests := []struct{ name, suffix string }{
		{"main", "/o/r/src/branch/main"},
		{"feature/x", "/o/r/src/branch/feature/x"},
		{"a&b#c/d", "/o/r/src/branch/a&b%23c/d"},
	}
	for _, tt := range tests {
		b, ok := branchNamed(got, tt.name)
		if !ok {
			t.Errorf("%q missing from %+v", tt.name, got)
			continue
		}
		if !strings.HasSuffix(b.WebURL, tt.suffix) {
			t.Errorf("%q WebURL = %q, want suffix %q", tt.name, b.WebURL, tt.suffix)
		}
	}
}

func TestListCommitsSendsBranchAndLimit(t *testing.T) {
	log := &hitLog{}
	f := branchStub(t, log, commitsRoute(t, log, 200))

	if _, err := f.ListCommits(t.Context(), branchRef, "main"); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	var found bool
	for _, h := range log.all() {
		if h.path != "/api/v1/repos/o/r/commits" {
			continue
		}
		found = true
		if !strings.Contains(h.query, "sha=main") || !strings.Contains(h.query, "limit=30") {
			t.Errorf("commits query = %q, want sha=main and limit=30", h.query)
		}
	}
	if !found {
		t.Error("commits endpoint was never requested")
	}
}

func TestListCommitsEscapesBranchName(t *testing.T) {
	log := &hitLog{}
	f := branchStub(t, log, commitsRoute(t, log, 200))

	if _, err := f.ListCommits(t.Context(), branchRef, "a&b#c/d"); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	for _, h := range log.all() {
		if h.path == "/api/v1/repos/o/r/commits" && !strings.Contains(h.query, "sha=a%26b%23c%2Fd") {
			t.Errorf("commits query = %q, want sha=a%%26b%%23c%%2Fd", h.query)
		}
	}
}

func TestListCommitsMapsFields(t *testing.T) {
	f := branchStub(t, &hitLog{}, commitsRoute(t, &hitLog{}, 200))
	got, err := f.ListCommits(t.Context(), branchRef, "main")
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if got[0].SHA != "4444444444444444444444444444444444444444" || got[0].Message != "Tidy the README" {
		t.Errorf("first commit = %q %q, want the 4444 SHA and subject Tidy the README", got[0].SHA, got[0].Message)
	}
	if got[0].Author != "Ada" {
		t.Errorf("first commit author = %q, want Ada", got[0].Author)
	}
	if got[0].Date.IsZero() {
		t.Error("first commit date is zero")
	}
}

func TestListCommitsMissingBranchIsNotFound(t *testing.T) {
	for _, status := range []int{404, 409} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			log := &hitLog{}
			f := branchStub(t, log, commitsRoute(t, log, status))

			_, err := f.ListCommits(t.Context(), branchRef, "gone")
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("ListCommits() on %d error = %v, want ErrNotFound", status, err)
			}
		})
	}
}
