package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// OpenAsset takes URLs on the web root, not the API, because github.com/user-attachments redirects to signed
// object storage. A private repo's attachment needs a browser session, so it fails with ErrNotFound.
func (f *Forge) OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	base, err := url.Parse(f.web)
	if err != nil {
		return nil, fmt.Errorf("open asset: parse host: %w", err)
	}
	if !forge.SameOrigin(u, base) {
		return nil, fmt.Errorf("open asset %s: off the session's host: %w", assetLabel(u), forge.ErrUnsupported)
	}
	op := "GET " + u.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request", op)
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.tokenOnlyOn(base).Do(req)
	if err != nil {
		return nil, doError(op, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.Body, nil
	}
	if resp.StatusCode >= 400 {
		return nil, checkStatus(op, resp)
	}
	_ = resp.Body.Close()
	return nil, fmt.Errorf("%s: unexpected status %d", op, resp.StatusCode)
}

// assetLabel drops the query string, which carries credentials on signed storage URLs.
func assetLabel(u *url.URL) string {
	if u == nil {
		return "<nil>"
	}
	return u.Host + u.Path
}
