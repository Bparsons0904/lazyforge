// Package gitea is the forge adapter for Gitea and Forgejo, built on net/http (ADR 0005).
// It imports domain and forge only; API JSON types never leave the package.
package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const pageSize = "50" // the instance's max_response_items (GET /settings/api); larger limits are clamped

var errGiteaActions = errors.New("actions runs and logs aren't supported on Gitea yet")

// Forge talks to one Gitea or Forgejo host; it is immutable after New and safe for concurrent use.
type Forge struct {
	hc    *http.Client
	base  string
	token string
	info  forge.HostInfo
}

var (
	_ forge.Forge     = (*Forge)(nil)
	_ forge.Approver  = (*Forge)(nil)
	_ forge.RunLister = (*Forge)(nil)
	_ forge.LogReader = (*Forge)(nil)
)

// New connects to a Gitea or Forgejo host; baseURL is the web root (no /api/v1).
// It reads the server version and the token's user up front, so a bad token fails here with ErrUnauthorized.
func New(ctx context.Context, baseURL, token string, hc *http.Client) (*Forge, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	f := &Forge{hc: hc, base: strings.TrimRight(baseURL, "/"), token: token}
	var v struct {
		Version string `json:"version"`
	}
	if err := f.call(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", f.base, err)
	}
	var u user
	if err := f.call(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", f.base, err)
	}
	kind := forge.KindGitea
	if strings.Contains(v.Version, "+gitea-") {
		kind = forge.KindForgejo
	}
	f.info = forge.HostInfo{Kind: kind, URL: f.base, Version: v.Version, User: u.Login, ChangeRequestTerm: "PR"}
	return f, nil
}

// Info returns what New learned at connect.
func (f *Forge) Info() forge.HostInfo { return f.info }

// Gate blocks runs and logs on Gitea, whose Actions JSON differs from Forgejo's and has no recorded fixture.
// ponytail: open it up once a Gitea runs/jobs fixture is recorded.
func (f *Forge) Gate(a forge.Action) error {
	if f.info.Kind == forge.KindGitea && (a == forge.ActRuns || a == forge.ActLogs) {
		return errGiteaActions
	}
	return nil
}

func repoPath(r domain.RepoRef) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

// send returns the response only on a 2xx status; the caller closes its body.
func (f *Forge) send(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", method, path, err)
		}
		rd = bytes.NewReader(b)
	}
	u := f.base + "/api/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "token "+f.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	var apiErr struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr)
	return nil, statusError(method, path, resp.StatusCode, apiErr.Message)
}

func statusError(method, path string, code int, msg string) error {
	var sentinel error
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		sentinel = forge.ErrUnauthorized
	case code == http.StatusNotFound:
		sentinel = forge.ErrNotFound
	case code == http.StatusTooManyRequests:
		sentinel = forge.ErrRateLimited
	// Forgejo answers 409 for merge conflicts and rejected pushes too; only this message means a stale head.
	case code == http.StatusConflict && msg == "head out of date":
		sentinel = forge.ErrHeadChanged
	default:
		return fmt.Errorf("%s %s: %d: %s", method, path, code, msg)
	}
	return fmt.Errorf("%s %s: %w", method, path, sentinel)
}

// call decodes a JSON response into out; a nil out discards the body.
func (f *Forge) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := f.send(ctx, method, path, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, path, err)
	}
	return nil
}

func list[T any](ctx context.Context, f *Forge, path string, q url.Values) ([]T, error) {
	return walk(ctx, f, path, q, func(r io.Reader) ([]T, int, error) {
		var items []T
		err := json.NewDecoder(r).Decode(&items)
		return items, -1, err
	})
}

// walk GETs every page; decode returns a page's items and the body's total, or -1 when the body has none.
// It stops at the X-Total-Count (else body) total or an empty page, never on a short page, since servers clamp limit.
func walk[T any](ctx context.Context, f *Forge, path string, q url.Values, decode func(io.Reader) ([]T, int, error)) ([]T, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("limit", pageSize)
	var all []T
	for p := 1; ; p++ {
		q.Set("page", strconv.Itoa(p))
		resp, err := f.send(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return nil, err
		}
		items, total, err := decode(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("GET %s: decode: %w", path, err)
		}
		if n, err := strconv.Atoi(resp.Header.Get("X-Total-Count")); err == nil {
			total = n
		}
		all = append(all, items...)
		if len(items) == 0 || (total >= 0 && len(all) >= total) {
			return all, nil
		}
	}
}
