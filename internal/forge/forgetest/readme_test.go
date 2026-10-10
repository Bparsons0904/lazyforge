package forgetest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var _ forge.ReadmeReader = (*forgetest.Fake)(nil)

func TestReadmeReturnsSetReadme(t *testing.T) {
	f := seeded()
	want := domain.Readme{Name: "README.md", Body: "# hi\n"}
	f.SetReadme(repoRef, want)
	got, err := f.GetReadme(context.Background(), repoRef)
	if err != nil || got != want {
		t.Errorf("GetReadme() = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestReadmeNotFound(t *testing.T) {
	ctx := context.Background()
	f := seeded()
	// A known repo with no SetReadme has no README.
	if _, err := f.GetReadme(ctx, repoRef); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("known repo without a README: err = %v, want ErrNotFound", err)
	}
	if _, err := f.GetReadme(ctx, domain.RepoRef{Owner: "no", Name: "pe"}); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("unknown repo: err = %v, want ErrNotFound", err)
	}
}

func TestSetReadmeMarksRepoKnown(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo})
	fresh := domain.RepoRef{Owner: "owner", Name: "fresh"}
	f.SetReadme(fresh, domain.Readme{Name: "README", Body: ""})
	got, err := f.GetReadme(context.Background(), fresh)
	if err != nil || got != (domain.Readme{Name: "README"}) {
		t.Errorf("GetReadme() = %+v, %v; want an empty-body README and nil", got, err)
	}
}

func TestReadmeFailNextAndCancel(t *testing.T) {
	f := seeded()
	f.SetReadme(repoRef, domain.Readme{Name: "README.md", Body: "x"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.GetReadme(ctx, repoRef); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v, want context.Canceled", err)
	}

	f.FailNext(forge.ErrRateLimited)
	if _, err := f.GetReadme(context.Background(), repoRef); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("FailNext: err = %v, want ErrRateLimited", err)
	}
}

func TestDemoReadmes(t *testing.T) {
	ctx := context.Background()
	f := forgetest.NewDemo(time.Now())
	repos, err := f.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var without []string
	for _, r := range repos {
		rd, err := f.GetReadme(ctx, r.RepoRef)
		if errors.Is(err, forge.ErrNotFound) {
			without = append(without, r.Owner+"/"+r.Name)
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		if rd.Name != "README.md" {
			t.Errorf("%s: Name = %q, want README.md", r.Name, rd.Name)
		}
		if !strings.HasPrefix(rd.Body, "# ") || !strings.Contains(rd.Body, "\n- ") || !strings.Contains(rd.Body, "](") {
			t.Errorf("%s: body lacks a heading, list and link:\n%s", r.Name, rd.Body)
		}
	}
	if len(without) != 1 {
		t.Errorf("repos without a README = %v, want exactly one", without)
	}
}
