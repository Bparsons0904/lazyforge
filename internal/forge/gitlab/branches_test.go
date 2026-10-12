package gitlab_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const (
	repoBranches = repoProject + "/repository/branches"
	repoCommits  = repoProject + "/repository/commits"
)

func TestListBranchesDecodesFixtureWithoutExtraCalls(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoBranches: repoServe(repoFixture(t, "branches.json")),
	})
	got, err := f.ListBranches(t.Context(), repoRef)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Branch{
		{
			Name: "feature/login",
			Commit: domain.Commit{
				SHA: "2f63b7a1c4d5e6f708192a3b4c5d6e7f80910a2b", Message: "Add the login form", Author: "Bob Example",
				Date: time.Date(2026, 9, 30, 8, 15, 0, 0, time.UTC),
			},
		},
		{
			Name: "main", Default: true,
			Commit: domain.Commit{
				SHA: "7b5c3a1e9d8f6a4b2c0e1f3a5b7c9d1e3f5a7b9c", Message: "Merge branch 'fix-typo' into 'main'", Author: "Alice Example",
				Date: time.Date(2026, 10, 1, 10, 5, 0, 0, time.UTC),
			},
		},
	}
	byName := map[string]domain.Branch{}
	for _, b := range got {
		byName[b.Name] = b
	}
	if len(got) != len(want) {
		t.Fatalf("ListBranches() returned %d branches, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range want {
		g := byName[w.Name]
		if g.Default != w.Default || g.Commit.SHA != w.Commit.SHA || g.Commit.Message != w.Commit.Message || g.Commit.Author != w.Commit.Author || !g.Commit.Date.Equal(w.Commit.Date) {
			t.Errorf("branch %s = %+v, want %+v", w.Name, g, w)
		}
	}
	if reqs := api.requests(); len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].Path != repoBranches {
		t.Errorf("requests = %+v, want exactly one GET %s (the tip commit is inline)", reqs, repoBranches)
	}
}

func TestListBranchesFollowsPages(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoBranches: func(w http.ResponseWriter, r *http.Request) {
			name, next := "first", true
			if r.URL.Query().Get("page") == "2" {
				name, next = "second", false
			}
			if next {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v4%s?page=2>; rel="next"`, r.Host, repoBranches))
			}
			_, _ = fmt.Fprintf(w, `[{"name":%q,"default":false,"commit":{"id":"abc","title":"t","author_name":"a","committed_date":"2026-10-01T10:05:00.000+00:00"}}]`, name)
		},
	})
	got, err := f.ListBranches(t.Context(), repoRef)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range got {
		names = append(names, b.Name)
	}
	slices.Sort(names)
	if want := []string{"first", "second"}; !slices.Equal(names, want) {
		t.Errorf("branches = %v, want %v", names, want)
	}
}

func TestListBranchesEmptyRepoIsNotAnError(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{"GET " + repoBranches: repoServe([]byte("[]"))})
	got, err := f.ListBranches(t.Context(), repoRef)
	if err != nil || len(got) != 0 {
		t.Errorf("ListBranches() = %+v, %v; want none and nil", got, err)
	}
}

func TestListCommitsDecodesFixtureNewestFirst(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoCommits: repoServe(repoFixture(t, "commits.json")),
	})
	got, err := f.ListCommits(t.Context(), repoRef, "release/1.x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCommits() returned %d commits, want 2: %+v", len(got), got)
	}
	first, second := got[0], got[1]
	if first.SHA != "7b5c3a1e9d8f6a4b2c0e1f3a5b7c9d1e3f5a7b9c" || first.Message != "Merge branch 'fix-typo' into 'main'" ||
		first.Author != "Alice Example" || !first.Date.Equal(time.Date(2026, 10, 1, 10, 5, 0, 0, time.UTC)) {
		t.Errorf("first commit = %+v", first)
	}
	if second.SHA != "3c1a5b7d9e0f2a4c6e8b0d2f4a6c8e0b2d4f6a8c" || second.Message != "Fix the typo in the intro" || second.Author != "Bob Example" {
		t.Errorf("second commit = %+v; Message is the subject line, without the trailing newline", second)
	}
	reqs := api.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].Path != repoCommits {
		t.Fatalf("requests = %+v, want exactly one GET %s", reqs, repoCommits)
	}
	if q := reqs[0].Query; q.Get("ref_name") != "release/1.x" || q.Get("per_page") != "30" {
		t.Errorf("query = %v, want ref_name=release/1.x and per_page=30", q)
	}
}

func TestListCommitsReadsFirstPageOnly(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoCommits: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v4%s?page=2>; rel="next"`, r.Host, repoCommits))
			_, _ = w.Write(repoFixture(t, "commits.json"))
		},
	})
	if _, err := f.ListCommits(t.Context(), repoRef, "main"); err != nil {
		t.Fatal(err)
	}
	if n := api.count(http.MethodGet, repoCommits); n != 1 {
		t.Errorf("GET %s made %d times, want 1: the Commits box shows at most 30", repoCommits, n)
	}
}

func TestListCommitsUnknownBranchIsNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoCommits: repoStatus(404, "404 Tree Not Found"),
	})
	_, err := f.ListCommits(t.Context(), repoRef, "gone")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
