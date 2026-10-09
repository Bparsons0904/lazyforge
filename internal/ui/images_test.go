package ui

import (
	"context"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// cachedImageModel returns a loaded model whose service holds one fetched image.
func cachedImageModel(t *testing.T) (Model, domain.RepoRef, string) {
	t.Helper()
	const raw = "/attachments/x.png"
	ref := domain.RepoRef{Owner: "o", Name: "r"}
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h", User: "bob", ChangeRequestTerm: "PR"})
	f.AddAsset("/attachments/x.png", forgetest.DemoPNG)
	m := boot(t, seededWith(t, f))
	if _, err := m.svc.Image(context.Background(), ref, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.svc.PeekImage(ref, raw); !ok {
		t.Fatal("the image is not cached before the test acts")
	}
	return m, ref, raw
}

func TestRefreshKeyClearsImageCache(t *testing.T) {
	m, ref, raw := cachedImageModel(t)
	m = press(t, m, "r")
	if _, ok, _ := m.svc.PeekImage(ref, raw); ok {
		t.Error("the r key left the image cached")
	}
}

func TestRefreshTickKeepsImageCache(t *testing.T) {
	m, ref, raw := cachedImageModel(t)
	m = run(t, m, refreshTickMsg{})
	if _, ok, _ := m.svc.PeekImage(ref, raw); !ok {
		t.Error("the five-minute refresh tick cleared the image cache")
	}
}
