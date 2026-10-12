package gitlab_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitlab"
)

var (
	_ forge.ReadmeReader  = (*gitlab.Forge)(nil)
	_ forge.BranchReader  = (*gitlab.Forge)(nil)
	_ forge.TreeReader    = (*gitlab.Forge)(nil)
	_ forge.BranchUpdater = (*gitlab.Forge)(nil)
)

// repoRef travels as group%2Fsub%2Frepo, the form every route below is keyed on.
var repoRef = domain.RepoRef{Owner: "group/sub", Name: "repo"}

const (
	repoProject = "/projects/group%2Fsub%2Frepo"
	repoTree    = repoProject + "/repository/tree"
	repoFiles   = repoProject + "/repository/files/"
)

// repoCall is one request the fake GitLab saw after the Forge was connected.
type repoCall struct {
	Method, Path string // Path is the escaped path under /api/v4
	Query        url.Values
}

// repoAPI answers requests by "METHOD escaped-path" and answers 404 to the rest.
type repoAPI struct {
	mu    sync.Mutex
	calls []repoCall
}

func (a *repoAPI) requests() []repoCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]repoCall(nil), a.calls...)
}

func (a *repoAPI) count(method, path string) int {
	n := 0
	for _, c := range a.requests() {
		if c.Method == method && c.Path == path {
			n++
		}
	}
	return n
}

// newRepoForge connects a Forge to a fake that serves the project at repoProject with default branch main,
// unless routes overrides that route.
func newRepoForge(t *testing.T, routes map[string]http.HandlerFunc) (*gitlab.Forge, *repoAPI) {
	t.Helper()
	api := &repoAPI{}
	connected := false
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4")
		mu.Lock()
		if connected {
			api.mu.Lock()
			api.calls = append(api.calls, repoCall{Method: r.Method, Path: path, Query: r.URL.Query()})
			api.mu.Unlock()
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch key := r.Method + " " + path; {
		case routes[key] != nil:
			routes[key](w, r)
		case key == "GET /user":
			_, _ = w.Write([]byte(`{"id":1,"username":"alice"}`))
		case key == "GET /metadata":
			_, _ = w.Write([]byte(`{"version":"17.7.0"}`))
		case key == "GET "+repoProject:
			_, _ = w.Write([]byte(`{"id":42,"default_branch":"main","path_with_namespace":"group/sub/repo"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	f, err := gitlab.New(t.Context(), srv.URL, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	connected = true
	mu.Unlock()
	return f, api
}

func repoFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "repo", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func repoServe(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }
}

func repoStatus(code int, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = fmt.Fprintf(w, `{"message":%q}`, msg)
	}
}

// repoTreeJSON is a root listing of the given blob names.
func repoTreeJSON(entries ...[2]string) []byte {
	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf(`{"id":"abc","name":%q,"type":%q,"path":%q,"mode":"100644"}`, e[0], e[1], e[0]))
	}
	return []byte("[" + strings.Join(parts, ",") + "]")
}

func repoFileJSON(name, body string) http.HandlerFunc {
	return repoServe([]byte(fmt.Sprintf(`{"file_name":%q,"file_path":%q,"encoding":"base64","content":%q}`,
		name, name, base64.StdEncoding.EncodeToString([]byte(body)))))
}

const readmeBody = "# Example\n\nA GitLab project.\n"

func TestGetReadmeDecodesFixture(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree:                repoServe(repoTreeJSON([2]string{"README.md", "blob"}, [2]string{"src", "tree"})),
		"GET " + repoFiles + "README.md": repoServe(repoFixture(t, "file_readme.json")),
	})
	got, err := f.GetReadme(t.Context(), repoRef)
	if err != nil {
		t.Fatal(err)
	}
	if want := (domain.Readme{Name: "README.md", Body: readmeBody}); got != want {
		t.Errorf("GetReadme() = %+v, want %+v", got, want)
	}
	for _, c := range api.requests() {
		if strings.HasPrefix(c.Path, repoTree) || strings.HasPrefix(c.Path, repoFiles) {
			if c.Method != http.MethodGet || c.Query.Get("ref") != "main" {
				t.Errorf("%s %s ref = %q, want GET with ref=main (GitLab requires a ref; the default branch is main)", c.Method, c.Path, c.Query.Get("ref"))
			}
		}
	}
}

func TestGetReadmeRanksNames(t *testing.T) {
	blob := func(names ...string) [][2]string {
		var es [][2]string
		for _, n := range names {
			es = append(es, [2]string{n, "blob"})
		}
		return es
	}
	tests := []struct {
		name    string
		entries [][2]string
		want    string
	}{
		{"md beats markdown, bare and other", blob("README.rst", "README", "README.markdown", "README.md"), "README.md"},
		{"markdown beats bare and other", blob("README.rst", "README", "README.markdown"), "README.markdown"},
		{"bare beats other extensions", blob("README.rst", "README"), "README"},
		{"other README.* is the last resort", blob("CONTRIBUTING.md", "README.rst"), "README.rst"},
		{"a directory named README.md is skipped", [][2]string{{"README.md", "tree"}, {"README", "blob"}}, "README"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newRepoForge(t, map[string]http.HandlerFunc{
				"GET " + repoTree:            repoServe(repoTreeJSON(tt.entries...)),
				"GET " + repoFiles + tt.want: repoFileJSON(tt.want, "body of "+tt.want),
			})
			got, err := f.GetReadme(t.Context(), repoRef)
			if err != nil {
				t.Fatal(err)
			}
			if want := (domain.Readme{Name: tt.want, Body: "body of " + tt.want}); got != want {
				t.Errorf("GetReadme() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGetReadmeNotFound(t *testing.T) {
	tests := []struct {
		name  string
		route http.HandlerFunc
	}{
		{"no README in the root", repoServe(repoTreeJSON([2]string{"main.go", "blob"}, [2]string{"docs", "tree"}))},
		{"empty root listing", repoServe([]byte("[]"))},
		{"tree answers 404, as for an empty repo", repoStatus(404, "404 Tree Not Found")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newRepoForge(t, map[string]http.HandlerFunc{"GET " + repoTree: tt.route})
			_, err := f.GetReadme(t.Context(), repoRef)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestGetReadmeFindsNameOnLaterTreePage(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write(repoTreeJSON([2]string{"README.md", "blob"}))
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v4%s?page=2&ref=main>; rel="next"`, r.Host, repoTree))
			_, _ = w.Write(repoTreeJSON([2]string{"main.go", "blob"}))
		},
		"GET " + repoFiles + "README.md": repoFileJSON("README.md", readmeBody),
	})
	got, err := f.GetReadme(t.Context(), repoRef)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "README.md" || got.Body != readmeBody {
		t.Errorf("GetReadme() = %+v, want README.md from the second page", got)
	}
}

func TestGetReadmeBadEncodingIsNotNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree:                repoServe(repoTreeJSON([2]string{"README.md", "blob"})),
		"GET " + repoFiles + "README.md": repoServe([]byte(`{"file_name":"README.md","encoding":"text","content":"hello"}`)),
	})
	_, err := f.GetReadme(t.Context(), repoRef)
	if err == nil {
		t.Fatal("GetReadme() succeeded, want an error for an encoding other than base64")
	}
	if errors.Is(err, forge.ErrNotFound) {
		t.Errorf("err = %v; a broken README must not look like a missing one", err)
	}
}
