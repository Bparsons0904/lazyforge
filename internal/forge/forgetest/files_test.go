package forgetest_test

import (
	"context"
	"errors"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var _ forge.TreeReader = (*forgetest.Fake)(nil)

func TestListTreeReturnsSeededEntries(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	want := []domain.TreeEntry{
		{Name: "docs", Path: "docs", Type: domain.EntryDir},
		{Name: "README.md", Path: "README.md", Type: domain.EntryFile, Size: 12},
	}
	f.SetTree(repoRef, "", want)

	got, err := f.ListTree(ctx, repoRef, "", "")
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ListTree = %+v, want %+v", got, want)
	}
}

func TestListTreeKeysByDirectory(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTree(repoRef, "", []domain.TreeEntry{{Name: "src", Path: "src", Type: domain.EntryDir}})
	f.SetTree(repoRef, "src", []domain.TreeEntry{{Name: "main.go", Path: "src/main.go", Type: domain.EntryFile}})

	got, err := f.ListTree(ctx, repoRef, "", "src")
	if err != nil {
		t.Fatalf("ListTree(src): %v", err)
	}
	if len(got) != 1 || got[0].Path != "src/main.go" {
		t.Errorf("ListTree(src) = %+v, want only src/main.go", got)
	}
}

func TestListTreeErrors(t *testing.T) {
	tests := []struct {
		name string
		repo domain.RepoRef
		dir  string
	}{
		{"known repo, dir never seeded", repoRef, "missing"},
		{"unknown repo", domain.RepoRef{Owner: "no", Name: "pe"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := seeded().ListTree(context.Background(), tt.repo, "", tt.dir)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestListTreeFailNextAndCancel(t *testing.T) {
	t.Run("FailNext is returned once", func(t *testing.T) {
		ctx := context.Background()
		f := seeded()
		f.SetTree(repoRef, "", []domain.TreeEntry{{Name: "a", Path: "a", Type: domain.EntryFile}})
		f.FailNext(forge.ErrRateLimited)

		if _, err := f.ListTree(ctx, repoRef, "", ""); !errors.Is(err, forge.ErrRateLimited) {
			t.Fatalf("first call: err = %v, want ErrRateLimited", err)
		}
		if _, err := f.ListTree(ctx, repoRef, "", ""); err != nil {
			t.Errorf("second call after FailNext: err = %v, want nil", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f := seeded()
		f.SetTree(repoRef, "", []domain.TreeEntry{{Name: "a", Path: "a", Type: domain.EntryFile}})

		if _, err := f.ListTree(ctx, repoRef, "", ""); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestListTreeResultIsACopy(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTree(repoRef, "", []domain.TreeEntry{{Name: "a", Path: "a", Type: domain.EntryFile}})

	got, _ := f.ListTree(ctx, repoRef, "", "")
	got[0].Name = "mutated"

	again, _ := f.ListTree(ctx, repoRef, "", "")
	if again[0].Name != "a" {
		t.Errorf("mutating a returned listing changed the Fake: name = %q, want %q", again[0].Name, "a")
	}
}

func TestReadFileReturnsSeededBody(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFile(repoRef, "docs/notes.txt", []byte("hello\n"))

	got, err := f.ReadFile(ctx, repoRef, "", "docs/notes.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("ReadFile = %q, want %q", got, "hello\n")
	}
}

func TestReadFileErrors(t *testing.T) {
	tests := []struct {
		name string
		repo domain.RepoRef
		path string
	}{
		{"known repo, path never seeded", repoRef, "missing.txt"},
		{"unknown repo", domain.RepoRef{Owner: "no", Name: "pe"}, "a.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := seeded().ReadFile(context.Background(), tt.repo, "", tt.path)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestReadFileFailNextAndCancel(t *testing.T) {
	t.Run("FailNext is returned once", func(t *testing.T) {
		ctx := context.Background()
		f := seeded()
		f.SetFile(repoRef, "a.txt", []byte("a"))
		f.FailNext(forge.ErrUnauthorized)

		if _, err := f.ReadFile(ctx, repoRef, "", "a.txt"); !errors.Is(err, forge.ErrUnauthorized) {
			t.Fatalf("first call: err = %v, want ErrUnauthorized", err)
		}
		if _, err := f.ReadFile(ctx, repoRef, "", "a.txt"); err != nil {
			t.Errorf("second call after FailNext: err = %v, want nil", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f := seeded()
		f.SetFile(repoRef, "a.txt", []byte("a"))

		if _, err := f.ReadFile(ctx, repoRef, "", "a.txt"); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestReadFileResultIsACopy(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFile(repoRef, "a.txt", []byte("abc"))

	got, _ := f.ReadFile(ctx, repoRef, "", "a.txt")
	got[0] = 'X'

	again, _ := f.ReadFile(ctx, repoRef, "", "a.txt")
	if string(again) != "abc" {
		t.Errorf("mutating a returned body changed the Fake: got %q, want %q", again, "abc")
	}
}

func TestSetFileDoesNotAliasCallerBuffer(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	body := []byte("abc")
	f.SetFile(repoRef, "a.txt", body)
	body[0] = 'X'

	got, _ := f.ReadFile(ctx, repoRef, "", "a.txt")
	if string(got) != "abc" {
		t.Errorf("mutating the buffer passed to SetFile changed the Fake: got %q, want %q", got, "abc")
	}
}
