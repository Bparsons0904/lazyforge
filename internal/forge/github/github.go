// Package github is the forge adapter for github.com and GitHub Enterprise Server, built on net/http (ADR 0017).
// It imports domain and forge only; API JSON types never leave the package.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const (
	pageSize     = "100"        // GitHub's max per_page
	apiVersion   = "2022-11-28" // pinned so a new REST version can't change shapes under us
	etagCap      = 1000
	etagBytes    = 32 << 20
	etagMaxBody  = 1 << 20
	maxRedirects = 10 // net/http's limit for a client with no CheckRedirect
)

// Forge is safe for concurrent use; only its ETag cache changes after New.
type Forge struct {
	hc    *http.Client
	web   string // web root, as Info().URL
	api   string // REST base: api.github.com or <web>/api/v3
	token string
	info  forge.HostInfo
	etags *etagCache
}

var (
	_ forge.Forge         = (*Forge)(nil)
	_ forge.Approver      = (*Forge)(nil)
	_ forge.RunLister     = (*Forge)(nil)
	_ forge.LogReader     = (*Forge)(nil)
	_ forge.Labeler       = (*Forge)(nil)
	_ forge.AssetReader   = (*Forge)(nil)
	_ forge.ReadmeReader  = (*Forge)(nil)
	_ forge.BranchReader  = (*Forge)(nil)
	_ forge.TreeReader    = (*Forge)(nil)
	_ forge.BranchUpdater = (*Forge)(nil)
)

// New takes webURL as the web root, not the API base, and reads the token's user up front,
// so a bad token fails here with ErrUnauthorized.
func New(ctx context.Context, webURL, token string, hc *http.Client) (*Forge, error) {
	f, err := newForge(webURL, token, hc)
	if err != nil {
		return nil, err
	}
	var u user
	if err := f.call(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", f.web, err)
	}
	var version string
	if !isDotcom(f.web) {
		var m meta
		if err := f.call(ctx, http.MethodGet, "/meta", nil, &m); err != nil {
			return nil, fmt.Errorf("connect to %s: %w", f.web, err)
		}
		version = m.InstalledVersion
	}
	f.info = forge.HostInfo{Kind: forge.KindGitHub, URL: f.web, Version: version, User: u.Login, ChangeRequestTerm: "PR"}
	return f, nil
}

// Probe reads GET /meta without a token. ErrUnauthorized (a private-mode GHES) returns ("", nil):
// reachable, kind unknown until the connection test.
func Probe(ctx context.Context, webURL string, hc *http.Client) (forge.Kind, error) {
	f, err := newForge(webURL, "", hc)
	if err != nil {
		return "", err
	}
	var m meta
	err = f.call(ctx, http.MethodGet, "/meta", nil, &m)
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.Is(err, forge.ErrUnauthorized):
		return "", nil
	case errors.As(err, &se) || errors.As(err, &te) || errors.Is(err, io.EOF):
		return "", fmt.Errorf("probe %s: %w: no GitHub API", f.web, forge.ErrNotFound)
	case err != nil:
		return "", fmt.Errorf("probe %s: %w", f.web, err)
	case m.VerifiablePasswordAuthentication == nil:
		return "", fmt.Errorf("probe %s: %w: no GitHub API", f.web, forge.ErrNotFound)
	}
	return forge.KindGitHub, nil
}

type meta struct {
	VerifiablePasswordAuthentication *bool  `json:"verifiable_password_authentication"` // present on every GitHub /meta; used as the signature
	InstalledVersion                 string `json:"installed_version"`                  // GHES only
}

func newForge(webURL, token string, hc *http.Client) (*Forge, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	web := strings.TrimRight(webURL, "/")
	u, err := url.Parse(web)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("github host %q: not an absolute URL", webURL)
	}
	api := web + "/api/v3"
	if isDotcom(web) {
		api = "https://api.github.com"
	}
	return &Forge{hc: hc, web: web, api: api, token: token, etags: newETagCache(etagCap, etagBytes, etagMaxBody)}, nil
}

func isDotcom(web string) bool {
	u, err := url.Parse(web)
	return err == nil && strings.EqualFold(u.Hostname(), "github.com")
}

// Info returns what New learned at connect.
func (f *Forge) Info() forge.HostInfo { return f.info }

// Gate allows everything; GitHub has no server-side feature switches the UI needs to honor.
func (f *Forge) Gate(forge.Action) error { return nil }

func repoPath(r domain.RepoRef) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

func (f *Forge) url(path string, q url.Values) string {
	u := f.api + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// send returns the response only on a 2xx status; the caller closes its body.
// A GET carries If-None-Match when the cache holds the URL, and a 304 is answered from the cache as a 200.
func (f *Forge) send(ctx context.Context, method, rawURL string, body any) (*http.Response, error) {
	req, op, err := f.request(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	cached, haveCached := cachedEntry{}, false
	if method == http.MethodGet {
		if cached, haveCached = f.etags.get(rawURL); haveCached {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, doError(op, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && haveCached:
		_ = resp.Body.Close()
		h := resp.Header.Clone()
		h.Del("Content-Length")
		h.Set("Link", cached.link)
		return &http.Response{
			StatusCode: http.StatusOK, Header: h, ContentLength: int64(len(cached.body)),
			Body: io.NopCloser(bytes.NewReader(cached.body)),
		}, nil
	case resp.StatusCode < 300 && method == http.MethodGet && resp.Header.Get("ETag") != "":
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: read: %w", op, err)
		}
		f.etags.put(rawURL, cachedEntry{etag: resp.Header.Get("ETag"), link: resp.Header.Get("Link"), body: b})
		resp.Body = io.NopCloser(bytes.NewReader(b))
		return resp, nil
	}
	if err := checkStatus(op, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// stream skips the ETag cache so a large body is never buffered, and drops the token on any redirect
// off the API origin, since Go's own rule forwards it to subdomains and other ports.
func (f *Forge) stream(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, op, err := f.request(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(f.api)
	if err != nil {
		return nil, fmt.Errorf("%s: parse API base: %w", op, err)
	}
	resp, err := f.tokenOnlyOn(base).Do(req)
	if err != nil {
		return nil, doError(op, err)
	}
	if err := checkStatus(op, resp); err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// tokenOnlyOn returns a copy of the session client that drops Authorization on any redirect hop off base's origin.
func (f *Forge) tokenOnlyOn(base *url.URL) *http.Client {
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
	return &hc
}

// doError drops the transport's URL: after a redirect it is a signed blob URL with credentials in its query.
func doError(op string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (f *Forge) request(ctx context.Context, method, rawURL string, body any) (*http.Request, string, error) {
	op := method + " " + strings.TrimPrefix(strings.SplitN(rawURL, "?", 2)[0], f.api)
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, op, fmt.Errorf("%s: %w", op, err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, op, fmt.Errorf("%s: %w", op, err)
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, op, nil
}

// checkStatus closes the body of a non-2xx response and returns its mapped error.
func checkStatus(op string, resp *http.Response) error {
	if resp.StatusCode < 300 {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	var apiErr struct {
		Message string            `json:"message"`
		Errors  []json.RawMessage `json:"errors"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr)
	return statusError(op, resp.StatusCode, resp.Header, errorMessage(apiErr.Message, apiErr.Errors))
}

// errorMessage keeps GitHub's per-field errors because they carry the real reason on a 422 ("Validation Failed").
func errorMessage(msg string, details []json.RawMessage) string {
	parts := []string{msg}
	for _, d := range details {
		var s string
		var o struct {
			Message string `json:"message"`
		}
		switch {
		case json.Unmarshal(d, &s) == nil && s != "":
			parts = append(parts, s)
		case json.Unmarshal(d, &o) == nil && o.Message != "":
			parts = append(parts, o.Message)
		}
	}
	return strings.Join(parts, ": ")
}

func statusError(op string, code int, h http.Header, msg string) error {
	var sentinel error
	switch {
	case code == http.StatusUnauthorized:
		sentinel = forge.ErrUnauthorized
	// GitHub answers an exhausted primary or a secondary rate limit with 403 as well as 429.
	case code == http.StatusForbidden && (h.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(msg), "rate limit")):
		sentinel = forge.ErrRateLimited
	case code == http.StatusForbidden:
		sentinel = forge.ErrUnauthorized
	case code == http.StatusNotFound:
		sentinel = forge.ErrNotFound
	case code == http.StatusTooManyRequests:
		sentinel = forge.ErrRateLimited
	// The merge endpoint's 409 means the sha we sent is no longer the head; elsewhere 409 is a plain refusal.
	case code == http.StatusConflict && strings.HasSuffix(op, "/merge"):
		sentinel = forge.ErrHeadChanged
	case code == http.StatusMethodNotAllowed || code == http.StatusUnprocessableEntity || code == http.StatusConflict:
		return fmt.Errorf("%s: %w: %s", op, forge.ErrRefused, msg)
	default:
		return fmt.Errorf("%s: %d: %s", op, code, msg)
	}
	return fmt.Errorf("%s: %w", op, sentinel)
}

// call discards the body when out is nil.
func (f *Forge) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := f.send(ctx, method, f.url(path, nil), body)
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
	return walk(ctx, f, path, q, func(r io.Reader) ([]T, error) {
		var items []T
		err := json.NewDecoder(r).Decode(&items)
		return items, err
	})
}

// walk follows Link rel="next" because a short page is not the end.
func walk[T any](ctx context.Context, f *Forge, path string, q url.Values, decode func(io.Reader) ([]T, error)) ([]T, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", pageSize)
	var all []T
	for next := f.url(path, q); next != ""; {
		resp, err := f.send(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		items, err := decode(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("GET %s: decode: %w", path, err)
		}
		all = append(all, items...)
		if next, err = f.nextLink(resp.Header.Get("Link")); err != nil {
			return nil, fmt.Errorf("GET %s: %w", path, err)
		}
	}
	return all, nil
}

// nextLink refuses a next URL off the API origin, since following it would send the token there.
func (f *Forge) nextLink(header string) (string, error) {
	for part := range strings.SplitSeq(header, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(params, `rel="next"`) {
			continue
		}
		next := strings.Trim(strings.TrimSpace(target), "<>")
		nu, err := url.Parse(next)
		if err != nil {
			return "", fmt.Errorf("next page link: %w", err)
		}
		base, _ := url.Parse(f.api)
		if !forge.SameOrigin(nu, base) {
			return "", fmt.Errorf("next page link off the API host: %s", nu.Host)
		}
		return next, nil
	}
	return "", nil
}

type cachedEntry struct {
	etag, link string
	body       []byte
}

// etagCache lets a 304 revalidation cost no rate-limit budget; it evicts oldest first once full.
type etagCache struct {
	mu                sync.Mutex
	maxEntries        int
	maxBytes, maxBody int
	bytes             int
	entries           map[string]cachedEntry
	order             []string
}

func newETagCache(maxEntries, maxBytes, maxBody int) *etagCache {
	return &etagCache{maxEntries: maxEntries, maxBytes: maxBytes, maxBody: maxBody, entries: make(map[string]cachedEntry)}
}

func (c *etagCache) get(key string) (cachedEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

// put drops an oversized body, and any older entry for its key, rather than cache it.
func (c *etagCache) put(key string, e cachedEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.bytes -= len(old.body)
		delete(c.entries, key)
		c.order = slices.DeleteFunc(c.order, func(k string) bool { return k == key })
	}
	if len(e.body) > c.maxBody {
		return
	}
	for len(c.order) > 0 && (len(c.order) >= c.maxEntries || c.bytes+len(e.body) > c.maxBytes) {
		c.bytes -= len(c.entries[c.order[0]].body)
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	c.order = append(c.order, key)
	c.entries[key] = e
	c.bytes += len(e.body)
}
