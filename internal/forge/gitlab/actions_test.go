package gitlab_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitlab"
)

var (
	_ forge.RunLister = (*gitlab.Forge)(nil)
	_ forge.LogReader = (*gitlab.Forge)(nil)
	_ forge.Labeler   = (*gitlab.Forge)(nil)
)

const (
	rtProject = "/projects/group%2Fsub%2Frepo"
	rtToken   = "rt-token"
)

var rtRepo = domain.RepoRef{Owner: "group/sub", Name: "repo"}

type stubReq struct{ Method, URI, Token, Body string }

// stubAPI answers /user and /metadata so gitlab.New connects, and hands every other request to handle.
type stubAPI struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []stubReq
}

func newStub(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, path string, body string)) (*gitlab.Forge, *stubAPI) {
	t.Helper()
	s := &stubAPI{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4")
		switch path {
		case "/user":
			_, _ = io.WriteString(w, `{"username":"tester"}`)
			return
		case "/metadata":
			_, _ = io.WriteString(w, `{"version":"17.0.0"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, stubReq{r.Method, r.RequestURI, r.Header.Get("PRIVATE-TOKEN"), string(b)})
		s.mu.Unlock()
		handle(w, r, path, string(b))
	}))
	t.Cleanup(s.Close)
	f, err := gitlab.New(t.Context(), s.URL, rtToken, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func (s *stubAPI) requests() []stubReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stubReq(nil), s.reqs...)
}

func (s *stubAPI) requestsTo(method, contains string) []stubReq {
	var out []stubReq
	for _, r := range s.requests() {
		if r.Method == method && strings.Contains(r.URI, contains) {
			out = append(out, r)
		}
	}
	return out
}

func rtFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rtPage serves page r's "page" query (default 1) of items at two per page, chained by an absolute Link rel="next".
func rtPage(t *testing.T, w http.ResponseWriter, r *http.Request, raw []byte) {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	const size = 2
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if _, err := fmt.Sscan(p, &page); err != nil {
			t.Fatal(err)
		}
	}
	lo := min((page-1)*size, len(items))
	hi := min(lo+size, len(items))
	if hi < len(items) {
		q := r.URL.Query()
		q.Set("page", fmt.Sprint(page+1))
		next := url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: q.Encode()}
		w.Header().Set("Link", `<`+next.String()+`>; rel="next"`)
	}
	out, _ := json.Marshal(items[lo:hi])
	_, _ = w.Write(out)
}

func rtNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"message":"404 Not found"}`)
}

func TestListRunsMapsPipelines(t *testing.T) {
	body := rtFixture(t, "actions/pipelines.json")
	f, s := newStub(t, func(w http.ResponseWriter, _ *http.Request, path, _ string) {
		if path != rtProject+"/pipelines" {
			rtNotFound(w)
			return
		}
		_, _ = w.Write(body)
	})
	got, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Run{
		{
			ID: 105, Number: 15, Workflow: "Build and test", Branch: "main",
			Commit: "7c1f0b2e9d4a53b8a6e17d02f5c3b9a41e8d6f70", Event: "push", Status: domain.CIPass,
			StartedAt: time.Date(2026, 9, 1, 10, 0, 5, 0, time.UTC), Duration: 312 * time.Second,
			WebURL: "https://gitlab.example.com/group/sub/repo/-/pipelines/105",
		},
		{
			ID: 104, Number: 14, Workflow: "merge_request_event", Branch: "renovate/example-lib-1.x",
			Commit: "ffff1111d4a53b8a6e17d02f5c3b9a41e8d6f70", Event: "merge_request_event", Status: domain.CIRunning,
			StartedAt: time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC),
			WebURL:    "https://gitlab.example.com/group/sub/repo/-/pipelines/104",
		},
		{
			ID: 103, Number: 13, Workflow: "Docs", Branch: "docs/changelog",
			Commit: "5e5e5e5e9d4a53b8a6e17d02f5c3b9a41e8d6f70", Event: "web", Status: domain.CICancelled,
			StartedAt: time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC),
			WebURL:    "https://gitlab.example.com/group/sub/repo/-/pipelines/103",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("%d runs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g := got[i]
		g.Title = ""
		if !g.StartedAt.Equal(want[i].StartedAt) {
			t.Errorf("run %d StartedAt %v, want %v", want[i].ID, g.StartedAt, want[i].StartedAt)
		}
		g.StartedAt, want[i].StartedAt = time.Time{}, time.Time{}
		if g != want[i] {
			t.Errorf("run %d:\n got %+v\nwant %+v", want[i].ID, g, want[i])
		}
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].Token != rtToken {
		t.Errorf("requests %+v, want one carrying the configured token", reqs)
	}
}

func TestListRunsAsksForOneNewestFirstPage(t *testing.T) {
	f, s := newStub(t, func(w http.ResponseWriter, r *http.Request, _, _ string) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", `<http://`+r.Host+`/api/v4`+rtProject+`/pipelines?page=2>; rel="next"`)
		}
		_, _ = io.WriteString(w, `[]`)
	})
	if _, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want exactly one page: %+v", len(reqs), reqs)
	}
	u, err := url.Parse(reqs[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, v := range map[string]string{"per_page": "100", "order_by": "id", "sort": "desc"} {
		if q.Get(k) != v {
			t.Errorf("query %s=%q, want %q (%s)", k, q.Get(k), v, reqs[0].URI)
		}
	}
	for _, k := range []string{"ref", "sha"} {
		if q.Has(k) {
			t.Errorf("empty RunFilter sent %s=%q", k, q.Get(k))
		}
	}
}

func TestListRunsSendsFilter(t *testing.T) {
	f, s := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { _, _ = io.WriteString(w, `[]`) })
	flt := forge.RunFilter{Branch: "renovate/example-lib-1.x", HeadSHA: "7c1f0b2e9d4a53b8a6e17d02f5c3b9a41e8d6f70"}
	if _, err := f.ListRuns(t.Context(), rtRepo, flt); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	u, err := url.Parse(reqs[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("ref"); got != flt.Branch {
		t.Errorf("ref=%q, want the bare branch %q", got, flt.Branch)
	}
	if got := u.Query().Get("sha"); got != flt.HeadSHA {
		t.Errorf("sha=%q, want %q", got, flt.HeadSHA)
	}
}

func TestListRunsFoldsStatus(t *testing.T) {
	cases := map[string]domain.CIState{
		"success":              domain.CIPass,
		"failed":               domain.CIFail,
		"canceled":             domain.CICancelled,
		"running":              domain.CIRunning,
		"skipped":              domain.CISkipped,
		"pending":              domain.CIPending,
		"created":              domain.CIPending,
		"manual":               domain.CIPending,
		"waiting_for_resource": domain.CIPending,
		"a_status_from_2030":   domain.CIPending,
	}
	for status, want := range cases {
		t.Run(status, func(t *testing.T) {
			f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) {
				_, _ = fmt.Fprintf(w, `[{"id":1,"iid":1,"ref":"main","sha":"abc","status":%q,"source":"push","created_at":"2026-09-01T10:00:00Z"}]`, status)
			})
			got, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Status != want {
				t.Fatalf("status %q folded to %+v, want %v", status, got, want)
			}
		})
	}
}

func TestListRunsFoldMatchesChangeRequestPipeline(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) {
		_, _ = io.WriteString(w, `[{"id":1,"iid":1,"ref":"main","sha":"abc","status":"canceling","source":"push","created_at":"2026-09-01T10:00:00Z"}]`)
	})
	got, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != domain.CICancelled {
		t.Fatalf("canceling folded to %+v, want CICancelled as #138's fold does", got)
	}
}

func TestListRunsNotFound(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { rtNotFound(w) })
	if _, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{}); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListJobsMapsStageAndFollowsPages(t *testing.T) {
	body := rtFixture(t, "actions/jobs.json")
	f, s := newStub(t, func(w http.ResponseWriter, r *http.Request, path, _ string) {
		if path != rtProject+"/pipelines/105/jobs" {
			rtNotFound(w)
			return
		}
		rtPage(t, w, r, body)
	})
	got, err := f.ListJobs(t.Context(), rtRepo, 105)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Job{
		{ID: 501, RunID: 105, Name: "compile", Stage: "build", Status: domain.CIPass, Attempt: 1},
		{ID: 502, RunID: 105, Name: "unit", Stage: "test", Status: domain.CIFail, Attempt: 1},
		{ID: 503, RunID: 105, Name: "lint", Stage: "test", Status: domain.CIRunning, Attempt: 1},
		{ID: 504, RunID: 105, Name: "deploy", Stage: "deploy", Status: domain.CIPending, Attempt: 1},
		{ID: 505, RunID: 105, Name: "docs", Stage: "deploy", Status: domain.CISkipped, Attempt: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("%d jobs, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("job %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	if n := len(s.requests()); n != 3 {
		t.Errorf("%d requests, want 3 pages of two", n)
	}
	for _, r := range s.requests() {
		if strings.Contains(r.URI, "include_retried") || strings.Contains(r.URI, "trigger_jobs") {
			t.Errorf("request %q asks for retried or trigger jobs", r.URI)
		}
	}
	if reqs := s.requests(); len(reqs) == 0 || !strings.Contains(reqs[0].URI, "per_page=100") {
		t.Errorf("first request %+v lacks per_page=100", reqs)
	}
}

func TestListJobsNotFound(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { rtNotFound(w) })
	if _, err := f.ListJobs(t.Context(), rtRepo, 9999); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// rtHoldingLog writes head, flushes, and holds the tail until release closes.
func rtHoldingLog(head, tail []byte) (http.HandlerFunc, func()) {
	release := make(chan struct{})
	var once sync.Once
	h := func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(head)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			return
		}
		_, _ = w.Write(tail)
	}
	return h, func() { once.Do(func() { close(release) }) }
}

func TestJobLogStreamsTraceUntouched(t *testing.T) {
	trace := rtFixture(t, "actions/trace.txt")
	head, tail := trace[:len(trace)/2], trace[len(trace)/2:]
	hold, release := rtHoldingLog(head, tail)
	t.Cleanup(release)
	f, s := newStub(t, func(w http.ResponseWriter, r *http.Request, path, _ string) {
		if path != rtProject+"/jobs/502/trace" {
			rtNotFound(w)
			return
		}
		hold(w, r)
	})
	rc, err := f.JobLog(t.Context(), rtRepo, 502)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	// The server holds the tail back, so reading the head first proves JobLog didn't buffer the whole log.
	buf := make([]byte, len(head))
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(head) {
		t.Fatalf("head %q, want %q", buf, head)
	}
	release()
	rest, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf)+string(rest) != string(trace) {
		t.Fatalf("log changed in transit:\n got %q\nwant %q", string(buf)+string(rest), trace)
	}
	if reqs := s.requestsTo("GET", "/jobs/502/trace"); len(reqs) != 1 || reqs[0].Token != rtToken {
		t.Fatalf("trace requests %+v, want one carrying the token", reqs)
	}
}

func TestJobLogNeverSendsTokenOffTheAPIOrigin(t *testing.T) {
	trace := rtFixture(t, "actions/trace.txt")
	var mu sync.Mutex
	var seen []http.Header
	logs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		_, _ = w.Write(trace)
	}))
	t.Cleanup(logs.Close)
	f, _ := newStub(t, func(w http.ResponseWriter, r *http.Request, _, _ string) {
		http.Redirect(w, r, logs.URL+"/job-logs.txt?sig=signed", http.StatusFound)
	})
	rc, err := f.JobLog(t.Context(), rtRepo, 502)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(trace) {
		t.Fatalf("log %q, want the redirect target's body", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("log host saw %d requests, want 1", len(seen))
	}
	for k, v := range seen[0] {
		for _, val := range v {
			if strings.Contains(val, rtToken) {
				t.Errorf("token leaked to the log host in header %s", k)
			}
		}
	}
	if seen[0].Get("Private-Token") != "" {
		t.Error("PRIVATE-TOKEN sent to the log host")
	}
}

func TestJobLogErrorsDoNotLeakTheToken(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { rtNotFound(w) })
	rc, err := f.JobLog(t.Context(), rtRepo, 1)
	if rc != nil {
		_ = rc.Close()
	}
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if strings.Contains(err.Error(), rtToken) {
		t.Fatalf("error %q contains the token", err)
	}
}
