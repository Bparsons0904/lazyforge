package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// listFake overrides ListReleases so a test can make the forge fail and count the calls.
type listFake struct {
	*forgetest.Fake
	err   error
	calls atomic.Int32
}

func (f *listFake) ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.Fake.ListReleases(ctx, r)
}

// releaseTags returns the tags of rels in order.
func releaseTags(rels []domain.Release) []string {
	tags := make([]string, 0, len(rels))
	for _, r := range rels {
		tags = append(tags, r.Tag)
	}
	return tags
}

func TestReleasesNewestFirstUndatedLastTiesStable(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA})
	// The adapter order is neither chronological nor grouped by date.
	for _, r := range []domain.Release{
		{Tag: "undated-a"},
		{Tag: "old", PublishedAt: t0},
		{Tag: "tie-first", PublishedAt: t0.Add(24 * time.Hour)},
		{Tag: "new", PublishedAt: t0.Add(48 * time.Hour)},
		{Tag: "tie-second", PublishedAt: t0.Add(24 * time.Hour)},
		{Tag: "undated-b"},
	} {
		f.AddRelease(repoA, r)
	}
	want := []string{"new", "tie-first", "tie-second", "old", "undated-a", "undated-b"}

	s := core.New(f, core.Options{})
	got, err := s.Releases(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(releaseTags(got), want) {
		t.Fatalf("Releases order %v, want %v", releaseTags(got), want)
	}
	peeked, _, ok := s.PeekReleases(repoA)
	if !ok || !slices.Equal(releaseTags(peeked), want) {
		t.Errorf("PeekReleases order %v (cached %v), want %v", releaseTags(peeked), ok, want)
	}
}

func TestReleasesEmptyRepoIsEmptyWithoutError(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	empty := domain.RepoRef{Owner: "owner", Name: "empty"}
	f.AddRepo(domain.Repo{RepoRef: empty})

	got, err := core.New(f, core.Options{}).Releases(context.Background(), empty)
	if err != nil || len(got) != 0 {
		t.Errorf("Releases = %v, %v; want no releases and no error", got, err)
	}
}

func TestReleasesNotFoundIsEmptyAndCached(t *testing.T) {
	f := &listFake{Fake: seeded(), err: fmt.Errorf("releases of %v: %w", repoA, forge.ErrNotFound)}
	s := core.New(f, core.Options{})
	for i := range 2 {
		got, err := s.Releases(context.Background(), repoA)
		if err != nil || len(got) != 0 {
			t.Fatalf("call %d: Releases = %v, %v; want no releases and no error", i+1, got, err)
		}
	}
	peeked, _, ok := s.PeekReleases(repoA)
	if !ok || len(peeked) != 0 {
		t.Errorf("PeekReleases = %v (cached %v); want the empty answer cached", peeked, ok)
	}
}

func TestReleasesOtherErrorIsNotMappedToEmpty(t *testing.T) {
	boom := errors.New("forge exploded")
	f := &listFake{Fake: seeded(), err: boom}

	_, err := core.New(f, core.Options{}).Releases(context.Background(), repoA)
	if !errors.Is(err, boom) {
		t.Fatalf("Releases error %v, want the adapter's error", err)
	}
}
