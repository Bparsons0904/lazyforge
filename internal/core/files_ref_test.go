package core_test

import (
	"context"
	"slices"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestTreeCacheIsKeyedByRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTreeAt(repoA, "", "src", []domain.TreeEntry{treeFile("main.go", 1)})
	f.SetTreeAt(repoA, "b", "src", []domain.TreeEntry{treeFile("feature.go", 1)})
	s := core.New(f, core.Options{Now: stepClock()})

	got, err := s.Tree(ctx, repoA, "b", "src")
	if err != nil || !slices.Equal(treeNames(got), []string{"feature.go"}) {
		t.Fatalf("Tree(b, src) = %v, %v; want feature.go and nil", treeNames(got), err)
	}
	if _, _, ok := s.PeekTree(repoA, "", "src"); ok {
		t.Error("fetching src on b left a cache entry for the default branch")
	}
	peek, when, ok := s.PeekTree(repoA, "b", "src")
	if !ok || !slices.Equal(treeNames(peek), []string{"feature.go"}) || when.IsZero() {
		t.Errorf("PeekTree(b, src) = %v, ok=%v; want the fetched listing with a fetch time", treeNames(peek), ok)
	}

	got, err = s.Tree(ctx, repoA, "", "src")
	if err != nil || !slices.Equal(treeNames(got), []string{"main.go"}) {
		t.Fatalf("Tree(default, src) = %v, %v; want main.go and nil", treeNames(got), err)
	}
	peek, _, ok = s.PeekTree(repoA, "", "src")
	if !ok || !slices.Equal(treeNames(peek), []string{"main.go"}) {
		t.Errorf("PeekTree(default, src) = %v, ok=%v; want main.go", treeNames(peek), ok)
	}
}

func TestPreviewCacheIsKeyedByRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFileAt(repoA, "", "notes.md", []byte("default\n"))
	f.SetFileAt(repoA, "b", "notes.md", []byte("branch\n"))
	s := core.New(f, core.Options{Now: stepClock()})

	onBranch := treeFile("notes.md", int64(len("branch\n")))
	got, err := s.Preview(ctx, repoA, "b", onBranch)
	if err != nil || got != (domain.FilePreview{Text: "branch\n"}) {
		t.Fatalf("Preview(b) = %+v, %v; want the branch's body and nil", got, err)
	}
	if _, _, ok := s.PeekPreview(repoA, "", "notes.md"); ok {
		t.Error("previewing on b left a cache entry for the default branch")
	}
	peek, when, ok := s.PeekPreview(repoA, "b", "notes.md")
	if !ok || peek != got || when.IsZero() {
		t.Errorf("PeekPreview(b) = %+v, ok=%v; want the fetched preview with a fetch time", peek, ok)
	}

	onDefault := treeFile("notes.md", int64(len("default\n")))
	got, err = s.Preview(ctx, repoA, "", onDefault)
	if err != nil || got != (domain.FilePreview{Text: "default\n"}) {
		t.Fatalf("Preview(default) = %+v, %v; want the default body and nil", got, err)
	}
	peek, _, ok = s.PeekPreview(repoA, "", "notes.md")
	if !ok || peek != got {
		t.Errorf("PeekPreview(default) = %+v, ok=%v; want the default preview", peek, ok)
	}
}
