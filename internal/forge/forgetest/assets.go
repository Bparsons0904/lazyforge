package forgetest

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"slices"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// DemoPNG is a small valid PNG for seeding Fake assets.
//
//go:embed demo.png
var DemoPNG []byte

// AddAsset seeds the body OpenAsset returns for path, matched on the URL's path alone.
func (f *Fake) AddAsset(path string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets[path] = slices.Clone(data)
}

// OpenAsset implements forge.AssetReader; an off-host URL is refused before FailNext is consumed.
func (f *Fake) OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	base, err := url.Parse(f.info.URL)
	if err != nil || !forge.SameOrigin(u, base) {
		return nil, fmt.Errorf("open asset %s: off the session's host: %w", fakeAssetLabel(u), forge.ErrUnsupported)
	}
	defer f.mu.Unlock()
	if err := f.begin(ctx); err != nil {
		return nil, err
	}
	data, ok := f.assets[u.Path]
	if !ok {
		return nil, fmt.Errorf("asset %s: %w", u.Path, forge.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fakeAssetLabel names u for refusals without its query string.
func fakeAssetLabel(u *url.URL) string {
	if u == nil {
		return "<nil>"
	}
	return u.Host + u.Path
}
