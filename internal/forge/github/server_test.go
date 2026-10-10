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
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
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
	labels   []obj
	itemLbls map[int][]obj
	logs     *logHost
	// job ID → where its log endpoint redirects
	logRedirects map[string]string
	assets       *assetHost
	// attachment ID → where /user-attachments/assets/{id} redirects
	assetRedirects map[string]string
}

// redirectLog makes the log endpoint for job id redirect to loc.
func (s *server) redirectLog(id int64, loc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logRedirects[strconv.FormatInt(id, 10)] = loc
}

// fixture reads a fixture from a handler goroutine, where t.Fatal is not allowed: it records the
// failure and answers 500 instead.
func (s *server) fixture(w http.ResponseWriter, name string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		s.t.Errorf("%s: %v", name, err)
		w.WriteHeader(http.StatusInternalServerError)
		return nil, false
	}
	return b, true
}

func (s *server) writeFixture(w http.ResponseWriter, status int, name string) {
	if b, ok := s.fixture(w, name); ok {
		writeRaw(w, status, b)
	}
}

func (s *server) decodeFixture(w http.ResponseWriter, name string, v any) bool {
	b, ok := s.fixture(w, name)
	if !ok {
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		s.t.Errorf("%s: %v", name, err)
		w.WriteHeader(http.StatusInternalServerError)
		return false
	}
	return true
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
		labels:   loadList(t, "labels.json"),
		itemLbls: map[int][]obj{openIssue: loadList(t, "issue_labels.json")},
		logs:     newLogHost(t),
	}
	s.logRedirects = map[string]string{strconv.FormatInt(logJob, 10): s.logs.signedURL()}
	s.assets = newAssetHost(t)
	s.assetRedirects = map[string]string{assetID: s.assets.URL + "/" + assetID + ".png?X-Amz-Signature=signed"}

	mux := http.NewServeMux()
	const p = apiPrefix
	mux.HandleFunc("GET "+p+"/meta", func(w http.ResponseWriter, _ *http.Request) {
		s.writeFixture(w, 200, "meta_ghes.json")
	})
	mux.HandleFunc("GET "+p+"/user", func(w http.ResponseWriter, _ *http.Request) {
		s.writeFixture(w, 200, "user.json")
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
	mux.HandleFunc("PUT "+p+"/repos/{owner}/{repo}/pulls/{index}/update-branch", s.updateBranch)
	mux.HandleFunc("POST "+p+"/repos/{owner}/{repo}/actions/workflows/{workflow}/dispatches", s.dispatchWorkflow)
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
		if r.PathValue("ref") == openHead && !s.decodeFixture(w, "check_runs.json", &body) {
			return
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
		s.writeFixture(w, 200, name)
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/releases", func(w http.ResponseWriter, r *http.Request) {
		var releases []obj
		if s.decodeFixture(w, "releases.json", &releases) {
			s.pageList(w, r, releases)
		}
	})

	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/runs", s.listRuns)
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/runs/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Jobs []obj `json:"jobs"`
		}
		if r.PathValue("id") == strconv.FormatInt(jobsRun, 10) && !s.decodeFixture(w, "actions_jobs.json", &body) {
			return
		}
		lo, hi := s.pageBounds(w, r, len(body.Jobs))
		writeJSON(w, 200, obj{"total_count": len(body.Jobs), "jobs": append([]obj{}, body.Jobs[lo:hi]...)})
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/actions/jobs/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		loc, ok := s.logRedirects[r.PathValue("id")]
		s.mu.Unlock()
		if !ok {
			s.notFound(w)
			return
		}
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/labels", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.pageList(w, r, s.labels)
	})
	mux.HandleFunc("GET "+p+"/repos/{owner}/{repo}/issues/{index}/labels", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		idx, _ := strconv.Atoi(r.PathValue("index"))
		s.pageList(w, r, s.itemLbls[idx])
	})
	mux.HandleFunc("PUT "+p+"/repos/{owner}/{repo}/issues/{index}/labels", s.setLabels)
	mux.HandleFunc("GET /user-attachments/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		loc, ok := s.assetRedirects[r.PathValue("id")]
		s.mu.Unlock()
		if !ok {
			s.notFound(w)
			return
		}
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	})

	s.Server = httptest.NewServer(s.wrap(mux))
	t.Cleanup(s.Close)
	return s
}

// wrap enforces the headers the adapter must send (the media type on API paths only), logs every request, and turns a GET whose ETag matches into a 304.
func (s *server) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		switch {
		case r.Header.Get("Authorization") != "Bearer "+testToken:
			writeJSON(rec, 401, obj{"message": "Bad credentials"})
		case strings.HasPrefix(r.URL.Path, apiPrefix+"/") &&
			(r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28"):
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
	s.writeFixture(w, 404, "error_404.json")
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
		s.writeFixture(w, 405, "merge_405.json")
		return
	}
	var body obj
	_ = json.NewDecoder(r.Body).Decode(&body)
	if sha, ok := body["sha"]; ok && sha != pr["head"].(obj)["sha"] {
		s.writeFixture(w, 409, "merge_409.json")
		return
	}
	pr["state"], pr["merged_at"] = "closed", time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, 200, obj{"sha": "0000000000000000000000000000000000000000", "merged": true, "message": "Pull Request successfully merged"})
}

// updateBranch answers 202 with GitHub's queued-update message; the head moves later, outside the response.
func (s *server) updateBranch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findPull(r) == nil {
		s.notFound(w)
		return
	}
	writeJSON(w, 202, obj{"message": "Updating pull request branch.", "url": "https://github.com/" + r.PathValue("owner") + "/" + r.PathValue("repo") + "/pull/" + r.PathValue("index")})
}

// dispatchWorkflow answers 204 for any repo the server holds; it doesn't check that the workflow file exists.
func (s *server) dispatchWorkflow(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, repo := range s.repos {
		if repo["full_name"] == r.PathValue("owner")+"/"+r.PathValue("repo") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.notFound(w)
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

const (
	jobsRun = 37942729045  // the run actions_jobs.json was recorded from
	logJob  = 113875256467 // the job job_log.txt was recorded from
)

// listRuns filters on branch and head_sha as GitHub does. It ignores pageCap and offers a next page from
// the first, standing in for a busy repo's long history, so a test catches the adapter following it.
func (s *server) listRuns(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowRuns []obj `json:"workflow_runs"`
	}
	if !s.decodeFixture(w, "actions_runs.json", &body) {
		return
	}
	q := r.URL.Query()
	var runs []obj
	for _, rn := range body.WorkflowRuns {
		if !s.isCLI(r) || (q.Has("branch") && rn["head_branch"] != q.Get("branch")) || (q.Has("head_sha") && rn["head_sha"] != q.Get("head_sha")) {
			continue
		}
		runs = append(runs, rn)
	}
	if !q.Has("page") {
		q.Set("page", "2")
		w.Header().Set("Link", `<`+s.URL+r.URL.Path+"?"+q.Encode()+`>; rel="next"`)
	}
	writeJSON(w, 200, obj{"total_count": 40000, "workflow_runs": runs})
}

// setLabels takes names, as GitHub does, and answers 422 for a name the repo doesn't have.
func (s *server) setLabels(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, _ := strconv.Atoi(r.PathValue("index"))
	var body struct {
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Labels == nil {
		writeJSON(w, 422, obj{"message": "Invalid request.", "errors": []string{"labels is missing"}})
		return
	}
	set := []obj{}
	for _, name := range body.Labels {
		i := slices.IndexFunc(s.labels, func(l obj) bool { return l["name"] == name })
		if i < 0 {
			writeJSON(w, 422, obj{"message": "Validation Failed", "errors": []string{"unknown label " + name}})
			return
		}
		set = append(set, s.labels[i])
	}
	s.itemLbls[idx] = set
	writeJSON(w, 200, set)
}

// logHost stands in for the blob store GitHub's log endpoint redirects to. It listens on "localhost" while
// the API is on 127.0.0.1, so the redirect crosses hosts, and it sends an ETag as the real store does.
// It holds the last line back until release, so a test can read the head before the body is complete.
type logHost struct {
	*httptest.Server
	release chan struct{}

	mu   sync.Mutex
	seen []http.Header
}

func newLogHost(t testing.TB) *logHost {
	t.Helper()
	h := &logHost{release: make(chan struct{})}
	log := readFixture(t, "job_log.txt")
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.seen = append(h.seen, r.Header.Clone())
		h.mu.Unlock()
		if r.URL.Query().Get("sig") != "signed" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"0x8DF2614623C6A82"`)
		_, _ = w.Write(log)
		w.(http.Flusher).Flush()
		select {
		case <-h.release:
			_, _ = io.WriteString(w, logTail)
		case <-time.After(2 * time.Second):
			_, _ = io.WriteString(w, "TIMED OUT: the client buffered the log\n")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(h.Close)
	return h
}

const logTail = "2026-10-09T14:48:28.0000000Z held back until released\n"

func (h *logHost) signedURL() string {
	return strings.Replace(h.URL, "127.0.0.1", "localhost", 1) + "/actions-results/job-logs.txt?sig=signed"
}

func (h *logHost) headers() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.seen)
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

// assetID is the attachment the fake web root serves at /user-attachments/assets/{id}.
const assetID = "0b1c2d3e-4f50-6172-8394-a5b6c7d8e9f0"

// assetHost stands in for the signed object store GitHub's attachment URLs redirect to. It shares the API's
// hostname on another port, a different origin that Go's own redirect rule would still send the token to.
type assetHost struct {
	*httptest.Server

	mu   sync.Mutex
	seen []http.Header
}

func newAssetHost(t testing.TB) *assetHost {
	t.Helper()
	h := &assetHost{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.seen = append(h.seen, r.Header.Clone())
		h.mu.Unlock()
		if r.URL.Query().Get("X-Amz-Signature") != "signed" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(forgetest.DemoPNG)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *assetHost) headers() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.seen)
}
