package core_test

import (
	"errors"
	"net/url"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	gRepoA = domain.RepoRef{Owner: "g", Name: "a"}
	gRepoB = domain.RepoRef{Owner: "g", Name: "b"}
)

// resolving is a recorder that also resolves body links, as a forge.AssetResolver does.
type resolving struct {
	*recorder
	resolve func(repo domain.RepoRef, ref *url.URL) (*url.URL, bool)
}

func (r *resolving) ResolveAsset(repo domain.RepoRef, ref *url.URL) (*url.URL, bool) {
	return r.resolve(repo, ref)
}

// perProject maps /uploads/... under the project, as GitLab does.
func perProject(repo domain.RepoRef, ref *url.URL) (*url.URL, bool) {
	if len(ref.Path) < 9 || ref.Path[:9] != "/uploads/" {
		return nil, false
	}
	return &url.URL{Scheme: "https", Host: "h", Path: "/" + repo.Owner + "/" + repo.Name + ref.Path}, true
}

func newResolving(t *testing.T, resolve func(domain.RepoRef, *url.URL) (*url.URL, bool)) (*core.Service, *resolving) {
	t.Helper()
	rec := &recorder{
		Fake:  forgetest.NewFake(forge.HostInfo{Kind: forge.KindGitLab, URL: "https://h", User: "bob", ChangeRequestTerm: "MR"}),
		serve: serveBytes(forgetest.DemoPNG),
	}
	r := &resolving{recorder: rec, resolve: resolve}
	return core.New(r, core.Options{}), r
}

func TestImageUsesTheForgesResolvedURL(t *testing.T) {
	svc, r := newResolving(t, perProject)
	if _, err := svc.Image(t.Context(), gRepoA, "/uploads/s/x.png"); err != nil {
		t.Fatal(err)
	}
	calls := r.callList()
	if len(calls) != 1 || calls[0].url != "https://h/g/a/uploads/s/x.png" {
		t.Fatalf("OpenAsset calls %+v, want one for https://h/g/a/uploads/s/x.png", calls)
	}
}

// Image always fetches and only PeekImage reads the cache, so the per-project entries are checked by peeking.
func TestImageCachesTheSameLinkPerProject(t *testing.T) {
	svc, r := newResolving(t, perProject)
	for _, repo := range []domain.RepoRef{gRepoA, gRepoB} {
		if _, err := svc.Image(t.Context(), repo, "/uploads/s/x.png"); err != nil {
			t.Fatal(err)
		}
	}
	calls := r.callList()
	if len(calls) != 2 || calls[0].url == calls[1].url {
		t.Fatalf("OpenAsset calls %+v, want one per project at different URLs", calls)
	}
	for _, repo := range []domain.RepoRef{gRepoA, gRepoB} {
		if _, ok, err := svc.PeekImage(repo, "/uploads/s/x.png"); err != nil || !ok {
			t.Errorf("PeekImage(%v) ok=%v err=%v, want each project's own entry cached", repo, ok, err)
		}
	}
}

func TestPeekImageSeesOnlyTheProjectsOwnCachedImage(t *testing.T) {
	svc, r := newResolving(t, perProject)
	if _, err := svc.Image(t.Context(), gRepoA, "/uploads/s/x.png"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := svc.PeekImage(gRepoA, "/uploads/s/x.png"); err != nil || !ok {
		t.Errorf("PeekImage(gRepoA) ok=%v err=%v, want a hit", ok, err)
	}
	if _, ok, err := svc.PeekImage(gRepoB, "/uploads/s/x.png"); err != nil || ok {
		t.Errorf("PeekImage(gRepoB) ok=%v err=%v, want a miss", ok, err)
	}
	if n := len(r.callList()); n != 1 {
		t.Errorf("%d OpenAsset calls, want PeekImage to do no I/O", n)
	}
}

func TestImageRefusesAResolvedURLOffTheOrigin(t *testing.T) {
	svc, r := newResolving(t, func(domain.RepoRef, *url.URL) (*url.URL, bool) {
		return &url.URL{Scheme: "https", Host: "evil.example", Path: "/x.png"}, true
	})
	_, err := svc.Image(t.Context(), gRepoA, "/uploads/s/x.png")
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
	if n := len(r.callList()); n != 0 {
		t.Fatalf("%d OpenAsset calls, want none", n)
	}
}

func TestImageStripsTheFragmentFromAResolvedURL(t *testing.T) {
	svc, r := newResolving(t, func(domain.RepoRef, *url.URL) (*url.URL, bool) {
		return &url.URL{Scheme: "https", Host: "h", Path: "/g/a/x.png", Fragment: "frag"}, true
	})
	if _, err := svc.Image(t.Context(), gRepoA, "/uploads/s/x.png"); err != nil {
		t.Fatal(err)
	}
	if calls := r.callList(); len(calls) != 1 || calls[0].url != "https://h/g/a/x.png" {
		t.Fatalf("OpenAsset calls %+v, want one for https://h/g/a/x.png", calls)
	}
}

func TestImageFallsBackToDefaultResolutionWhenTheForgeDeclines(t *testing.T) {
	svc, r := newResolving(t, perProject)
	if _, err := svc.Image(t.Context(), gRepoA, "attachments/a.png"); err != nil {
		t.Fatal(err)
	}
	if calls := r.callList(); len(calls) != 1 || calls[0].url != "https://h/g/a/attachments/a.png" {
		t.Fatalf("OpenAsset calls %+v, want one for https://h/g/a/attachments/a.png", calls)
	}
}

func TestImageWithoutAResolverIsUnchanged(t *testing.T) {
	svc, f := newImages(t, nil)
	if _, err := svc.Image(t.Context(), imgRepo, "/uploads/s/x.png"); err != nil {
		t.Fatal(err)
	}
	if calls := f.callList(); len(calls) != 1 || calls[0].url != "https://h/uploads/s/x.png" {
		t.Fatalf("OpenAsset calls %+v, want one for https://h/uploads/s/x.png", calls)
	}
}
