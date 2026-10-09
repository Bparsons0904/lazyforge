package gitea_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// refCall is one request a refLog answered: its URI, and the ref query parameter as the server reads it.
type refCall struct {
	uri    string
	ref    string
	hasRef bool
}

// refLog records the requests it answers. Handlers run on server goroutines, so it locks.
type refLog struct {
	mu    sync.Mutex
	calls []refCall
}

// answer records each request and answers it with status and body.
func (l *refLog) answer(status int, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		_, has := q["ref"]
		l.mu.Lock()
		l.calls = append(l.calls, refCall{uri: r.URL.RequestURI(), ref: q.Get("ref"), hasRef: has})
		l.mu.Unlock()
		writeRaw(w, status, body)
	}
}

func (l *refLog) all() []refCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]refCall(nil), l.calls...)
}

// answerByRef answers 404 with notFound for the ref missing, and 200 with ok for any other ref.
func answerByRef(missing string, ok, notFound []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") == missing {
			writeRaw(w, 404, notFound)
			return
		}
		writeRaw(w, 200, ok)
	}
}

func TestListTreeSendsRefOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name, ref, dir, uri string
	}{
		{"branch root", "feature/x", "", "/api/v1/repos/deadstyle/lazyforge/contents?ref=feature%2Fx"},
		{"branch directory", "feature/x", "docs", "/api/v1/repos/deadstyle/lazyforge/contents/docs?ref=feature%2Fx"},
		{"default root", "", "", "/api/v1/repos/deadstyle/lazyforge/contents"},
		{"default directory", "", "docs", "/api/v1/repos/deadstyle/lazyforge/contents/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &refLog{}
			body := readFixture(t, "contents_dir.json")
			f := stubForge(t, map[string]http.HandlerFunc{
				rootListing: log.answer(200, body),
				fileAtPath:  log.answer(200, body),
			})
			if _, err := f.ListTree(context.Background(), lazyforge, tt.ref, tt.dir); err != nil {
				t.Fatalf("ListTree(%q, %q): %v", tt.ref, tt.dir, err)
			}
			got := log.all()
			if len(got) != 1 || got[0].uri != tt.uri {
				t.Fatalf("requests = %+v, want one at %q", got, tt.uri)
			}
			if got[0].ref != tt.ref || got[0].hasRef != (tt.ref != "") {
				t.Errorf("ref query = %q (present %v), want %q (present %v)", got[0].ref, got[0].hasRef, tt.ref, tt.ref != "")
			}
		})
	}
}

func TestReadFileSendsRefOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name, ref, uri string
	}{
		{"branch name with reserved runes", "a&b#c/d", "/api/v1/repos/deadstyle/lazyforge/contents/x.txt?ref=a%26b%23c%2Fd"},
		{"default branch", "", "/api/v1/repos/deadstyle/lazyforge/contents/x.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &refLog{}
			f := stubForge(t, map[string]http.HandlerFunc{
				fileAtPath: log.answer(200, readFixture(t, "contents_file.json")),
			})
			if _, err := f.ReadFile(context.Background(), lazyforge, tt.ref, "x.txt"); err != nil {
				t.Fatalf("ReadFile(%q): %v", tt.ref, err)
			}
			got := log.all()
			if len(got) != 1 || got[0].uri != tt.uri {
				t.Fatalf("requests = %+v, want one at %q", got, tt.uri)
			}
			if got[0].ref != tt.ref || got[0].hasRef != (tt.ref != "") {
				t.Errorf("ref query = %q (present %v), want %q (present %v)", got[0].ref, got[0].hasRef, tt.ref, tt.ref != "")
			}
		})
	}
}

func TestUnknownRefIsNotFound(t *testing.T) {
	ctx := context.Background()
	notFound := readFixture(t, "error_404.json")

	listing := stubForge(t, map[string]http.HandlerFunc{
		rootListing: answerByRef("nope", readFixture(t, "contents_dir.json"), notFound),
	})
	if _, err := listing.ListTree(ctx, lazyforge, "feature/x", ""); err != nil {
		t.Fatalf("ListTree on a known ref: %v", err)
	}
	if _, err := listing.ListTree(ctx, lazyforge, "nope", ""); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListTree on an unknown ref: err = %v, want ErrNotFound", err)
	}

	file := stubForge(t, map[string]http.HandlerFunc{
		fileAtPath: answerByRef("nope", readFixture(t, "contents_file.json"), notFound),
	})
	if _, err := file.ReadFile(ctx, lazyforge, "feature/x", "README.md"); err != nil {
		t.Fatalf("ReadFile on a known ref: %v", err)
	}
	if _, err := file.ReadFile(ctx, lazyforge, "nope", "README.md"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ReadFile on an unknown ref: err = %v, want ErrNotFound", err)
	}
}

func TestListTreeConflictOnRefIsNotFound(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: contentsAnswer(&contentsLog{}, 409, readFixture(t, "error_404.json")),
	})
	_, err := f.ListTree(context.Background(), lazyforge, "feature/x", "")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListTreeAtRefKeepsHTMLURL(t *testing.T) {
	body := readFixture(t, "contents_dir.json")
	var listing []struct {
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	f := stubForge(t, map[string]http.HandlerFunc{
		rootListing: contentsAnswer(&contentsLog{}, 200, body),
	})
	got, err := f.ListTree(context.Background(), lazyforge, "feature/x", "")
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	if len(got) == 0 || len(got) != len(listing) {
		t.Fatalf("got %d entries, want %d", len(got), len(listing))
	}
	webURL := map[string]string{}
	for _, e := range got {
		webURL[e.Name] = e.WebURL
	}
	for _, want := range listing {
		if webURL[want.Name] != want.HTMLURL {
			t.Errorf("%s: WebURL = %q, want the listing's html_url %q", want.Name, webURL[want.Name], want.HTMLURL)
		}
	}
}
