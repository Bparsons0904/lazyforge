package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// refOf reads back the ref query parameter the way the server does, and reports whether it was sent.
func refOf(t *testing.T, uri string) (string, bool) {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse request URI %q: %v", uri, err)
	}
	q := u.Query()
	_, has := q["ref"]
	return q.Get("ref"), has
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
		{"branch root", "feature/x", "", "/api/v3/repos/cli/cli/contents?ref=feature%2Fx"},
		{"branch directory", "feature/x", "docs", "/api/v3/repos/cli/cli/contents/docs?ref=feature%2Fx"},
		{"default root", "", "", "/api/v3/repos/cli/cli/contents"},
		{"default directory", "", "docs", "/api/v3/repos/cli/cli/contents/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &contentsLog{}
			body := readFixture(t, "contents_dir.json")
			f := stubRoutes(t, map[string]http.HandlerFunc{
				contentsRoot: contentsAnswer(log, 200, body),
				contentsPath: contentsAnswer(log, 200, body),
			})
			if _, err := f.ListTree(context.Background(), cli, tt.ref, tt.dir); err != nil {
				t.Fatalf("ListTree(%q, %q): %v", tt.ref, tt.dir, err)
			}
			got := log.all()
			if len(got) != 1 || got[0] != tt.uri {
				t.Fatalf("requests = %q, want [%q]", got, tt.uri)
			}
			ref, has := refOf(t, got[0])
			if ref != tt.ref || has != (tt.ref != "") {
				t.Errorf("ref query = %q (present %v), want %q (present %v)", ref, has, tt.ref, tt.ref != "")
			}
		})
	}
}

func TestReadFileSendsRefOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name, ref, uri string
	}{
		{"branch name with reserved runes", "a&b#c/d", "/api/v3/repos/cli/cli/contents/x.txt?ref=a%26b%23c%2Fd"},
		{"default branch", "", "/api/v3/repos/cli/cli/contents/x.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &contentsLog{}
			f := stubRoutes(t, map[string]http.HandlerFunc{
				contentsPath: contentsAnswer(log, 200, readFixture(t, "contents_file.json")),
			})
			if _, err := f.ReadFile(context.Background(), cli, tt.ref, "x.txt"); err != nil {
				t.Fatalf("ReadFile(%q): %v", tt.ref, err)
			}
			got := log.all()
			if len(got) != 1 || got[0] != tt.uri {
				t.Fatalf("requests = %q, want [%q]", got, tt.uri)
			}
			ref, has := refOf(t, got[0])
			if ref != tt.ref || has != (tt.ref != "") {
				t.Errorf("ref query = %q (present %v), want %q (present %v)", ref, has, tt.ref, tt.ref != "")
			}
		})
	}
}

func TestUnknownRefIsNotFound(t *testing.T) {
	ctx := context.Background()
	notFound := readFixture(t, "error_404.json")

	listing := stubRoutes(t, map[string]http.HandlerFunc{
		contentsRoot: answerByRef("nope", readFixture(t, "contents_dir.json"), notFound),
	})
	if _, err := listing.ListTree(ctx, cli, "feature/x", ""); err != nil {
		t.Fatalf("ListTree on a known ref: %v", err)
	}
	if _, err := listing.ListTree(ctx, cli, "nope", ""); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListTree on an unknown ref: err = %v, want ErrNotFound", err)
	}

	file := stubRoutes(t, map[string]http.HandlerFunc{
		contentsPath: answerByRef("nope", readFixture(t, "contents_file.json"), notFound),
	})
	if _, err := file.ReadFile(ctx, cli, "feature/x", "README.md"); err != nil {
		t.Fatalf("ReadFile on a known ref: %v", err)
	}
	if _, err := file.ReadFile(ctx, cli, "nope", "README.md"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ReadFile on an unknown ref: err = %v, want ErrNotFound", err)
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
	f := stubRoutes(t, map[string]http.HandlerFunc{
		contentsRoot: contentsAnswer(&contentsLog{}, 200, body),
	})
	got, err := f.ListTree(context.Background(), cli, "feature/x", "")
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
