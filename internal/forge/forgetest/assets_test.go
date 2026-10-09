package forgetest_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/png"
	"io"
	"net/url"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var _ forge.AssetReader = (*forgetest.Fake)(nil)

const assetPath = "/attachments/contract"

// assetFake serves DemoPNG at assetPath on the host https://fake.test.
func assetFake() *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://fake.test"})
	f.AddAsset(assetPath, forgetest.DemoPNG)
	return f
}

// readAsset opens raw through f and reads the whole body.
func readAsset(ctx context.Context, f forge.AssetReader, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	rc, err := f.OpenAsset(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func TestDemoPNGDecodes(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(forgetest.DemoPNG))
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" || cfg.Width == 0 || cfg.Height == 0 {
		t.Errorf("DemoPNG decodes as %q %dx%d, want a non-empty png", format, cfg.Width, cfg.Height)
	}
}

func TestFakeOpenAssetServesSeededPath(t *testing.T) {
	ctx := context.Background()
	f := assetFake()
	tests := []string{
		"https://fake.test" + assetPath,
		"https://fake.test" + assetPath + "?sig=ignored",
		"https://FAKE.test" + assetPath,
		"https://fake.test:443" + assetPath,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			got, err := readAsset(ctx, f, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, forgetest.DemoPNG) {
				t.Errorf("body is %d bytes, want the %d-byte DemoPNG", len(got), len(forgetest.DemoPNG))
			}
		})
	}
}

func TestFakeOpenAssetAcceptsDecodableImage(t *testing.T) {
	got, err := readAsset(context.Background(), assetFake(), "https://fake.test"+assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(got)); err != nil || format != "png" {
		t.Errorf("served body decodes as %q, %v; want png", format, err)
	}
}

func TestFakeOpenAssetRefusesMissingPathOnHost(t *testing.T) {
	_, err := readAsset(context.Background(), assetFake(), "https://fake.test/attachments/unseeded")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestFakeOpenAssetRefusesOffOrigin(t *testing.T) {
	tests := []struct {
		name, raw string
	}{
		{"other hostname", "https://other.test" + assetPath},
		{"other port", "https://fake.test:8443" + assetPath},
		{"other scheme", "http://fake.test" + assetPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readAsset(context.Background(), assetFake(), tt.raw)
			if !errors.Is(err, forge.ErrUnsupported) {
				t.Errorf("got %v, want ErrUnsupported", err)
			}
		})
	}
}

func TestFakeOpenAssetRefusesEveryURLWithoutOrigin(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo})
	f.AddAsset(assetPath, forgetest.DemoPNG)
	for _, raw := range []string{"https://fake.test" + assetPath, assetPath} {
		_, err := readAsset(context.Background(), f, raw)
		if !errors.Is(err, forge.ErrUnsupported) {
			t.Errorf("OpenAsset(%q) = %v, want ErrUnsupported", raw, err)
		}
	}
}

func TestFakeOpenAssetOffOriginDoesNotConsumeFailNext(t *testing.T) {
	ctx := context.Background()
	f := assetFake()
	boom := errors.New("boom")
	f.FailNext(boom)

	if _, err := readAsset(ctx, f, "https://other.test"+assetPath); !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("off-origin got %v, want ErrUnsupported", err)
	}
	if _, err := readAsset(ctx, f, "https://fake.test"+assetPath); !errors.Is(err, boom) {
		t.Fatalf("first on-host call got %v, want the FailNext error", err)
	}
	got, err := readAsset(ctx, f, "https://fake.test"+assetPath)
	if err != nil {
		t.Fatalf("second on-host call: %v; FailNext should have cleared", err)
	}
	if !bytes.Equal(got, forgetest.DemoPNG) {
		t.Error("second on-host call did not return the seeded bytes")
	}
}

func TestFakeOpenAssetHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readAsset(ctx, assetFake(), "https://fake.test"+assetPath)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}
