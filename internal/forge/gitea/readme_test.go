package gitea_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// readmeBody is the decoded text of testdata/readme_file.json.
const readmeBody = "# lazyforge\n\nA keyboard-driven terminal UI for git forges.\n\n- Forgejo and Gitea\n- GitHub\n- GitLab\n\nSee [the design](docs/design.md).\n"

const (
	rootListing = "GET /api/v1/repos/{owner}/{repo}/contents"
	fileAtPath  = "GET /api/v1/repos/{owner}/{repo}/contents/{path...}"
)

// entry is one row of a contents listing.
type entry struct{ name, typ string }

func file(name string) entry { return entry{name, "file"} }
func dir(name string) entry  { return entry{name, "dir"} }

// listingOf serves a root contents listing with the given entries, in order.
func listingOf(entries ...entry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		rows := make([]obj, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, obj{"name": e.name, "path": e.name, "type": e.typ})
		}
		writeJSON(w, 200, rows)
	}
}

// fixtureAt serves the named fixture for the file at path, and reports any other path the adapter asks for.
func fixtureAt(t *testing.T, path, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("path"); got != path {
			t.Errorf("requested contents/%s, want contents/%s", got, path)
			writeRaw(w, 404, readFixture(t, "error_404.json"))
			return
		}
		writeRaw(w, 200, readFixture(t, name))
	}
}

// contentOf serves the file at path with the given base64 content.
func contentOf(t *testing.T, path, content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("path"); got != path {
			t.Errorf("requested contents/%s, want contents/%s", got, path)
		}
		writeJSON(w, 200, obj{"name": path, "path": path, "type": "file", "encoding": "base64", "content": content})
	}
}

func TestGetReadmeDecodesRootReadme(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: func(w http.ResponseWriter, _ *http.Request) {
			writeRaw(w, 200, readFixture(t, "readme_contents.json"))
		},
		fileAtPath: fixtureAt(t, "README.md", "readme_file.json"),
	})
	got, err := f.GetReadme(t.Context(), lazyforge)
	if err != nil {
		t.Fatal(err)
	}
	if want := (domain.Readme{Name: "README.md", Body: readmeBody}); got != want {
		t.Errorf("GetReadme() = %+v, want %+v", got, want)
	}
}

func TestGetReadmeRanksCandidates(t *testing.T) {
	tests := []struct {
		name    string
		listing []entry
		want    string
	}{
		{"md beats other extensions and no extension", []entry{file("README.txt"), file("readme.md"), file("README")}, "readme.md"},
		{"markdown beats no extension", []entry{file("README"), file("README.markdown")}, "README.markdown"},
		{"no extension beats other extensions", []entry{file("readme.rst"), file("README")}, "README"},
		{"other extension is still found", []entry{file("go.mod"), file("readme.rst")}, "readme.rst"},
		{"ties go to the first listed", []entry{file("readme.md"), file("README.md")}, "readme.md"},
		{"a directory named README is skipped", []entry{dir("README"), file("readme.rst")}, "readme.rst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				rootListing: listingOf(tt.listing...),
				fileAtPath:  contentOf(t, tt.want, base64.StdEncoding.EncodeToString([]byte(readmeBody))),
			})
			got, err := f.GetReadme(t.Context(), lazyforge)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.want {
				t.Errorf("GetReadme() chose %q, want %q", got.Name, tt.want)
			}
		})
	}
}

func TestGetReadmeNotFound(t *testing.T) {
	tests := []struct {
		name   string
		routes map[string]http.HandlerFunc
	}{
		{"empty listing", map[string]http.HandlerFunc{rootListing: listingOf()}},
		{"listing 404", map[string]http.HandlerFunc{
			rootListing: func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 404, readFixture(t, "error_404.json")) },
		}},
		{"file 404 after the listing matched", map[string]http.HandlerFunc{
			rootListing: listingOf(file("README.md")),
			fileAtPath: func(w http.ResponseWriter, _ *http.Request) {
				writeRaw(w, 404, readFixture(t, "error_404.json"))
			},
		}},
		{"only a README in docs or .github", map[string]http.HandlerFunc{
			rootListing: listingOf(dir("docs"), dir(".github"), file("LICENSE")),
			fileAtPath: func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("adapter fetched contents/%s; the root listing has no README", r.PathValue("path"))
				writeRaw(w, 404, readFixture(t, "error_404.json"))
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, tt.routes)
			_, err := f.GetReadme(t.Context(), lazyforge)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestGetReadmeFileErrorsAreNotNotFound(t *testing.T) {
	tests := []struct {
		name    string
		file    http.HandlerFunc
		wantMsg string
	}{
		{
			name: "null encoding and empty content (oversized file)",
			file: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, 200, obj{"name": "README.md", "path": "README.md", "type": "file", "encoding": nil, "content": ""})
			},
			wantMsg: "README.md",
		},
		{
			name: "encoding other than base64",
			file: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, 200, obj{"name": "README.md", "path": "README.md", "type": "file", "encoding": "utf-8", "content": "# hi"})
			},
			wantMsg: "README.md",
		},
		{
			name: "content that is not base64",
			file: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, 200, obj{"name": "README.md", "path": "README.md", "type": "file", "encoding": "base64", "content": "!!not base64!!"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				rootListing: listingOf(file("README.md")),
				fileAtPath:  tt.file,
			})
			_, err := f.GetReadme(t.Context(), lazyforge)
			if err == nil {
				t.Fatal("GetReadme() succeeded, want an error")
			}
			if errors.Is(err, forge.ErrNotFound) {
				t.Errorf("err = %v; a broken README must not look like a missing one", err)
			}
			if tt.wantMsg != "" && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err %q does not name the file %q", err, tt.wantMsg)
			}
		})
	}
}
