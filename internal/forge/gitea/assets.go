package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// maxRedirects matches net/http's limit for a client with no CheckRedirect.
const maxRedirects = 10

// OpenAsset implements forge.AssetReader. Go forwards Authorization to any redirect target with the same
// hostname, so each hop off the session's origin drops it here instead.
func (f *Forge) OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	base, err := url.Parse(f.base)
	if err != nil {
		return nil, fmt.Errorf("open asset: parse host: %w", err)
	}
	if !forge.SameOrigin(u, base) {
		return nil, fmt.Errorf("open asset %s: off the session's host: %w", assetLabel(u), forge.ErrUnsupported)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: build request", u.Path)
	}
	if f.token != "" {
		req.Header.Set("Authorization", "token "+f.token)
	}
	hc := *f.hc
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if !forge.SameOrigin(req.URL, base) {
			req.Header.Del("Authorization")
		}
		return nil
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, assetError(u, err)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.Body, nil
	case resp.StatusCode >= 400:
		defer func() { _ = resp.Body.Close() }()
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr)
		return nil, statusError("GET", u.Path, resp.StatusCode, apiErr.Message)
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %d", u.Path, resp.StatusCode)
	}
}

// assetError drops the transport's URL, which carries the query: signed object-storage redirects keep credentials there.
func assetError(u *url.URL, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("GET %s: %w", u.Path, err)
}

// assetLabel names u for refusals without its query string.
func assetLabel(u *url.URL) string {
	if u == nil {
		return "<nil>"
	}
	return u.Host + u.Path
}
