package github_test

import (
	"errors"
	"net/http"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// readmeBody is the decoded text of testdata/readme.json.
const readmeBody = "# lazyforge\n\nA keyboard-driven terminal UI for git forges.\n\n- Forgejo and Gitea\n- GitHub\n- GitLab\n\nSee [the design](docs/design.md).\n"

const readmeRoute = "GET /api/v3/repos/{owner}/{repo}/readme"

func TestGetReadmeDecodesFixture(t *testing.T) {
	repo := domain.RepoRef{Owner: "deadstyle", Name: "lazyforge"}
	f := stub(t, readmeRoute, func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "readme.json"))
	})
	got, err := f.GetReadme(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := (domain.Readme{Name: "README.md", Body: readmeBody}); got != want {
		t.Errorf("GetReadme() = %+v, want %+v", got, want)
	}
}

func TestGetReadmeNotFoundOn404(t *testing.T) {
	repo := domain.RepoRef{Owner: "deadstyle", Name: "empty"}
	f := stub(t, readmeRoute, func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 404, readFixture(t, "error_404.json"))
	})
	_, err := f.GetReadme(t.Context(), repo)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetReadmeNonBase64IsNotNotFound(t *testing.T) {
	repo := domain.RepoRef{Owner: "deadstyle", Name: "lazyforge"}
	f := stub(t, readmeRoute, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, obj{"name": "README.md", "path": "README.md", "type": "file", "encoding": "none", "content": ""})
	})
	_, err := f.GetReadme(t.Context(), repo)
	if err == nil {
		t.Fatal("GetReadme() succeeded, want an error for encoding \"none\"")
	}
	if errors.Is(err, forge.ErrNotFound) {
		t.Errorf("err = %v; a broken README must not look like a missing one", err)
	}
}
