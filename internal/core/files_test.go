package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// noTrees hides the wrapped forge's TreeReader: embedding the interface promotes only forge.Forge.
type noTrees struct{ forge.Forge }

func treeFile(name string, size int64) domain.TreeEntry {
	return domain.TreeEntry{Name: name, Path: name, Type: domain.EntryFile, Size: size}
}

func treeDir(name string) domain.TreeEntry {
	return domain.TreeEntry{Name: name, Path: name, Type: domain.EntryDir}
}

func treeNames(es []domain.TreeEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestTreeSortsDirectoriesFirstThenByName(t *testing.T) {
	f := seeded()
	f.SetTree(repoA, "", []domain.TreeEntry{
		treeFile("b.txt", 1),
		treeDir("zeta"),
		{Name: "Link", Path: "Link", Type: domain.EntrySymlink},
		treeFile("A.txt", 1),
		treeDir("alpha"),
		treeFile("a.txt", 1),
		{Name: "vendor", Path: "vendor", Type: domain.EntrySubmodule},
		treeDir("Beta"),
	})
	s := core.New(f, core.Options{Now: stepClock()})

	got, err := s.Tree(context.Background(), repoA, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Directories come first. Within each group the order is case-insensitive, and the exact name breaks ties.
	want := []string{"alpha", "Beta", "zeta", "A.txt", "a.txt", "b.txt", "Link", "vendor"}
	if !slices.Equal(treeNames(got), want) {
		t.Errorf("order = %v, want %v", treeNames(got), want)
	}
}

func TestTreeCachedPerDirectory(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetTree(repoA, "src", []domain.TreeEntry{treeFile("main.go", 10)})
	s := core.New(f, core.Options{Now: stepClock()})

	if _, _, ok := s.PeekTree(repoA, "", "src"); ok {
		t.Fatal("PeekTree before any fetch reported ok")
	}
	if got, err := s.Tree(ctx, repoA, "", "src"); err != nil || len(got) != 1 {
		t.Fatalf("Tree(src) = %+v, %v; want one entry and nil", got, err)
	}
	if _, _, ok := s.PeekTree(repoA, "", ""); ok {
		t.Error("fetching src left a cache entry for the root")
	}

	f.FailNext(forge.ErrRateLimited)
	peek, when, ok := s.PeekTree(repoA, "", "src")
	if !ok || !slices.Equal(treeNames(peek), []string{"main.go"}) {
		t.Errorf("PeekTree(src) = %+v, ok=%v; want the fetched listing", peek, ok)
	}
	if when.IsZero() {
		t.Error("PeekTree returned a zero fetch time")
	}
	// PeekTree does no I/O, so the armed failure is still waiting for the next fetch.
	if _, err := s.Tree(ctx, repoA, "", "src"); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("Tree(src) after PeekTree err = %v, want the armed ErrRateLimited", err)
	}
}

func TestTreeNotFoundIsEmptyAndCached(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	s := core.New(f, core.Options{Now: stepClock()})

	// A directory nothing was seeded for is what the adapter reports as not found: an empty repo, or a directory deleted since it was listed.
	got, err := s.Tree(ctx, repoA, "", "gone")
	if err != nil || len(got) != 0 {
		t.Fatalf("Tree() for a missing directory = %+v, %v; want empty and nil", got, err)
	}
	if _, err := s.Tree(ctx, domain.RepoRef{Owner: "no", Name: "pe"}, "", ""); err != nil {
		t.Errorf("Tree() for an unknown repo err = %v, want nil", err)
	}
	peek, _, ok := s.PeekTree(repoA, "", "gone")
	if !ok || len(peek) != 0 {
		t.Errorf("PeekTree() after not-found = %+v, ok=%v; want the empty listing cached", peek, ok)
	}

	f.FailNext(forge.ErrRateLimited)
	// Fetches always reach the forge; a failed refetch must not evict the cached empty listing.
	if _, err := s.Tree(ctx, repoA, "", "gone"); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("second Tree() err = %v, want the forge to be asked again and fail with ErrRateLimited", err)
	}
	if peek, _, ok := s.PeekTree(repoA, "", "gone"); !ok || len(peek) != 0 {
		t.Errorf("PeekTree() after a failed refetch = %+v, ok=%v; want the empty listing kept", peek, ok)
	}
}

func TestTreeUnsupportedWithoutTreeReader(t *testing.T) {
	s := core.New(noTrees{seeded()}, core.Options{})
	if _, err := s.Tree(context.Background(), repoA, "", ""); !errors.Is(err, forge.ErrUnsupported) {
		t.Errorf("Tree() err = %v, want ErrUnsupported", err)
	}
	if _, _, ok := s.PeekTree(repoA, "", ""); ok {
		t.Error("unsupported Tree left a cache entry")
	}
}

func TestPreviewTooLargeBySizeWithoutReadingTheFile(t *testing.T) {
	// Nothing is seeded at this path, so a ReadFile call would fail with ErrNotFound instead of returning TooLarge.
	s := core.New(seeded(), core.Options{Now: stepClock()})
	e := treeFile("big.iso", core.MaxPreviewSize+1)

	got, err := s.Preview(context.Background(), repoA, "", e)
	if err != nil || got != (domain.FilePreview{TooLarge: true}) {
		t.Errorf("Preview() = %+v, %v; want {TooLarge: true} and nil", got, err)
	}
}

func TestPreviewAcceptsExactlyTheCap(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	f.SetFile(repoA, "edge.txt", []byte(strings.Repeat("a", core.MaxPreviewSize)))
	s := core.New(f, core.Options{Now: stepClock()})

	got, err := s.Preview(ctx, repoA, "", treeFile("edge.txt", core.MaxPreviewSize))
	if err != nil || got.TooLarge || got.Binary || len(got.Text) != core.MaxPreviewSize {
		t.Errorf("Preview() at the cap: TooLarge=%v Binary=%v len(Text)=%d err=%v; want the whole file as text",
			got.TooLarge, got.Binary, len(got.Text), err)
	}
}

func TestPreviewTooLargeByBodyLength(t *testing.T) {
	f := seeded()
	f.SetFile(repoA, "grown.txt", []byte(strings.Repeat("a", core.MaxPreviewSize+1)))
	s := core.New(f, core.Options{Now: stepClock()})

	// The listing said one byte, but the body is over the cap, so the body length decides.
	got, err := s.Preview(context.Background(), repoA, "", treeFile("grown.txt", 1))
	if err != nil || !got.TooLarge || got.Binary {
		t.Errorf("Preview() = %+v, %v; want TooLarge and nil", got, err)
	}
}

func TestPreviewBinaryForNULOrInvalidUTF8(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"NUL byte", []byte("PK\x03\x00\x04")},
		{"invalid UTF-8", []byte{'o', 'k', 0xff, 0xfe}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seeded()
			f.SetFile(repoA, "blob", tt.body)
			s := core.New(f, core.Options{Now: stepClock()})

			got, err := s.Preview(context.Background(), repoA, "", treeFile("blob", int64(len(tt.body))))
			if err != nil || !got.Binary || got.TooLarge {
				t.Errorf("Preview() = %+v, %v; want Binary and nil", got, err)
			}
		})
	}
}

func TestPreviewTextAndPeekPreview(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	const body = "# Notes\n\nhéllo\n"
	f.SetFile(repoA, "notes.md", []byte(body))
	s := core.New(f, core.Options{Now: stepClock()})

	if _, _, ok := s.PeekPreview(repoA, "", "notes.md"); ok {
		t.Fatal("PeekPreview before any fetch reported ok")
	}
	got, err := s.Preview(ctx, repoA, "", treeFile("notes.md", int64(len(body))))
	if err != nil || got != (domain.FilePreview{Text: body}) {
		t.Fatalf("Preview() = %+v, %v; want the file as text and nil", got, err)
	}
	peek, when, ok := s.PeekPreview(repoA, "", "notes.md")
	if !ok || peek != got {
		t.Errorf("PeekPreview() = %+v, ok=%v; want the fetched preview", peek, ok)
	}
	if when.IsZero() {
		t.Error("PeekPreview returned a zero fetch time")
	}
}

func TestPreviewZeroByteFileIsEmpty(t *testing.T) {
	f := seeded()
	f.SetFile(repoA, "empty.txt", []byte{})
	s := core.New(f, core.Options{Now: stepClock()})

	got, err := s.Preview(context.Background(), repoA, "", treeFile("empty.txt", 0))
	if err != nil || got != (domain.FilePreview{}) {
		t.Errorf("Preview() of a zero-byte file = %+v, %v; want the zero FilePreview and nil", got, err)
	}
}

func TestPreviewMissingFileIsNotFound(t *testing.T) {
	s := core.New(seeded(), core.Options{Now: stepClock()})

	_, err := s.Preview(context.Background(), repoA, "", treeFile("gone.txt", 5))
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("Preview() err = %v, want ErrNotFound", err)
	}
}
