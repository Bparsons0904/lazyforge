package gitlab_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	apiRoot   = "/api/v4"

	openMR   = 7
	openHead = "7c1f0b2e9d4a53b8a6e17d02f5c3b9a41e8d6f70"
	draftMR  = 8
	draftSHA = "8d2a1c3f0e5b64c9b7f28e13a6d4c0b52f9e7a81"
	openIss  = 3
	missing  = 9999
)

type obj = map[string]any

type request struct {
	Method, URI, Token, Body string
	Status                   int
}

// reply is a canned answer for scripted requests.
type reply struct {
	Status int
	Header map[string]string
	Body   string
}

type scripted struct {
	method, contains string
	replies          []reply
}

// server is a fixture-backed GitLab API under <prefix>/api/v4 that keeps just enough state for forgetest.RunContract.
// It routes on the raw escaped path, so a project path stays one segment when its slashes arrive as %2F.
type server struct {
	*httptest.Server
	t      testing.TB
	prefix string

	mu        sync.Mutex
	log       []request
	script    []*scripted
	projects  []obj
	minAccess []obj
	mrs       []obj
	heads     map[int]obj
	pipelines []obj
	issues    []obj
	notes     map[string][]obj // "issues/3" or "merge_requests/7"
	releases  []obj
	uploads   map[string][]byte // "<full path>/<secret>/<file>", decoded
	nextNote  int
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

func loadHeads(t testing.TB) map[int]obj {
	t.Helper()
	var raw map[string]obj
	if err := json.Unmarshal(readFixture(t, "head_pipelines.json"), &raw); err != nil {
		t.Fatal(err)
	}
	out := map[int]obj{}
	for k, v := range raw {
		n, err := strconv.Atoi(k)
		if err != nil {
			t.Fatal(err)
		}
		out[n] = v
	}
	return out
}

func newServer(t testing.TB) *server { return newServerAt(t, "") }

// newServerAt mounts the API under prefix, as a self-managed GitLab served from a relative URL root is.
func newServerAt(t testing.TB, prefix string) *server {
	t.Helper()
	s := &server{
		t:         t,
		prefix:    prefix,
		projects:  loadList(t, "projects.json"),
		minAccess: loadList(t, "projects_min_access.json"),
		mrs:       loadList(t, "merge_requests.json"),
		heads:     loadHeads(t),
		pipelines: loadList(t, "pipelines.json"),
		issues:    loadList(t, "issues.json"),
		notes: map[string][]obj{
			"issues/3":         loadList(t, "notes_issue_3.json"),
			"merge_requests/7": loadList(t, "notes_mr_7.json"),
		},
		releases: loadList(t, "releases.json"),
		uploads: map[string][]byte{
			uploadKey("group/sub/repo", "abc123", "shot.png"): forgetest.DemoPNG,
			uploadKey("group/sub/repo", "abc123", "a b.png"):  forgetest.DemoPNG,
		},
		nextNote: 1000,
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// webURL is the root a user would type: the server's address plus any relative URL root.
func (s *server) webURL() string { return s.URL + s.prefix }

func (s *server) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = nil
}

func (s *server) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

// requestsTo returns the logged requests with method whose request line contains substr.
func (s *server) requestsTo(method, substr string) []request {
	var out []request
	for _, r := range s.requests() {
		if r.Method == method && strings.Contains(r.URI, substr) {
			out = append(out, r)
		}
	}
	return out
}

// queryOf parses the query of a logged request line.
func queryOf(t testing.TB, uri string) url.Values {
	t.Helper()
	_, raw, _ := strings.Cut(uri, "?")
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// answer scripts replies for the next requests with method whose request line contains substr; once they run out the fake answers normally.
func (s *server) answer(method, substr string, replies ...reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = append(s.script, &scripted{method, substr, replies})
}

func (s *server) setProject(path, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projects {
		if p["path_with_namespace"] == path {
			p[key] = value
		}
	}
}

func (s *server) setProjects(ps []obj) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects = ps
}

func (s *server) setHead(iid int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == "" {
		s.heads[iid] = nil
		return
	}
	s.heads[iid] = obj{"id": 900, "sha": "any", "status": status}
}

func (s *server) setPipelines(ps []obj) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines = ps
}

func (s *server) setMRs(mrs []obj) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mrs = mrs
}

func (s *server) mrState(iid int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mrs {
		if iidOf(m) == iid {
			return m["state"].(string)
		}
	}
	return ""
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	rec := httptest.NewRecorder()
	if rp, ok := s.scripted(r); ok {
		for k, v := range rp.Header {
			rec.Header().Set(k, v)
		}
		rec.WriteHeader(rp.Status)
		_, _ = rec.WriteString(rp.Body)
	} else {
		s.route(rec, r, body)
	}
	s.mu.Lock()
	s.log = append(s.log, request{r.Method, r.RequestURI, r.Header.Get("PRIVATE-TOKEN"), string(body), rec.Code})
	s.mu.Unlock()
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

func (s *server) scripted(r *http.Request) (reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sc := range s.script {
		if len(sc.replies) > 0 && sc.method == r.Method && strings.Contains(r.RequestURI, sc.contains) {
			rp := sc.replies[0]
			sc.replies = sc.replies[1:]
			return rp, true
		}
	}
	return reply{}, false
}

var (
	reProjects = regexp.MustCompile(`^/projects$`)
	reProject  = regexp.MustCompile(`^/projects/([^/]+)$`)
	reColl     = regexp.MustCompile(`^/projects/([^/]+)/(merge_requests|issues|pipelines|pipeline|releases)$`)
	reItem     = regexp.MustCompile(`^/projects/([^/]+)/(merge_requests|issues)/(\d+)$`)
	reMRAction = regexp.MustCompile(`^/projects/([^/]+)/merge_requests/(\d+)/(merge|approve|rebase)$`)
	reUpload   = regexp.MustCompile(`^/projects/([^/]+)/uploads/([^/]+)/([^/]+)$`)
	reNotes    = regexp.MustCompile(`^/projects/([^/]+)/(merge_requests|issues)/(\d+)/notes$`)
)

func (s *server) route(w http.ResponseWriter, r *http.Request, body []byte) {
	p := r.URL.EscapedPath()
	root := s.prefix + apiRoot
	if !strings.HasPrefix(p, root+"/") {
		writeJSON(w, 404, obj{"message": "404 Not Found"})
		return
	}
	p = strings.TrimPrefix(p, root)
	token := r.Header.Get("PRIVATE-TOKEN")
	if token != testToken {
		writeJSON(w, 401, obj{"message": "401 Unauthorized"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case p == "/user" && r.Method == http.MethodGet:
		writeRaw(w, 200, readFixture(s.t, "user.json"))
		return
	case p == "/metadata" && r.Method == http.MethodGet:
		writeRaw(w, 200, readFixture(s.t, "metadata.json"))
		return
	case reProjects.MatchString(p) && r.Method == http.MethodGet:
		list := s.projects
		if r.URL.Query().Get("min_access_level") != "" {
			list = s.minAccess
		}
		s.pageList(w, r, list)
		return
	}
	if m := reProject.FindStringSubmatch(p); m != nil && r.Method == http.MethodGet {
		if pr := s.project(m[1]); pr != nil {
			writeJSON(w, 200, pr)
			return
		}
		s.notFound(w)
		return
	}
	if m := reColl.FindStringSubmatch(p); m != nil {
		if s.project(m[1]) == nil {
			s.notFound(w)
			return
		}
		s.collection(w, r, m[2], body)
		return
	}
	if m := reItem.FindStringSubmatch(p); m != nil {
		if s.project(m[1]) == nil {
			s.notFound(w)
			return
		}
		iid, _ := strconv.Atoi(m[3])
		s.item(w, r, m[2], iid, body)
		return
	}
	if m := reMRAction.FindStringSubmatch(p); m != nil {
		if s.project(m[1]) == nil {
			s.notFound(w)
			return
		}
		iid, _ := strconv.Atoi(m[2])
		s.mrAction(w, r, m[3], iid, body)
		return
	}
	if m := reUpload.FindStringSubmatch(p); m != nil && r.Method == http.MethodGet {
		s.upload(w, m[1], m[2], m[3])
		return
	}
	if m := reNotes.FindStringSubmatch(p); m != nil {
		if s.project(m[1]) == nil {
			s.notFound(w)
			return
		}
		iid, _ := strconv.Atoi(m[3])
		s.notesFor(w, r, m[2], iid, body)
		return
	}
	s.notFound(w)
}

func uploadKey(path, secret, file string) string { return path + "/" + secret + "/" + file }

// upload answers the uploads API; the project is addressed by escaped full path or by numeric id.
func (s *server) upload(w http.ResponseWriter, project, secret, file string) {
	file, err := url.PathUnescape(file)
	if err != nil {
		s.notFound(w)
		return
	}
	pr := s.project(project)
	if pr == nil {
		for _, p := range s.projects {
			if strconv.Itoa(int(p["id"].(float64))) == project {
				pr = p
			}
		}
	}
	if pr == nil {
		s.notFound(w)
		return
	}
	b, ok := s.uploads[uploadKey(pr["path_with_namespace"].(string), secret, file)]
	if !ok {
		s.notFound(w)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// project finds a project by its URL-decoded full path.
func (s *server) project(seg string) obj {
	path, err := url.PathUnescape(seg)
	if err != nil {
		return nil
	}
	for _, p := range s.projects {
		if p["path_with_namespace"] == path {
			return p
		}
	}
	return nil
}

func (s *server) notFound(w http.ResponseWriter) { writeFixture(s.t, w, 404, "error_404.json") }

func (s *server) collection(w http.ResponseWriter, r *http.Request, kind string, _ []byte) {
	if kind == "pipeline" && r.Method == http.MethodPost {
		writeRaw(w, 201, readFixture(s.t, "pipeline_created.json"))
		return
	}
	if r.Method != http.MethodGet {
		s.notFound(w)
		return
	}
	q := r.URL.Query()
	switch kind {
	case "merge_requests":
		var out []obj
		for _, m := range s.mrs {
			if st := q.Get("state"); st == "" || st == "all" || m["state"] == st {
				out = append(out, m)
			}
		}
		s.pageList(w, r, out)
	case "issues":
		var out []obj
		for _, i := range s.issues {
			if st := q.Get("state"); st != "" && st != "all" && i["state"] != st {
				continue
			}
			if it := q.Get("issue_type"); it != "" && i["issue_type"] != it {
				continue
			}
			out = append(out, i)
		}
		s.pageList(w, r, out)
	case "pipelines":
		writeJSON(w, 200, s.pipelines)
	case "releases":
		s.pageList(w, r, s.releases)
	}
}

func (s *server) find(list []obj, iid int) obj {
	for _, o := range list {
		if iidOf(o) == iid {
			return o
		}
	}
	return nil
}

func (s *server) item(w http.ResponseWriter, r *http.Request, kind string, iid int, body []byte) {
	list := s.mrs
	if kind == "issues" {
		list = s.issues
	}
	o := s.find(list, iid)
	if o == nil {
		s.notFound(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if kind == "merge_requests" {
			c := obj{}
			for k, v := range o {
				c[k] = v
			}
			c["head_pipeline"] = s.heads[iid]
			writeJSON(w, 200, c)
			return
		}
		writeJSON(w, 200, o)
	case http.MethodPut:
		var in obj
		_ = json.Unmarshal(body, &in)
		if in["state_event"] == "close" {
			o["state"] = "closed"
		}
		if d, ok := in["description"]; ok {
			o["description"] = d
		}
		writeJSON(w, 200, o)
	default:
		s.notFound(w)
	}
}

func (s *server) mrAction(w http.ResponseWriter, r *http.Request, action string, iid int, body []byte) {
	o := s.find(s.mrs, iid)
	if o == nil {
		s.notFound(w)
		return
	}
	var in obj
	_ = json.Unmarshal(body, &in)
	switch action {
	case "merge":
		if r.Method != http.MethodPut {
			s.notFound(w)
			return
		}
		if o["state"] != "opened" || o["draft"] == true {
			writeFixture(s.t, w, 405, "merge_405.json")
			return
		}
		if sha, _ := in["sha"].(string); sha != "" && sha != o["sha"] {
			writeFixture(s.t, w, 409, "merge_409.json")
			return
		}
		o["state"] = "merged"
		writeJSON(w, 200, o)
	case "approve":
		if r.Method != http.MethodPost {
			s.notFound(w)
			return
		}
		writeJSON(w, 201, obj{"iid": iid, "approved_by": []obj{{"user": obj{"username": "alice"}}}})
	case "rebase":
		if r.Method != http.MethodPut {
			s.notFound(w)
			return
		}
		writeJSON(w, 202, obj{"rebase_in_progress": true})
	}
}

func (s *server) notesFor(w http.ResponseWriter, r *http.Request, kind string, iid int, body []byte) {
	key := kind + "/" + strconv.Itoa(iid)
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		out := slices.Clone(s.notes[key])
		desc := q.Get("sort") != "asc"
		slices.SortStableFunc(out, func(a, b obj) int {
			c := strings.Compare(a["created_at"].(string), b["created_at"].(string))
			if desc {
				return -c
			}
			return c
		})
		s.pageList(w, r, out)
	case http.MethodPost:
		var in obj
		_ = json.Unmarshal(body, &in)
		s.nextNote++
		n := obj{
			"id": s.nextNote, "body": in["body"], "system": false,
			"author":     obj{"username": "alice"},
			"created_at": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		}
		s.notes[key] = append(s.notes[key], n)
		writeJSON(w, 201, n)
	default:
		s.notFound(w)
	}
}

// pageList serves one offset page of list, capped at pageCap, with the Link header GitLab sends; it never sends X-Total.
func (s *server) pageList(w http.ResponseWriter, r *http.Request, list []obj) {
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	size := pageCap
	if n, err := strconv.Atoi(q.Get("per_page")); err == nil && n > 0 && n < size {
		size = n
	}
	lo := min((page-1)*size, len(list))
	hi := min(lo+size, len(list))
	if hi < len(list) {
		q.Set("page", strconv.Itoa(page+1))
		next := "http://" + r.Host + r.URL.EscapedPath() + "?" + q.Encode()
		w.Header().Set("Link", "<"+next+`>; rel="next"`)
	}
	writeJSON(w, 200, append([]obj{}, list[lo:hi]...))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	writeRaw(w, status, b)
}

func writeRaw(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeFixture(t testing.TB, w http.ResponseWriter, status int, name string) {
	writeRaw(w, status, readFixture(t, name))
}

// iidOf reads an iid seeded from JSON (float64) or from a Go literal (int).
func iidOf(o obj) int {
	if n, ok := o["iid"].(int); ok {
		return n
	}
	return int(o["iid"].(float64))
}
