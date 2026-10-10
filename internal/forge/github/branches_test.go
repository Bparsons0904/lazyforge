package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/github"
)

var _ forge.BranchReader = (*github.Forge)(nil)

var branchRef = domain.RepoRef{Owner: "o", Name: "r"}

const (
	branchesPath = "/api/v3/repos/o/r/branches"
	commitsPath  = "/api/v3/repos/o/r/commits"
	commitPrefix = "/api/v3/repos/o/r/commits/"
)

// apiCall is one request a branch stub served.
type apiCall struct{ path, query string }

// apiCallLog records requests; handlers run on the server's goroutines, so access is locked.
type apiCallLog struct {
	mu    sync.Mutex
	calls []apiCall
}

func (l *apiCallLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, apiCall{path: r.URL.Path, query: r.URL.RawQuery})
}

func (l *apiCallLog) all() []apiCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

func (l *apiCallLog) count(match func(apiCall) bool) int {
	n := 0
	for _, c := range l.all() {
		if match(c) {
			n++
		}
	}
	return n
}

// commitObj is one commit in GitHub's commit shape. Author and committer share a date, so the test does not depend on which the adapter reads.
func commitObj(sha, message, author string, date time.Time) obj {
	when := date.Format(time.RFC3339)
	return obj{"sha": sha, "commit": obj{
		"message":   message,
		"author":    obj{"name": author, "date": when},
		"committer": obj{"name": author, "date": when},
	}}
}

// branchRoutes serves repo o/r with default branch main, the list as the one branch-list response, byBranch for
// GET branches/{branch}, and bySHA for GET commits/{sha}. Anything else is a 404.
func branchRoutes(log *apiCallLog, list []obj, byBranch, bySHA map[string]obj) map[string]http.HandlerFunc {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		writeRaw(w, 404, []byte(`{"message":"Not Found"}`))
	}
	return map[string]http.HandlerFunc{
		"GET /api/v3/repos/{owner}/{repo}": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			writeJSON(w, 200, obj{"name": r.PathValue("repo"), "default_branch": "main"})
		},
		"GET /api/v3/repos/{owner}/{repo}/branches": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			writeJSON(w, 200, list)
		},
		"GET /api/v3/repos/{owner}/{repo}/branches/{branch}": func(w http.ResponseWriter, r *http.Request) {
			d, ok := byBranch[r.PathValue("branch")]
			if !ok {
				notFound(w, r)
				return
			}
			log.add(r)
			writeJSON(w, 200, d)
		},
		"GET /api/v3/repos/{owner}/{repo}/commits/{sha}": func(w http.ResponseWriter, r *http.Request) {
			d, ok := bySHA[r.PathValue("sha")]
			if !ok {
				notFound(w, r)
				return
			}
			log.add(r)
			writeJSON(w, 200, d)
		},
	}
}

// stubRoutes is stub for several patterns at once; the user and meta fixtures are served as in stub.
func stubRoutes(t *testing.T, routes map[string]http.HandlerFunc) *github.Forge {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 200, readFixture(t, "user.json")) })
	mux.HandleFunc("GET /api/v3/meta", func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 200, readFixture(t, "meta_ghes.json")) })
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f, err := github.New(t.Context(), srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// fixtureDetails returns the branch and commit details for the branches.json fixture.
func fixtureDetails() (byBranch, bySHA map[string]obj) {
	main := commitObj("1111111111111111111111111111111111111111", "Fix the build\n\nThe lockfile was stale.\n", "Ada", time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	x := commitObj("2222222222222222222222222222222222222222", "Add the x box", "Grace", time.Date(2026, 9, 20, 9, 30, 0, 0, time.UTC))
	odd := commitObj("3333333333333333333333333333333333333333", "Odd name", "Linus", time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC))
	byBranch = map[string]obj{"main": {"name": "main", "commit": main}}
	bySHA = map[string]obj{
		"1111111111111111111111111111111111111111": main,
		"2222222222222222222222222222222222222222": x,
		"3333333333333333333333333333333333333333": odd,
	}
	return byBranch, bySHA
}

func branchNamed(bs []domain.Branch, name string) (domain.Branch, bool) {
	for _, b := range bs {
		if b.Name == name {
			return b, true
		}
	}
	return domain.Branch{}, false
}

func isBranchList(c apiCall) bool { return c.path == branchesPath }

func isCommitDetail(c apiCall) bool { return strings.HasPrefix(c.path, commitPrefix) }

func TestListBranchesIssuesOneListCall(t *testing.T) {
	log := &apiCallLog{}
	byBranch, bySHA := fixtureDetails()
	f := stubRoutes(t, branchRoutes(log, loadList(t, "branches.json"), byBranch, bySHA))

	if _, err := f.ListBranches(t.Context(), branchRef); err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	lists := log.count(isBranchList)
	if lists != 1 {
		t.Errorf("branch list requested %d times, want exactly 1 (no pagination walk)", lists)
	}
	for _, c := range log.all() {
		if isBranchList(c) && !strings.Contains(c.query, "per_page=100") {
			t.Errorf("branch list query = %q, want per_page=100", c.query)
		}
	}
}

func TestListBranchesFetchesDefaultOnceAndEachOtherCommitOnce(t *testing.T) {
	log := &apiCallLog{}
	byBranch, bySHA := fixtureDetails()
	f := stubRoutes(t, branchRoutes(log, loadList(t, "branches.json"), byBranch, bySHA))

	if _, err := f.ListBranches(t.Context(), branchRef); err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if n := log.count(func(c apiCall) bool { return c.path == "/api/v3/repos/o/r/branches/main" }); n != 1 {
		t.Errorf("default branch detail requested %d times, want 1", n)
	}
	// main is the default, so only feature/x and a&b#c/d need a commit detail.
	if n := log.count(isCommitDetail); n != 2 {
		t.Errorf("commit detail requested %d times, want 2 (one per non-default branch)", n)
	}
}

func TestListBranchesMapsFieldsAndDefault(t *testing.T) {
	log := &apiCallLog{}
	byBranch, bySHA := fixtureDetails()
	f := stubRoutes(t, branchRoutes(log, loadList(t, "branches.json"), byBranch, bySHA))

	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}

	main, ok := branchNamed(got, "main")
	if !ok {
		t.Fatalf("main missing from %+v", got)
	}
	if !main.Default {
		t.Error("main.Default = false, want true")
	}
	if main.Commit.SHA != "1111111111111111111111111111111111111111" || main.Commit.Message != "Fix the build" {
		t.Errorf("main commit = %q %q, want the 1111 SHA and subject Fix the build", main.Commit.SHA, main.Commit.Message)
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
	if x.Default || x.Commit.Message != "Add the x box" || x.Commit.Author != "Grace" {
		t.Errorf("feature/x = %+v, want non-default with subject Add the x box by Grace", x)
	}

	defaults := 0
	for _, b := range got {
		if b.Default {
			defaults++
		}
	}
	if defaults != 1 || len(got) != 3 {
		t.Errorf("got %d branches with %d defaults, want 3 branches with exactly 1 default", len(got), defaults)
	}
}

func TestListBranchesWebURL(t *testing.T) {
	log := &apiCallLog{}
	byBranch, bySHA := fixtureDetails()
	f := stubRoutes(t, branchRoutes(log, loadList(t, "branches.json"), byBranch, bySHA))

	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	tests := []struct{ name, suffix string }{
		{"main", "/o/r/tree/main"},
		{"feature/x", "/o/r/tree/feature/x"},
		{"a&b#c/d", "/o/r/tree/a&b%23c/d"},
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

func TestListBranchesCapsAtOneHundredKeepingDefaultAndFirstNinetyNineOthers(t *testing.T) {
	log := &apiCallLog{}
	list := []obj{{"name": "main", "commit": obj{"sha": "0000000000000000000000000000000000000000"}}}
	bySHA := map[string]obj{}
	for i := range 149 {
		name := fmt.Sprintf("b%03d", i)
		sha := fmt.Sprintf("%040d", i+1)
		list = append(list, obj{"name": name, "commit": obj{"sha": sha}})
		bySHA[sha] = commitObj(sha, "subject "+name, "Ada", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	}
	bySHA["0000000000000000000000000000000000000000"] = commitObj("0000000000000000000000000000000000000000", "default", "Ada", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	byBranch := map[string]obj{"main": {"name": "main", "commit": bySHA["0000000000000000000000000000000000000000"]}}
	f := stubRoutes(t, branchRoutes(log, list, byBranch, bySHA))

	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("got %d branches, want 100 (default plus first 99 others)", len(got))
	}
	if !got[0].Default || got[0].Name != "main" {
		t.Errorf("first branch = %+v, want the default", got[0])
	}
	for i, b := range got[1:] {
		if want := fmt.Sprintf("b%03d", i); b.Name != want {
			t.Fatalf("branch %d = %q, want %q", i+1, b.Name, want)
		}
	}
	if n := log.count(isCommitDetail); n != 99 {
		t.Errorf("commit detail requested %d times, want 99", n)
	}
}

func TestListBranchesFetchesDefaultWhenNotOnFirstPage(t *testing.T) {
	log := &apiCallLog{}
	list := make([]obj, 0, 100)
	bySHA := map[string]obj{}
	for i := range 100 {
		name := fmt.Sprintf("b%03d", i)
		sha := fmt.Sprintf("%040d", i+1)
		list = append(list, obj{"name": name, "commit": obj{"sha": sha}})
		bySHA[sha] = commitObj(sha, "subject "+name, "Ada", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	}
	const mainSHA = "9999999999999999999999999999999999999999"
	mainCommit := commitObj(mainSHA, "Fix the build", "Ada", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	byBranch := map[string]obj{"main": {"name": "main", "commit": mainCommit}}
	f := stubRoutes(t, branchRoutes(log, list, byBranch, bySHA))

	got, err := f.ListBranches(t.Context(), branchRef)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	main, ok := branchNamed(got, "main")
	if !ok || !main.Default {
		t.Fatalf("default branch missing or not marked default: %+v", got)
	}
	if main.Commit.SHA != mainSHA {
		t.Errorf("default SHA = %q, want %q from the branch detail", main.Commit.SHA, mainSHA)
	}
	defaults := 0
	for _, b := range got {
		if b.Name == "main" {
			defaults++
		}
	}
	if defaults != 1 || len(got) != 100 {
		t.Errorf("got %d branches with %d main entries, want 100 and 1", len(got), defaults)
	}
}

func TestListBranchesDetailFailureFailsTheWholeCall(t *testing.T) {
	for _, status := range []int{404, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			log := &apiCallLog{}
			byBranch, bySHA := fixtureDetails()
			routes := branchRoutes(log, loadList(t, "branches.json"), byBranch, bySHA)
			routes["GET /api/v3/repos/{owner}/{repo}/commits/{sha}"] = func(w http.ResponseWriter, r *http.Request) {
				log.add(r)
				writeRaw(w, status, []byte(`{"message":"boom"}`))
			}
			f := stubRoutes(t, routes)

			got, err := f.ListBranches(t.Context(), branchRef)
			if err == nil {
				t.Fatalf("ListBranches() = %+v, nil; want an error when a commit detail returns %d", got, status)
			}
			if got != nil {
				t.Errorf("ListBranches() returned %+v alongside the error, want nil", got)
			}
		})
	}
}

func TestListCommitsSendsBranchAndPerPage(t *testing.T) {
	log := &apiCallLog{}
	routes := map[string]http.HandlerFunc{
		"GET /api/v3/repos/{owner}/{repo}/commits": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			writeJSON(w, 200, loadList(t, "branches_commits.json"))
		},
	}
	f := stubRoutes(t, routes)

	if _, err := f.ListCommits(t.Context(), branchRef, "main"); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	calls := log.all()
	if len(calls) != 1 || calls[0].path != commitsPath {
		t.Fatalf("calls = %+v, want one request to %s", calls, commitsPath)
	}
	if !strings.Contains(calls[0].query, "sha=main") || !strings.Contains(calls[0].query, "per_page=30") {
		t.Errorf("commits query = %q, want sha=main and per_page=30", calls[0].query)
	}
}

func TestListCommitsEscapesBranchName(t *testing.T) {
	log := &apiCallLog{}
	routes := map[string]http.HandlerFunc{
		"GET /api/v3/repos/{owner}/{repo}/commits": func(w http.ResponseWriter, r *http.Request) {
			log.add(r)
			writeJSON(w, 200, loadList(t, "branches_commits.json"))
		},
	}
	f := stubRoutes(t, routes)

	if _, err := f.ListCommits(t.Context(), branchRef, "a&b#c/d"); err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	calls := log.all()
	if len(calls) != 1 || !strings.Contains(calls[0].query, "sha=a%26b%23c%2Fd") {
		t.Errorf("commits query = %+v, want sha=a%%26b%%23c%%2Fd", calls)
	}
}

func TestListCommitsMapsFields(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET /api/v3/repos/{owner}/{repo}/commits": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, loadList(t, "branches_commits.json"))
		},
	}
	f := stubRoutes(t, routes)

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
	routes := map[string]http.HandlerFunc{
		"GET /api/v3/repos/{owner}/{repo}/commits": func(w http.ResponseWriter, _ *http.Request) {
			writeRaw(w, 404, []byte(`{"message":"Not Found"}`))
		},
	}
	f := stubRoutes(t, routes)

	_, err := f.ListCommits(t.Context(), branchRef, "gone")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListCommits() error = %v, want ErrNotFound", err)
	}
}
