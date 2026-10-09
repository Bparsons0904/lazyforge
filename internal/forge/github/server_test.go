package github_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	"time"
)

const (
	testToken = "test-token"
	pageCap   = 2 // stand-in for per_page=100, so the fixtures span several Link-chained pages
	apiPrefix = "/api/v3"
)

type obj = map[string]any

type request struct {
	Method, Path, Query, Body, IfNoneMatch string
	Status                                 int
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

func loadObj(t testing.TB, name string) obj {
	t.Helper()
	var v obj
	if err := json.Unmarshal(readFixture(t, name), &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

// server is a fixture-backed GHES API under /api/v3 that keeps just enough state for forgetest.RunContract.
// It answers GETs with a content-hash ETag and honors If-None-Match, as GitHub does.
type server struct {
	*httptest.Server
	t testing.TB

	mu       sync.Mutex
	log      []request
	repos    []obj
	prs      []obj // cli/cli, open and closed
	issues   []obj // cli/cli open issues and PRs, as the issues endpoint returns them
	comments map[int][]obj
	nextID   int
	ciStatus map[string]int // head SHA → status both CI endpoints answer with instead of their fixture
}

const (
	openPR      = 14634
	openHead    = "424f38df651e0b178d752073859af7ee1d3d02c3"
	openIssue   = 14627
	failingHead = "57bfea5f807ffb0c78336679d052ab56217aafde" // served the kubernetes combined status with a failure
)

func newServer(t testing.TB) *server {
	t.Helper()
	s := &server{
		t:        t,
		repos:    append(loadList(t, "user_repos.json"), loadObj(t, "repo_cli.json")),
		prs:      append(loadList(t, "pulls_open.json"), loadList(t, "pulls_closed.json")...),
		issues:   loadList(t, "issues_open.json"),
		comments: map[int][]obj{openIssue: loadList(t, "issue_comments.json")},
		nextID:   1000,
	}

	mux := http.NewServeMux()
	const p = apiPrefix
	mux.HandleFunc("GET "+p+"/meta", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "meta_ghes.json"))
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
	mux.HandleFunc("PUT "+p+"/repos/{owner}/{repo}/pulls/{index}/merge", s.mergePull)
	mux.HandleFunc("PATCH "+p+"/repos/{owner}/{repo}/pulls/{index}", s.patchPull)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/pulls/{index}/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, obj{"state": "APPROVED"})
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/issues", s.listIssues)
	mux.HandleFunc("PATCH "+p+"/repos/{owner}/{repo}/issues/{index}", s.patchIssue)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/issues/{index}/comments", s.listComments)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/issues/{index}/comments", s.postComment)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/commits/{ref}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if s.ciFails(w, r) {
			return
		}
		var body struct {
			CheckRuns []obj `json:"check_runs"`
		}
		if r.PathValue("ref") == openHead {
			if err := json.Unmarshal(readFixture(t, "check_runs.json"), &body); err != nil {
				t.Fatal(err)
			}
		}
		lo, hi := s.pageBounds(w, r, len(body.CheckRuns))
		writeJSON(w, 200, obj{"total_count": len(body.CheckRuns), "check_runs": append([]obj{}, body.CheckRuns[lo:hi]...)})
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/commits/{ref}/status", func(w http.ResponseWriter, r *http.Request) {
		if s.ciFails(w, r) {
			return
		}
		name := "status_none.json"
		if r.PathValue("ref") == failingHead {
			name = "status_failure.json"
		}
		writeRaw(w, 200, readFixture(t, name))
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/releases", func(w http.ResponseWriter, r *http.Request) {
		s.pageList(w, r, loadList(t, "releases.json"))
	})

	s.Server = httptest.NewServer(s.wrap(mux))
	t.Cleanup(s.Close)
	return s
}

// wrap enforces the headers the adapter must send, logs every request, and turns a GET whose ETag matches into a 304.
func (s *server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		switch {
		case r.Header.Get("Authorization") != "Bearer "+testToken:
			writeJSON(rec, 401, obj{"message": "Bad credentials"})
		case r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28":
			writeJSON(rec, 415, obj{"message": "missing GitHub media type or API version"})
		default:
			next.ServeHTTP(rec, r)
		}
		if r.Method == http.MethodGet && rec.Code == 200 {
			sum := sha256.Sum256(rec.Body.Bytes())
			etag := `W/"` + hex.EncodeToString(sum[:]) + `"`
			rec.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				rec = httptest.NewRecorder()
				rec.Header().Set("ETag", etag)
				rec.WriteHeader(http.StatusNotModified)
			}
		}
		s.mu.Lock()
		s.log = append(s.log, request{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.Header.Get("If-None-Match"), rec.Code})
		s.mu.Unlock()
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
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

func (s *server) setMergeFlags(fullName string, mergeCommit, squash, rebase any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.repos {
		if r["full_name"] == fullName {
			r["allow_merge_commit"], r["allow_squash_merge"], r["allow_rebase_merge"] = mergeCommit, squash, rebase
		}
	}
}

// pageBounds reads page and per_page, capped at pageCap, and sets the Link header GitHub sends; it never sends X-Total-Count.
func (s *server) pageBounds(w http.ResponseWriter, r *http.Request, total int) (lo, hi int) {
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	per, err := strconv.Atoi(q.Get("per_page"))
	if err != nil || per < 1 || per > pageCap {
		per = pageCap
	}
	lo = min((page-1)*per, total)
	hi = min(lo+per, total)
	if hi < total {
		q.Set("page", strconv.Itoa(page+1))
		w.Header().Set("Link", `<`+s.URL+r.URL.Path+"?"+q.Encode()+`>; rel="next", <`+s.URL+r.URL.Path+`?page=1>; rel="first"`)
	}
	return lo, hi
}

// pageList writes one capped page of items; callers hold s.mu if items are shared state.
func (s *server) pageList(w http.ResponseWriter, r *http.Request, items []obj) {
	lo, hi := s.pageBounds(w, r, len(items))
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

func (s *server) failCI(sha string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ciStatus == nil {
		s.ciStatus = map[string]int{}
	}
	s.ciStatus[sha] = code
}

func (s *server) ciFails(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	code, ok := s.ciStatus[r.PathValue("ref")]
	s.mu.Unlock()
	if ok {
		writeJSON(w, code, obj{"message": http.StatusText(code)})
	}
	return ok
}

func (s *server) notFound(w http.ResponseWriter) {
	writeRaw(w, 404, readFixture(s.t, "error_404.json"))
}

func (s *server) isCLI(r *http.Request) bool {
	return r.PathValue("owner") == "cli" && r.PathValue("repo") == "cli"
}

func (s *server) findPull(r *http.Request) obj {
	idx, _ := strconv.Atoi(r.PathValue("index"))
	if !s.isCLI(r) {
		return nil
	}
	for _, pr := range s.prs {
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
	for _, pr := range s.prs {
		if s.isCLI(r) && (state == "all" || pr["state"] == state) {
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
	if pr["state"] != "open" {
		writeRaw(w, 405, readFixture(s.t, "merge_405.json"))
		return
	}
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	if sha, ok := body["sha"]; ok && sha != pr["head"].(obj)["sha"] {
		writeRaw(w, 409, readFixture(s.t, "merge_409.json"))
		return
	}
	pr["state"], pr["merged_at"] = "closed", time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, 200, obj{"sha": "0000000000000000000000000000000000000000", "merged": true, "message": "Pull Request successfully merged"})
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
	writeJSON(w, 200, pr)
}

func (s *server) findIssue(r *http.Request) obj {
	idx, _ := strconv.Atoi(r.PathValue("index"))
	if !s.isCLI(r) {
		return nil
	}
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
	state := r.URL.Query().Get("state")
	var items []obj
	for _, is := range s.issues {
		if s.isCLI(r) && (state == "all" || is["state"] == state) {
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
	writeJSON(w, 200, is)
}

func (s *server) listComments(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := strconv.Atoi(r.PathValue("index"))
	s.pageList(w, r, s.comments[idx])
}

func (s *server) postComment(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := strconv.Atoi(r.PathValue("index"))
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.nextID++
	c := obj{"id": s.nextID, "body": body["body"], "user": obj{"login": "Bparsons0904"}, "created_at": "2026-10-09T12:00:00Z"}
	s.comments[idx] = append(s.comments[idx], c)
	writeJSON(w, 201, c)
}

func writeRaw(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	writeRaw(w, status, b)
}
