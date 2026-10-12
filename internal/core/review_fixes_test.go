package core_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var trunkRepo = domain.RepoRef{Owner: "g", Name: "p"}

// trunkService returns a Service whose repo list is loaded, so the default branch of trunkRepo is cached.
func trunkService(t *testing.T, defaultBranch string) (*core.Service, *forgetest.Fake) {
	t.Helper()
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindGitLab, User: "bob", ChangeRequestTerm: "MR"})
	f.AddRepo(domain.Repo{RepoRef: trunkRepo, DefaultBranch: defaultBranch})
	s := core.New(f, core.Options{Now: stepClock()})
	if _, err := s.Repos(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, f
}

func TestTreeAskedForTheDefaultBranchReadsTheCachedDefaultBranch(t *testing.T) {
	s, f := trunkService(t, "trunk")
	f.SetTreeAt(trunkRepo, "trunk", "", []domain.TreeEntry{treeFile("main.go", 1)})

	got, err := s.Tree(t.Context(), trunkRepo, "", "")
	if err != nil || !slices.Equal(treeNames(got), []string{"main.go"}) {
		t.Fatalf("Tree(default) = %v, %v; want main.go read at ref trunk", treeNames(got), err)
	}
}

func TestPreviewAskedForTheDefaultBranchReadsTheCachedDefaultBranch(t *testing.T) {
	s, f := trunkService(t, "trunk")
	f.SetFileAt(trunkRepo, "trunk", "notes.md", []byte("trunk body\n"))

	got, err := s.Preview(t.Context(), trunkRepo, "", treeFile("notes.md", int64(len("trunk body\n"))))
	if err != nil || got != (domain.FilePreview{Text: "trunk body\n"}) {
		t.Fatalf("Preview(default) = %+v, %v; want the body read at ref trunk", got, err)
	}
}

func TestDefaultBranchSwapKeepsTheCallersEmptyRefAsTheCacheKey(t *testing.T) {
	s, f := trunkService(t, "trunk")
	f.SetTreeAt(trunkRepo, "trunk", "", []domain.TreeEntry{treeFile("main.go", 1)})
	f.SetFileAt(trunkRepo, "trunk", "notes.md", []byte("x\n"))
	e := treeFile("notes.md", 2)
	if _, err := s.Tree(t.Context(), trunkRepo, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Preview(t.Context(), trunkRepo, "", e); err != nil {
		t.Fatal(err)
	}

	tree, _, ok := s.PeekTree(trunkRepo, "", "")
	if !ok || !slices.Equal(treeNames(tree), []string{"main.go"}) {
		t.Errorf("PeekTree(\"\") = %v, ok=%v; want the listing cached under the empty ref", treeNames(tree), ok)
	}
	if _, _, ok := s.PeekTree(trunkRepo, "trunk", ""); ok {
		t.Error("the listing was cached under ref trunk; the key must keep the caller's empty ref")
	}
	if _, _, ok := s.PeekPreview(trunkRepo, "", "notes.md"); !ok {
		t.Error("PeekPreview(\"\") missed; the key must keep the caller's empty ref")
	}
}

func TestExplicitRefIsNotReplacedByTheDefaultBranch(t *testing.T) {
	s, f := trunkService(t, "trunk")
	f.SetTreeAt(trunkRepo, "trunk", "", []domain.TreeEntry{treeFile("main.go", 1)})
	f.SetTreeAt(trunkRepo, "feature", "", []domain.TreeEntry{treeFile("feature.go", 1)})

	got, err := s.Tree(t.Context(), trunkRepo, "feature", "")
	if err != nil || !slices.Equal(treeNames(got), []string{"feature.go"}) {
		t.Fatalf("Tree(feature) = %v, %v; want feature.go", treeNames(got), err)
	}
}

func TestEmptyRefStaysEmptyWithoutACachedDefaultBranch(t *testing.T) {
	t.Run("no repo list loaded", func(t *testing.T) {
		f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindGitLab})
		f.SetTreeAt(trunkRepo, "", "", []domain.TreeEntry{treeFile("main.go", 1)})
		f.SetFileAt(trunkRepo, "", "notes.md", []byte("x\n"))
		s := core.New(f, core.Options{Now: stepClock()})
		assertEmptyRefReads(t, s)
	})
	t.Run("cached repo has no default branch", func(t *testing.T) {
		s, f := trunkService(t, "")
		f.SetTreeAt(trunkRepo, "", "", []domain.TreeEntry{treeFile("main.go", 1)})
		f.SetFileAt(trunkRepo, "", "notes.md", []byte("x\n"))
		assertEmptyRefReads(t, s)
	})
}

func assertEmptyRefReads(t *testing.T, s *core.Service) {
	t.Helper()
	got, err := s.Tree(t.Context(), trunkRepo, "", "")
	if err != nil || !slices.Equal(treeNames(got), []string{"main.go"}) {
		t.Errorf("Tree(default) = %v, %v; want main.go read at ref \"\"", treeNames(got), err)
	}
	pv, err := s.Preview(t.Context(), trunkRepo, "", treeFile("notes.md", 2))
	if err != nil || pv != (domain.FilePreview{Text: "x\n"}) {
		t.Errorf("Preview(default) = %+v, %v; want the body read at ref \"\"", pv, err)
	}
}

func TestImageLinkOnASubgroupRepoKeepsTheOwnerSlashes(t *testing.T) {
	tests := []struct {
		name string
		repo domain.RepoRef
		want string
	}{
		{"subgroup owner", domain.RepoRef{Owner: "grp/sub", Name: "app"}, "https://gl.example/grp/sub/app/img/a.png"},
		{"plain owner", domain.RepoRef{Owner: "deadstyle", Name: "lazyforge"}, "https://gl.example/deadstyle/lazyforge/img/a.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{
				Fake:  forgetest.NewFake(forge.HostInfo{Kind: forge.KindGitLab, URL: "https://gl.example"}),
				serve: serveBytes(forgetest.DemoPNG),
			}
			s := core.New(rec, core.Options{})
			if _, err := s.Image(t.Context(), tt.repo, "img/a.png"); err != nil {
				t.Fatal(err)
			}
			calls := rec.callList()
			if len(calls) != 1 || calls[0].url != tt.want {
				t.Fatalf("OpenAsset calls %+v, want one for %s", calls, tt.want)
			}
			if strings.Contains(calls[0].url, "%2F") {
				t.Errorf("URL %s carries an escaped slash", calls[0].url)
			}
		})
	}
}
