package gitlab_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// previewCap restates core.MaxPreviewSize, which an adapter test does not import.
const previewCap = 256 << 10

func TestListTreeMapsEntryTypes(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree: repoServe(repoFixture(t, "tree_src.json")),
	})
	got, err := f.ListTree(t.Context(), repoRef, "feature/x", "src")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.TreeEntry{
		"cmd":      {Name: "cmd", Path: "src/cmd", Type: domain.EntryDir},
		"main.go":  {Name: "main.go", Path: "src/main.go", Type: domain.EntryFile},
		"run.sh":   {Name: "run.sh", Path: "src/run.sh", Type: domain.EntryFile},
		"vendored": {Name: "vendored", Path: "src/vendored", Type: domain.EntrySubmodule},
		"pinned":   {Name: "pinned", Path: "src/pinned", Type: domain.EntrySubmodule},
	}
	if len(got) != len(want) {
		t.Fatalf("ListTree() returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for _, g := range got {
		w, ok := want[g.Name]
		if !ok {
			t.Errorf("unexpected entry %+v", g)
			continue
		}
		if g.Path != w.Path || g.Type != w.Type || g.Size != 0 {
			t.Errorf("entry %s = %+v, want path %q type %v and size 0 (the listing has no sizes)", g.Name, g, w.Path, w.Type)
		}
	}
	reqs := api.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].Path != repoTree {
		t.Fatalf("requests = %+v, want one GET %s", reqs, repoTree)
	}
	if q := reqs[0].Query; q.Get("path") != "src" || q.Get("ref") != "feature/x" || q.Get("per_page") != "100" {
		t.Errorf("query = %v, want path=src, ref=feature/x, per_page=100", q)
	}
}

func TestListTreeRootAndDefaultRef(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree: repoServe(repoTreeJSON([2]string{"README.md", "blob"})),
	})
	got, err := f.ListTree(t.Context(), repoRef, "", "")
	if err != nil || len(got) != 1 || got[0].Name != "README.md" || got[0].Path != "README.md" {
		t.Fatalf("ListTree() = %+v, %v; want README.md", got, err)
	}
	for _, c := range api.requests() {
		if c.Path != repoTree {
			continue
		}
		if p := c.Query.Get("path"); p != "" {
			t.Errorf("path = %q, want none for the root", p)
		}
		if r := c.Query.Get("ref"); r != "" && r != "main" {
			t.Errorf("ref = %q, want the default branch (omitted or main)", r)
		}
	}
}

func TestListTreeFollowsPages(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write(repoTreeJSON([2]string{"b.go", "blob"}))
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v4%s?page=2&ref=main>; rel="next"`, r.Host, repoTree))
			_, _ = w.Write(repoTreeJSON([2]string{"a.go", "blob"}))
		},
	})
	got, err := f.ListTree(t.Context(), repoRef, "main", "")
	if err != nil || len(got) != 2 {
		t.Fatalf("ListTree() = %+v, %v; want both pages", got, err)
	}
}

func TestListTreeMissingPathOr404IsNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree: repoStatus(404, "404 Tree Not Found"),
	})
	_, err := f.ListTree(t.Context(), repoRef, "main", "nope")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListTreeEmptyListingFrom200IsNotAnError(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{"GET " + repoTree: repoServe([]byte("[]"))})
	got, err := f.ListTree(t.Context(), repoRef, "main", "nope")
	if err != nil || len(got) != 0 {
		t.Errorf("ListTree() = %+v, %v; want an empty listing and nil (older servers answer 200 [] for a missing path)", got, err)
	}
}

// fileRoutes serves HEAD and raw GET for path (already escaped) with the given size header and body.
func fileRoutes(path string, size int, body []byte) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"HEAD " + repoFiles + path: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Gitlab-Size", strconv.Itoa(size))
			w.WriteHeader(http.StatusOK)
		},
		"GET " + repoFiles + path + "/raw": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(body)
		},
	}
}

func TestReadFileReturnsExactBytesFromRaw(t *testing.T) {
	body := []byte("text\x00binary \xff\xfe tail\r\n")
	f, api := newRepoForge(t, fileRoutes("docs%2Fa%20b.bin", len(body), body))
	got, err := f.ReadFile(t.Context(), repoRef, "feature/x", "docs/a b.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("ReadFile() = %q, want %q", got, body)
	}
	var sawGet bool
	for _, c := range api.requests() {
		if c.Path == repoFiles+"docs%2Fa%20b.bin/raw" {
			sawGet = true
			if c.Method != http.MethodGet || c.Query.Get("ref") != "feature/x" {
				t.Errorf("raw request = %s ref=%q, want GET with ref=feature/x", c.Method, c.Query.Get("ref"))
			}
		}
	}
	if !sawGet {
		t.Errorf("requests = %+v, want a GET on the fully encoded path (slash as %%2F) with /raw", api.requests())
	}
}

func TestReadFileChecksSizeBeforeDownloading(t *testing.T) {
	f, api := newRepoForge(t, fileRoutes("big.bin", previewCap+1, []byte("must not be read")))
	_, err := f.ReadFile(t.Context(), repoRef, "main", "big.bin")
	if err == nil {
		t.Fatal("ReadFile() succeeded for a file over the preview cap, want an error")
	}
	if n := api.count(http.MethodHead, repoFiles+"big.bin"); n != 1 {
		t.Errorf("HEAD made %d times, want 1", n)
	}
	if n := api.count(http.MethodGet, repoFiles+"big.bin/raw"); n != 0 {
		t.Errorf("raw GET made %d times, want none: an oversized file is refused before any download", n)
	}
}

func TestReadFileAtTheCapIsRead(t *testing.T) {
	body := bytes.Repeat([]byte("a"), previewCap)
	f, _ := newRepoForge(t, fileRoutes("edge.txt", previewCap, body))
	got, err := f.ReadFile(t.Context(), repoRef, "main", "edge.txt")
	if err != nil || len(got) != previewCap {
		t.Fatalf("ReadFile() = %d bytes, %v; want %d bytes: only a file over the cap is refused", len(got), err, previewCap)
	}
}

func TestReadFileDefaultRefIsTheDefaultBranch(t *testing.T) {
	body := []byte("hi")
	f, api := newRepoForge(t, fileRoutes("a.txt", len(body), body))
	if _, err := f.ReadFile(t.Context(), repoRef, "", "a.txt"); err != nil {
		t.Fatal(err)
	}
	var sawRaw bool
	for _, c := range api.requests() {
		if c.Path == repoFiles+"a.txt" || c.Path == repoFiles+"a.txt/raw" {
			sawRaw = sawRaw || c.Path == repoFiles+"a.txt/raw"
			if got := c.Query.Get("ref"); got != "main" {
				t.Errorf("%s %s ref = %q, want main: GitLab requires a ref", c.Method, c.Path, got)
			}
		}
	}
	if !sawRaw {
		t.Errorf("requests = %+v, want a raw GET", api.requests())
	}
}

func TestReadFileMissingIsNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"HEAD " + repoFiles + "gone.txt":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"GET " + repoFiles + "gone.txt/raw": repoStatus(404, "404 File Not Found"),
	})
	_, err := f.ReadFile(t.Context(), repoRef, "main", "gone.txt")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
