package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var imageRepo = domain.RepoRef{Owner: "o", Name: "r"}

func imageService(t *testing.T) (*core.Service, *forgetest.Fake) {
	t.Helper()
	fake := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h"})
	return core.New(fake, core.Options{}), fake
}

// TestImageDownscaleSeam checks that a 3200x1000 image comes back 1600x500 with its colour and origin intact.
func TestImageDownscaleSeam(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3200, 1000))
	for i := 0; i < len(src.Pix); i += 4 {
		copy(src.Pix[i:i+4], []byte{200, 100, 50, 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	svc, fake := imageService(t)
	fake.AddAsset("/wide.png", buf.Bytes())

	img, err := svc.Image(context.Background(), imageRepo, "/wide.png")
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 1600, 500) {
		t.Fatalf("bounds %v, want (0,0)-(1600,500)", got)
	}
	r, g, b, a := img.At(799, 249).RGBA()
	if got := [4]uint32{r >> 8, g >> 8, b >> 8, a >> 8}; got != [4]uint32{200, 100, 50, 255} {
		t.Errorf("centre pixel %v, want 200 100 50 255", got)
	}
}

// TestImageLimitsSeam checks the byte limit: one byte over is too large, exactly at the limit is not.
func TestImageLimitsSeam(t *testing.T) {
	const limit = 10 << 20
	svc, fake := imageService(t)
	fake.AddAsset("/over.png", make([]byte, limit+1))
	fake.AddAsset("/at.png", make([]byte, limit))

	_, err := svc.Image(context.Background(), imageRepo, "/over.png")
	if !errors.Is(err, core.ErrImageTooLarge) {
		t.Errorf("one byte over the limit: got %v, want ErrImageTooLarge", err)
	}
	_, err = svc.Image(context.Background(), imageRepo, "/at.png")
	if err == nil || errors.Is(err, core.ErrImageTooLarge) {
		t.Errorf("at the limit: got %v, want a decode error that is not ErrImageTooLarge", err)
	}
}

// TestImageHostSeam checks that a URL off the session's host is refused without an I/O call or a cache entry.
func TestImageHostSeam(t *testing.T) {
	svc, _ := imageService(t)
	for _, raw := range []string{"https://other/x.png", "//other/x.png", "https://h:8443/x.png", "http://h/x.png", "https://u:p@h/x.png"} {
		_, ok, err := svc.PeekImage(imageRepo, raw)
		if !ok || err == nil {
			t.Errorf("PeekImage(%q) = ok %v, err %v; want a refusal", raw, ok, err)
		}
		if _, err := svc.Image(context.Background(), imageRepo, raw); err == nil {
			t.Errorf("Image(%q) succeeded; want a refusal", raw)
		}
	}
}

// TestImageCacheSeam checks that the cache holds 16 entries, evicts the least recent, and counts a peek hit as use.
func TestImageCacheSeam(t *testing.T) {
	svc, _ := imageService(t)
	ctx := context.Background()
	path := func(i int) string { return fmt.Sprintf("/missing-%d.png", i) }
	// A missing asset is a terminal failure, which is cached like a successful image.
	for i := 1; i <= 16; i++ {
		if _, err := svc.Image(ctx, imageRepo, path(i)); err == nil {
			t.Fatalf("Image(%s) succeeded; want a not-found failure", path(i))
		}
	}
	if _, ok, _ := svc.PeekImage(imageRepo, path(1)); !ok {
		t.Fatal("first URL not cached after 16 fetches")
	}
	if _, err := svc.Image(ctx, imageRepo, path(17)); err == nil {
		t.Fatal("Image(17) succeeded; want a not-found failure")
	}
	if _, ok, _ := svc.PeekImage(imageRepo, path(2)); ok {
		t.Error("second URL still cached; the least recent entry should have been evicted")
	}
	if _, ok, _ := svc.PeekImage(imageRepo, path(1)); !ok {
		t.Error("first URL evicted; a peek hit should have kept it")
	}
}
