package gitea_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

var _ forge.TreeReader = (*gitea.Forge)(nil)

// filesReadmeBody is what contents_file.json decodes to.
const filesReadmeBody = "# Notes\n\nThe quick brown fox jumps over the lazy dog, again and again until this body wraps across lines.\n"

// contentsLog records the request URI of each call a stub answers. Handlers run on server goroutines, so it locks.
type contentsLog struct {
	mu   sync.Mutex
	uris []string
}

func (l *contentsLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uris = append(l.uris, r.URL.RequestURI())
}

func (l *contentsLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.uris...)
}

// contentsAnswer logs the request and answers it with status and body.
func contentsAnswer(log *contentsLog, status int, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.add(r)
		writeRaw(w, status, body)
	}
}

func TestListTreeRootRequestsContents(t *testing.T) {
	log := &contentsLog{}
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: contentsAnswer(log, 200, readFixture(t, "contents_dir.json")),
	})
	if _, err := f.ListTree(context.Background(), lazyforge, ""); err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	want := "/api/v1/repos/deadstyle/lazyforge/contents"
	if got := log.all(); len(got) != 1 || got[0] != want {
		t.Errorf("requests = %q, want [%q]", got, want)
	}
}

func TestListTreeMapsEveryEntry(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: contentsAnswer(&contentsLog{}, 200, readFixture(t, "contents_dir.json")),
	})
	got, err := f.ListTree(context.Background(), lazyforge, "")
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	byPath := map[string]domain.TreeEntry{}
	for _, e := range got {
		byPath[e.Path] = e
	}
	base := "https://git.bobparsons.dev/deadstyle/lazyforge/src/branch/main/"
	want := []domain.TreeEntry{
		{Name: "src", Path: "src", Type: domain.EntryDir, WebURL: base + "src"},
		{Name: "README.md", Path: "README.md", Type: domain.EntryFile, Size: 106, WebURL: base + "README.md"},
		{Name: "latest", Path: "latest", Type: domain.EntrySymlink, Size: 3, WebURL: base + "latest"},
		{
			Name: "vendor", Path: "vendor", Type: domain.EntrySubmodule,
			WebURL: "https://git.bobparsons.dev/deadstyle/lazyforge/src/commit/1f7a7a472abf77e74b3d48b8b6f8f2c0f1b24e7a/vendor",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		if g, ok := byPath[w.Path]; !ok || g != w {
			t.Errorf("entry %q = %+v (present %v), want %+v", w.Path, g, ok, w)
		}
	}
}

func TestListTreeEscapesDirectory(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		uri  string
	}{
		{"space", "src/my dir", "/api/v1/repos/deadstyle/lazyforge/contents/src/my%20dir"},
		{"hash and question mark", "docs/a#b?c", "/api/v1/repos/deadstyle/lazyforge/contents/docs/a%23b%3Fc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &contentsLog{}
			f := stubForge(t, map[string]http.HandlerFunc{
				fileAtPath: contentsAnswer(log, 200, readFixture(t, "contents_dir.json")),
			})
			if _, err := f.ListTree(context.Background(), lazyforge, tt.dir); err != nil {
				t.Fatalf("ListTree(%q): %v", tt.dir, err)
			}
			if got := log.all(); len(got) != 1 || got[0] != tt.uri {
				t.Errorf("requests = %q, want [%q]", got, tt.uri)
			}
		})
	}
}

func TestListTreeEmptyListingIsNotAnError(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: contentsAnswer(&contentsLog{}, 200, []byte("[]")),
	})
	got, err := f.ListTree(context.Background(), lazyforge, "")
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListTree = %+v, want no entries", got)
	}
}

func TestListTreeMissingIsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"404", 404},
		{"409 for a repo with no commits", 409},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				rootListing: contentsAnswer(&contentsLog{}, tt.status, readFixture(t, "error_404.json")),
			})
			_, err := f.ListTree(context.Background(), lazyforge, "")
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestReadFileDecodesWrappedBase64(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		fileAtPath: contentsAnswer(&contentsLog{}, 200, readFixture(t, "contents_file.json")),
	})
	got, err := f.ReadFile(context.Background(), lazyforge, "README.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != filesReadmeBody {
		t.Errorf("ReadFile = %q, want %q", got, filesReadmeBody)
	}
}

func TestReadFileEscapesPath(t *testing.T) {
	log := &contentsLog{}
	f := stubForge(t, map[string]http.HandlerFunc{
		fileAtPath: contentsAnswer(log, 200, readFixture(t, "contents_file.json")),
	})
	if _, err := f.ReadFile(context.Background(), lazyforge, "src/my dir/notes.txt"); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "/api/v1/repos/deadstyle/lazyforge/contents/src/my%20dir/notes.txt"
	if got := log.all(); len(got) != 1 || got[0] != want {
		t.Errorf("requests = %q, want [%q]", got, want)
	}
}

func TestReadFileEmptyContentIsEmptyNotError(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		fileAtPath: contentsAnswer(&contentsLog{}, 200, readFixture(t, "contents_file_empty.json")),
	})
	got, err := f.ReadFile(context.Background(), lazyforge, "empty.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadFile = %q, want empty", got)
	}
}

func TestReadFileRejectsOtherEncodings(t *testing.T) {
	for _, enc := range []string{"", "none", "utf-8"} {
		t.Run(fmt.Sprintf("encoding %q", enc), func(t *testing.T) {
			body := obj{"name": "a.txt", "path": "a.txt", "type": "file", "encoding": enc, "content": "aGkK"}
			f := stubForge(t, map[string]http.HandlerFunc{
				fileAtPath: func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, body) },
			})
			if _, err := f.ReadFile(context.Background(), lazyforge, "a.txt"); err == nil {
				t.Errorf("ReadFile with encoding %q: err = nil, want an error", enc)
			}
		})
	}
}

func TestReadFileMissingIsNotFound(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		fileAtPath: contentsAnswer(&contentsLog{}, 404, readFixture(t, "error_404.json")),
	})
	_, err := f.ReadFile(context.Background(), lazyforge, "gone.txt")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFilesAvailableAtReadAccess(t *testing.T) {
	f := stubForge(t, nil)
	repo := domain.Repo{RepoRef: lazyforge, Access: domain.AccessRead}
	if got := forge.Can(f, forge.ActFiles, repo); !got.OK {
		t.Errorf("Can(ActFiles) at read access = %+v, want OK", got)
	}
}
