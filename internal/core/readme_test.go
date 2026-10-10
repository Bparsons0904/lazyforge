package core_test

import (
	"context"
	"errors"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// noReadme hides the wrapped forge's ReadmeReader: embedding the interface promotes only forge.Forge.
type noReadme struct{ forge.Forge }

func TestReadmeReturnsAndCachesReadme(t *testing.T) {
	f := seeded()
	want := domain.Readme{Name: "README.md", Body: "# a\n"}
	f.SetReadme(repoA, want)
	s := core.New(f, core.Options{Now: stepClock()})
	if _, _, ok := s.PeekReadme(repoA); ok {
		t.Fatal("PeekReadme before any fetch reported ok")
	}

	got, err := s.Readme(context.Background(), repoA)
	if err != nil || got != want {
		t.Fatalf("Readme() = %+v, %v; want %+v, nil", got, err, want)
	}
	peek, at, ok := s.PeekReadme(repoA)
	if !ok || peek != want || !at.Equal(t0) {
		t.Errorf("PeekReadme() = %+v at %v, ok=%v; want %+v at %v, true", peek, at, ok, want, t0)
	}
}

func TestReadmeNotFoundIsEmptyAndCached(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	s := core.New(f, core.Options{Now: stepClock()})

	// repoA has no README; the adapter's not-found must come back as an empty Readme, not an error.
	got, err := s.Readme(ctx, repoA)
	if err != nil || got != (domain.Readme{}) {
		t.Fatalf("Readme() for a repo without a README = %+v, %v; want the empty Readme and nil", got, err)
	}
	peek, _, ok := s.PeekReadme(repoA)
	if !ok || peek != (domain.Readme{}) {
		t.Errorf("PeekReadme() after not-found = %+v, ok=%v; want the empty Readme cached", peek, ok)
	}

	got, err = s.Readme(ctx, repoA)
	if err != nil || got != (domain.Readme{}) {
		t.Errorf("second Readme() = %+v, %v; want the empty Readme and nil", got, err)
	}
	if peek, _, ok := s.PeekReadme(repoA); !ok || peek != (domain.Readme{}) {
		t.Errorf("PeekReadme() after the second call = %+v, ok=%v; want the empty Readme still cached", peek, ok)
	}

	// An unknown repo is also not-found from the adapter.
	got, err = s.Readme(ctx, domain.RepoRef{Owner: "no", Name: "pe"})
	if err != nil || got != (domain.Readme{}) {
		t.Errorf("Readme() for an unknown repo = %+v, %v; want the empty Readme and nil", got, err)
	}
}

func TestReadmeUnsupportedWithoutReadmeReader(t *testing.T) {
	s := core.New(noReadme{seeded()}, core.Options{})
	_, err := s.Readme(context.Background(), repoA)
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if _, _, ok := s.PeekReadme(repoA); ok {
		t.Error("unsupported Readme left a cache entry")
	}
}

func TestReadmePassesAdapterErrorsThrough(t *testing.T) {
	f := seeded()
	f.SetReadme(repoA, domain.Readme{Name: "README.md", Body: "old"})
	s := core.New(f, core.Options{Now: stepClock()})
	if _, err := s.Readme(context.Background(), repoA); err != nil {
		t.Fatal(err)
	}

	f.FailNext(forge.ErrRateLimited)
	_, err := s.Readme(context.Background(), repoA)
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("err = %v, want it to match ErrRateLimited", err)
	}
	if err.Error() == forge.ErrRateLimited.Error() {
		t.Errorf("err %q carries no context", err)
	}
	if peek, _, ok := s.PeekReadme(repoA); !ok || peek.Body != "old" {
		t.Errorf("PeekReadme after failed fetch = %+v, ok=%v; want the old README kept", peek, ok)
	}
}
