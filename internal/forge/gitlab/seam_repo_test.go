package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestSeamReadmeRank(t *testing.T) {
	for _, c := range []struct {
		name string
		want int
	}{{"README.md", 0}, {"readme.MD", 0}, {"README.markdown", 1}, {"README", 2}, {"README.rst", 3}, {"READMEfoo", -1}, {"main.go", -1}} {
		if got := readmeRank(c.name); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSeamEntryType(t *testing.T) {
	for _, c := range []struct {
		it   treeItem
		want domain.EntryType
	}{
		{treeItem{Type: "tree", Mode: "040000"}, domain.EntryDir},
		{treeItem{Type: "blob", Mode: "100644"}, domain.EntryFile},
		{treeItem{Type: "commit", Mode: "160000"}, domain.EntrySubmodule},
		{treeItem{Type: "blob", Mode: "160000"}, domain.EntrySubmodule},
		{treeItem{Type: "blob", Mode: "120000"}, domain.EntrySymlink},
	} {
		if got := entryType(c.it); got != c.want {
			t.Errorf("%+v: got %v, want %v", c.it, got, c.want)
		}
	}
}

func TestSeamUpdateBranch(t *testing.T) {
	var method, path string
	status := http.StatusAccepted
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"Cannot push to source branch"}`))
	}))
	defer srv.Close()
	f, err := newForge(srv.URL, "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := domain.RepoRef{Owner: "g/s", Name: "p"}
	ctx := context.Background()
	if err := f.UpdateBranch(ctx, r, 7, forge.UpdateRebase); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if method != http.MethodPut || path != "/api/v4/projects/g%2Fs%2Fp/merge_requests/7/rebase" {
		t.Errorf("got %s %s", method, path)
	}
	if err := f.UpdateBranch(ctx, r, 7, forge.UpdateMerge); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("merge: got %v, want ErrUnsupported", err)
	}
	for _, code := range []int{http.StatusForbidden, http.StatusConflict} {
		status = code
		if err := f.UpdateBranch(ctx, r, 7, forge.UpdateRebase); !errors.Is(err, forge.ErrRefused) {
			t.Errorf("%d: got %v, want ErrRefused", code, err)
		}
	}
}

func TestSeamReadFileOversize(t *testing.T) {
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("X-Gitlab-Size", "300000")
			return
		}
		gets++
	}))
	defer srv.Close()
	f, err := newForge(srv.URL, "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.ReadFile(context.Background(), domain.RepoRef{Owner: "g", Name: "p"}, "main", "big.bin")
	if !errors.Is(err, forge.ErrTooLarge) || gets != 0 {
		t.Errorf("gets %d, err %v: want ErrTooLarge and no download", gets, err)
	}
}
