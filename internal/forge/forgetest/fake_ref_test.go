package forgetest_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestSetTreeAtIsScopedToItsRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	onDefault := []domain.TreeEntry{{Name: "main.go", Path: "main.go", Type: domain.EntryFile}}
	onBranch := []domain.TreeEntry{{Name: "feature.go", Path: "feature.go", Type: domain.EntryFile}}
	f.SetTree(repoRef, "", onDefault)
	f.SetTreeAt(repoRef, "b", "", onBranch)

	got, err := f.ListTree(ctx, repoRef, "b", "")
	if err != nil {
		t.Fatalf("ListTree on b: %v", err)
	}
	if !slices.Equal(got, onBranch) {
		t.Errorf("ListTree(b) = %+v, want %+v", got, onBranch)
	}

	got, err = f.ListTree(ctx, repoRef, "", "")
	if err != nil {
		t.Fatalf("ListTree on default: %v", err)
	}
	if !slices.Equal(got, onDefault) {
		t.Errorf("ListTree(default) = %+v, want %+v", got, onDefault)
	}
}

func TestSetTreeIsSetTreeAtDefaultRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	want := []domain.TreeEntry{{Name: "a.go", Path: "docs/a.go", Type: domain.EntryFile}}
	f.SetTree(repoRef, "docs", want)

	got, err := f.ListTree(ctx, repoRef, "", "docs")
	if err != nil {
		t.Fatalf("ListTree on default: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("ListTree(default, docs) = %+v, want %+v", got, want)
	}
	if _, err := f.ListTree(ctx, repoRef, "b", "docs"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListTree(b, docs) err = %v, want ErrNotFound: SetTree must not seed b", err)
	}
}

func TestSetFileAtIsScopedToItsRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFile(repoRef, "notes.txt", []byte("default\n"))
	f.SetFileAt(repoRef, "b", "notes.txt", []byte("branch\n"))

	got, err := f.ReadFile(ctx, repoRef, "b", "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile on b: %v", err)
	}
	if string(got) != "branch\n" {
		t.Errorf("ReadFile(b) = %q, want %q", got, "branch\n")
	}

	got, err = f.ReadFile(ctx, repoRef, "", "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile on default: %v", err)
	}
	if string(got) != "default\n" {
		t.Errorf("ReadFile(default) = %q, want %q", got, "default\n")
	}
}

func TestSetFileIsSetFileAtDefaultRef(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFile(repoRef, "notes.txt", []byte("hello\n"))

	got, err := f.ReadFile(ctx, repoRef, "", "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile on default: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("ReadFile(default) = %q, want %q", got, "hello\n")
	}
	if _, err := f.ReadFile(ctx, repoRef, "b", "notes.txt"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ReadFile(b) err = %v, want ErrNotFound: SetFile must not seed b", err)
	}
}

func TestUnseededRefIsNotFound(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTreeAt(repoRef, "b", "", []domain.TreeEntry{{Name: "x", Path: "x", Type: domain.EntryFile}})
	f.SetFileAt(repoRef, "b", "x", []byte("x"))

	if _, err := f.ListTree(ctx, repoRef, "c", ""); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListTree on unseeded ref: err = %v, want ErrNotFound", err)
	}
	if _, err := f.ReadFile(ctx, repoRef, "c", "x"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ReadFile on unseeded ref: err = %v, want ErrNotFound", err)
	}
}

func TestRefSeedsAreScopedToTheRepo(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTreeAt(repoRef, "b", "", []domain.TreeEntry{{Name: "x", Path: "x", Type: domain.EntryFile}})
	f.SetFileAt(repoRef, "b", "x", []byte("x"))

	if _, err := f.ListTree(ctx, otherRef, "b", ""); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ListTree on another repo: err = %v, want ErrNotFound", err)
	}
	if _, err := f.ReadFile(ctx, otherRef, "b", "x"); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("ReadFile on another repo: err = %v, want ErrNotFound", err)
	}
}
