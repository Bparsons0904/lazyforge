package gitea_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

const (
	testToken = "test-token"
	pageCap   = 2 // stand-in for Forgejo's max_response_items, to force multi-page walks
)

type obj = map[string]any

type request struct {
	Method, Path, Query, Body string
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadList(t testing.TB, name string) []obj {
	t.Helper()
	var v []obj
	if err := json.Unmarshal(readFixture(t, name), &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

// server is a fixture-backed Gitea that keeps just enough state for forgetest.RunContract.
type server struct {
	*httptest.Server
	t testing.TB

	mu       sync.Mutex
	log      []request
	prs      map[string][]obj // keyed by owner/repo
	issues   []obj
	comments map[int][]obj
	repos    []obj
	nextID   int
}

func newServer(t testing.TB) *server {
	t.Helper()
	s := &server{
		t: t,
		prs: map[string][]obj{
			"deadstyle/lazyforge": loadList(t, "pulls.json"),
			"deadstyle/waugzee":   loadList(t, "pulls_closed_unmerged.json"),
		},
		issues:   loadList(t, "issues.json"),
		comments: map[int][]obj{17: loadList(t, "issue_comments.json")},
		repos:    loadList(t, "user_repos.json"),
		nextID:   1000,
	}

	mux := http.NewServeMux()
	const p = "/api/v1"
	mux.HandleFunc("GET "+p+"/version", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "version.json"))
	})
	mux.HandleFunc("GET "+p+"/user", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "user.json"))
	})
	mux.HandleFunc("GET "+p+"/user/repos", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.pageList(w, r, s.repos)
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}", s.getRepo)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/pulls", s.listPulls)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/pulls/{index}", s.getPull)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/pulls/{index}/merge", s.mergePull)
	mux.HandleFunc("PATCH "+p+"/repos/{owner}/{repo}/pulls/{index}", s.patchPull)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/pulls/{index}/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, obj{})
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/issues", s.listIssues)
	mux.HandleFunc("PATCH "+p+"/repos/{owner}/{repo}/issues/{index}", s.patchIssue)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/issues/{index}/comments", s.listComments)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/issues/{index}/comments", s.postComment)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/commits/{ref}/status", func(w http.ResponseWriter, r *http.Request) {
		name := "commit_status_none.json"
		if r.PathValue("ref") == "5f5b7243858f450342de15babccedd52a632fe43" {
			name = "commit_status_pr18.json"
		}
		writeRaw(w, 200, readFixture(t, name))
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/releases", func(w http.ResponseWriter, r *http.Request) {
		s.pageList(w, r, loadList(t, "releases.json"))
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Runs  []obj `json:"workflow_runs"`
			Total int   `json:"total_count"`
		}
		if err := json.Unmarshal(readFixture(t, "actions/runs_list.json"), &body); err != nil {
			t.Fatal(err)
		}
		lo, hi, ok := pageBounds(r, len(body.Runs))
		if !ok {
			writeRaw(w, 400, []byte(`{"message":"page required"}`))
			return
		}
		// Real runs listings carry the total in the body, not the X-Total-Count header.
		writeJSON(w, 200, obj{"workflow_runs": body.Runs[lo:hi], "total_count": len(body.Runs)})
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/runs/{id}/jobs", func(w http.ResponseWriter, _ *http.Request) {
		// Unpaginated on the real server: a bare array, limit and page ignored.
		writeJSON(w, 200, loadList(t, "actions/run_jobs.json"))
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/jobs/{id}/logs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(readFixture(t, "actions/job_log.txt"))
	})

	mux.HandleFunc("GET /attachments/contract", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(forgetest.DemoPNG)
	})

	s.Server = httptest.NewServer(s.wrap(mux))
	t.Cleanup(s.Close)
	return s
}

// wrap records every request and enforces the auth header the adapter must send.
func (s *server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		s.mu.Lock()
		s.log = append(s.log, request{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "token "+testToken {
			writeRaw(w, 401, readFixture(s.t, "error_401.json"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.log...)
}

func (s *server) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = nil
}

// requestsTo returns the logged requests with the given method whose path ends in suffix.
func (s *server) requestsTo(method, suffix string) []request {
	var out []request
	for _, r := range s.requests() {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

func (s *server) setMergeStyle(repo, style string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r["full_name"] == repo {
			r["default_merge_style"] = style
		}
	}
}

func pageBounds(r *http.Request, total int) (lo, hi int, ok bool) {
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		return 0, 0, false
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > pageCap {
		limit = pageCap
	}
	lo = min((page-1)*limit, total)
	return lo, min(lo+limit, total), true
}

// pageList writes one capped page of items with X-Total-Count; callers hold s.mu if items are shared state.
func (s *server) pageList(w http.ResponseWriter, r *http.Request, items []obj) {
	lo, hi, ok := pageBounds(r, len(items))
	if !ok {
		writeRaw(w, 400, []byte(`{"message":"page required"}`))
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(items)))
	writeJSON(w, 200, append([]obj{}, items[lo:hi]...))
}

func (s *server) getRepo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, repo := range s.repos {
		if repo["full_name"] == r.PathValue("owner")+"/"+r.PathValue("repo") {
			writeJSON(w, 200, repo)
			return
		}
	}
	s.notFound(w)
}

func (s *server) notFound(w http.ResponseWriter) {
	writeRaw(w, 404, readFixture(s.t, "error_404.json"))
}

func (s *server) findPull(r *http.Request) obj {
	idx, _ := strconv.Atoi(r.PathValue("index"))
	for _, pr := range s.prs[r.PathValue("owner")+"/"+r.PathValue("repo")] {
		if int(pr["number"].(float64)) == idx {
			return pr
		}
	}
	return nil
}

func (s *server) listPulls(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := r.URL.Query().Get("state")
	var items []obj
	for _, pr := range s.prs[r.PathValue("owner")+"/"+r.PathValue("repo")] {
		if state == "all" || pr["state"] == state {
			items = append(items, pr)
		}
	}
	s.pageList(w, r, items)
}

func (s *server) getPull(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pr := s.findPull(r); pr != nil {
		writeJSON(w, 200, pr)
		return
	}
	s.notFound(w)
}

func (s *server) mergePull(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr := s.findPull(r)
	if pr == nil {
		s.notFound(w)
		return
	}
	if pr["merged"] == true {
		writeJSON(w, 405, obj{"message": "PR already merged"})
		return
	}
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body["head_commit_id"] != pr["head"].(obj)["sha"] {
		writeRaw(w, 409, readFixture(s.t, "merge_head_out_of_date.json"))
		return
	}
	pr["state"], pr["merged"] = "closed", true
	w.WriteHeader(200)
}

func (s *server) patchPull(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr := s.findPull(r)
	if pr == nil {
		s.notFound(w)
		return
	}
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	if st, ok := body["state"]; ok {
		pr["state"] = st
	}
	writeJSON(w, 201, pr)
}

func (s *server) findIssue(r *http.Request) obj {
	idx, _ := strconv.Atoi(r.PathValue("index"))
	for _, is := range s.issues {
		if int(is["number"].(float64)) == idx {
			return is
		}
	}
	return nil
}

func (s *server) listIssues(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	if q.Get("type") != "issues" {
		writeRaw(w, 400, []byte(`{"message":"type=issues required"}`))
		return
	}
	var items []obj
	for _, is := range s.issues {
		if is["state"] == q.Get("state") {
			items = append(items, is)
		}
	}
	s.pageList(w, r, items)
}

func (s *server) patchIssue(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.findIssue(r)
	if is == nil {
		s.notFound(w)
		return
	}
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	for _, k := range []string{"state", "body"} {
		if v, ok := body[k]; ok {
			is[k] = v
		}
	}
	writeJSON(w, 201, is)
}

func (s *server) listComments(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := strconv.Atoi(r.PathValue("index"))
	// Comments are unpaginated in the API; returning everything at once is the behavior under test.
	writeJSON(w, 200, append([]obj{}, s.comments[idx]...))
}

func (s *server) postComment(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := strconv.Atoi(r.PathValue("index"))
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.nextID++
	c := obj{"id": s.nextID, "body": body["body"], "user": obj{"login": "deadstyle"}, "created_at": "2026-10-02T12:00:00Z"}
	s.comments[idx] = append(s.comments[idx], c)
	writeJSON(w, 201, c)
}

func writeRaw(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	writeRaw(w, status, b)
}
