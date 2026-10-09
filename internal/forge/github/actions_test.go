package github_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestListRunsMapsFieldsAndFoldsStatus(t *testing.T) {
	f, s := newForge(t)
	got, err := f.ListRuns(t.Context(), cli, forge.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	const merge14634 = "Merge pull request #14634 from cli/dependabot/go_modules/github.com/m…"
	const bump = "chore(deps): bump github.com/mattn/go-colorable from 0.1.15 to 0.1.16"
	const depBranch = "dependabot/go_modules/github.com/mattn/go-colorable-0.1.16"
	want := []domain.Run{
		{
			ID: 37946858179, Number: 22765, Workflow: "Unit and Integration Tests", Title: merge14634,
			Branch: "trunk", Commit: "ec5b512045db67e5a2a4ff4a1b02660b2fb24390", Event: "push",
			Status: domain.CIRunning, StartedAt: mustTime(t, "2026-10-09T14:47:48Z"), Duration: 0,
			WebURL: "https://github.com/cli/cli/actions/runs/37946858179",
		},
		{
			ID: 37946858122, Number: 17930, Workflow: "Lint", Title: merge14634,
			Branch: "trunk", Commit: "ec5b512045db67e5a2a4ff4a1b02660b2fb24390", Event: "push",
			Status: domain.CIPass, StartedAt: mustTime(t, "2026-10-09T14:47:48Z"), Duration: 2*time.Minute + 46*time.Second,
			WebURL: "https://github.com/cli/cli/actions/runs/37946858122",
		},
		{
			ID: 37941367784, Number: 3181, Workflow: "PR Triaging", Title: bump,
			Branch: depBranch, Commit: openHead, Event: "pull_request_target",
			Status: domain.CISkipped, StartedAt: mustTime(t, "2026-10-09T14:03:37Z"), Duration: 9 * time.Second,
			WebURL: "https://github.com/cli/cli/actions/runs/37941367784",
		},
		{
			ID: 37915254425, Number: 17924, Workflow: "Lint", Title: "Avoid spawning git for each extension at startup (#14620)",
			Branch: "trunk", Commit: "a45729e87739152888e3d4ab708c18721dc2d028", Event: "push",
			Status: domain.CIFail, StartedAt: mustTime(t, "2026-10-09T10:03:45Z"), Duration: 2*time.Minute + 30*time.Second,
			WebURL: "https://github.com/cli/cli/actions/runs/37915254425",
		},
		{
			ID: 37941364158, Number: 22763, Workflow: "Unit and Integration Tests", Title: bump,
			Branch: depBranch, Commit: openHead, Event: "pull_request",
			Status: domain.CIPending, StartedAt: mustTime(t, "2026-10-09T14:03:35Z"), Duration: 0,
			WebURL: "https://github.com/cli/cli/actions/runs/37941364158",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runs:\n got %+v\nwant %+v", got, want)
	}
	// The server always offers a next page; following it would page through a busy repo's whole history.
	reqs := s.requestsTo("GET", "/actions/runs")
	if len(reqs) != 1 {
		t.Fatalf("%d list requests, want 1: only the newest page", len(reqs))
	}
	if q, _ := url.ParseQuery(reqs[0].Query); q.Get("per_page") != "100" || q.Has("page") {
		t.Fatalf("query %q, want per_page=100 and no page", reqs[0].Query)
	}
}

func TestListRunsFilters(t *testing.T) {
	tests := []struct {
		name    string
		flt     forge.RunFilter
		query   url.Values
		wantIDs []int64
	}{
		{"bare branch name", forge.RunFilter{Branch: "trunk"}, url.Values{"branch": {"trunk"}}, []int64{37946858179, 37946858122, 37915254425}},
		{"head sha", forge.RunFilter{HeadSHA: openHead}, url.Values{"head_sha": {openHead}}, []int64{37941367784, 37941364158}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForge(t)
			got, err := f.ListRuns(t.Context(), cli, tt.flt)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, r := range got {
				ids = append(ids, r.ID)
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) {
				t.Fatalf("ids %v, want %v", ids, tt.wantIDs)
			}
			q, _ := url.ParseQuery(s.requestsTo("GET", "/actions/runs")[0].Query)
			for k, v := range tt.query {
				if !reflect.DeepEqual(q[k], v) {
					t.Fatalf("query %s = %v, want %v", k, q[k], v)
				}
			}
		})
	}
}

func TestListJobsPagesAndMaps(t *testing.T) {
	f, s := newForge(t)
	got, err := f.ListJobs(t.Context(), cli, jobsRun)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Job{
		{ID: 113860997440, RunID: jobsRun, Name: "build (ubuntu-latest)", Status: domain.CIPass, Attempt: 1},
		{ID: 113860997547, RunID: jobsRun, Name: "build (windows-latest)", Status: domain.CIPass, Attempt: 1},
		{ID: 113860997171, RunID: jobsRun, Name: "integration-tests (windows-latest)", Status: domain.CIPass, Attempt: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("jobs:\n got %+v\nwant %+v", got, want)
	}
	if n := len(s.requestsTo("GET", "/jobs")); n != 2 {
		t.Fatalf("%d job requests, want 2", n)
	}
}

func TestJobLogStreamsFromRedirectWithoutToken(t *testing.T) {
	f, s := newForge(t)
	head := readFixture(t, "job_log.txt")
	for i := range 2 {
		rc, err := f.JobLog(t.Context(), cli, logJob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = rc.Close() })
		// The log host holds the tail back until release, so reading the head proves JobLog didn't buffer.
		buf := make([]byte, len(head))
		if _, err := io.ReadFull(rc, buf); err != nil {
			t.Fatal(err)
		}
		if string(buf) != string(head) {
			t.Fatalf("call %d: head %q, want the fixture", i, buf)
		}
		if i == 0 {
			close(s.logs.release)
		}
		rest, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if string(rest) != logTail {
			t.Fatalf("call %d: tail %q, want %q", i, rest, logTail)
		}
	}
	seen := s.logs.headers()
	if len(seen) != 2 {
		t.Fatalf("log host saw %d requests, want 2", len(seen))
	}
	for _, h := range seen {
		if a := h.Get("Authorization"); a != "" {
			t.Fatalf("token sent to the log host: %q", a)
		}
		if h.Get("If-None-Match") != "" {
			t.Fatal("log request revalidated from the ETag cache")
		}
	}
	for _, r := range s.requestsTo("GET", "/logs") {
		if r.Status != 302 || r.IfNoneMatch != "" {
			t.Fatalf("API log request %+v, want a plain 302", r)
		}
	}
}

// A redirect to the API's hostname on another port is a different origin, but Go's own redirect rule
// matches on hostname only and would forward the token there.
func TestJobLogDropsTokenOnSameHostOtherPortRedirect(t *testing.T) {
	f, s := newForge(t)
	close(s.logs.release)
	const job = 42
	s.redirectLog(job, s.logs.URL+"/actions-results/job-logs.txt?sig=signed")
	rc, err := f.JobLog(t.Context(), cli, job)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatal(err)
	}
	seen := s.logs.headers()
	if len(seen) != 1 {
		t.Fatalf("log host saw %d requests, want 1", len(seen))
	}
	if a := seen[0].Get("Authorization"); a != "" {
		t.Fatalf("token sent to %s: %q", s.logs.URL, a)
	}
}

func TestJobLogErrorOmitsSignedURL(t *testing.T) {
	f, s := newForge(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	const job = 43
	s.redirectLog(job, deadURL+"/job-logs.txt?sig=secret")
	rc, err := f.JobLog(t.Context(), cli, job)
	if err == nil {
		_ = rc.Close()
		t.Fatal("JobLog succeeded against a closed log host")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error carries the signed URL: %v", err)
	}
}

func TestJobLogUnknownJobIsNotFound(t *testing.T) {
	f, _ := newForge(t)
	if _, err := f.JobLog(t.Context(), cli, 1); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
