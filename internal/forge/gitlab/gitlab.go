// Package gitlab is the forge adapter for gitlab.com and self-managed GitLab, built on net/http (ADR 0018).
// It imports domain and forge only; API JSON types never leave the package.
package gitlab

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
	"strconv"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const (
	pageSize      = "100" // GitLab's max per_page
	maxRetryAfter = 60 * time.Second
	maxRedirects  = 10          // net/http's limit for a client with no CheckRedirect
	defaultWait   = time.Second // when a 429 carries no usable Retry-After
)

// Forge is safe for concurrent use; nothing in it changes after New.
type Forge struct {
	hc    *http.Client
	web   string // web root, as Info().URL
	api   string // <web>/api/v4
	token string
	info  forge.HostInfo
}

var (
	_ forge.Forge              = (*Forge)(nil)
	_ forge.Approver           = (*Forge)(nil)
	_ forge.RunLister          = (*Forge)(nil)
	_ forge.LogReader          = (*Forge)(nil)
	_ forge.Labeler            = (*Forge)(nil)
	_ forge.WorkflowDispatcher = (*Forge)(nil)
)

// retryWait is the pause before the single 429 retry; it returns ctx.Err() if ctx ends first.
var retryWait = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

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
	var m metadata
	if err := f.call(ctx, http.MethodGet, "/metadata", nil, &m); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", f.web, err)
	}
	f.info = forge.HostInfo{Kind: forge.KindGitLab, URL: f.web, Version: m.Version, User: u.Username, ChangeRequestTerm: "MR"}
	return f, nil
}

// Probe reads GET /metadata without a token. GitLab answers it 401 {"message":"401 Unauthorized"}, which still
// identifies the host; any other answer that isn't a version is ErrNotFound.
func Probe(ctx context.Context, webURL string, hc *http.Client) (forge.Kind, error) {
	f, err := newForge(webURL, "", hc)
	if err != nil {
		return "", err
	}
	req, op, err := f.request(ctx, http.MethodGet, f.url("/metadata", nil), nil)
	if err != nil {
		return "", err
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("probe %s: %w", f.web, doError(op, err))
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Message *string `json:"message"`
		Version *string `json:"version"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	switch {
	case decodeErr != nil:
	case resp.StatusCode == http.StatusUnauthorized && body.Message != nil && *body.Message == "401 Unauthorized":
		return forge.KindGitLab, nil
	case resp.StatusCode == http.StatusOK && body.Version != nil:
		return forge.KindGitLab, nil
	}
	return "", fmt.Errorf("probe %s: %w: no GitLab API", f.web, forge.ErrNotFound)
}

type metadata struct {
	Version string `json:"version"`
}

func newForge(webURL, token string, hc *http.Client) (*Forge, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	web := strings.TrimRight(webURL, "/")
	u, err := url.Parse(web)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("gitlab host %q: not an absolute URL", webURL)
	}
	return &Forge{hc: tokenOnlyOn(hc, u), web: web, api: web + "/api/v4", token: token}, nil
}

// tokenOnlyOn returns a copy of hc that drops the token on any redirect hop off base's origin. Go forwards a
// custom header like PRIVATE-TOKEN to every hop, and strips Authorization only across domains.
func tokenOnlyOn(hc *http.Client, base *url.URL) *http.Client {
	c := *hc
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if !forge.SameOrigin(req.URL, base) {
			req.Header.Del("PRIVATE-TOKEN")
			req.Header.Del("Authorization")
		}
		return nil
	}
	return &c
}

// Info returns what New learned at connect.
func (f *Forge) Info() forge.HostInfo { return f.info }

// Gate allows everything; GitLab has no server-side feature switches the UI needs to honor.
func (f *Forge) Gate(forge.Action) error { return nil }

// projectPath addresses a project by its encoded full path, so a subgroup's slashes travel as %2F.
// url.Parse keeps the escaping in RawPath, and net/http sends RawPath on the request line.
func projectPath(r domain.RepoRef) string {
	return "/projects/" + url.PathEscape(r.String())
}

func (f *Forge) url(path string, q url.Values) string {
	u := f.api + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// send returns the response only on a 2xx status; the caller closes its body.
// A 429 is retried once after Retry-After (capped at a minute); a second 429 is ErrRateLimited.
func (f *Forge) send(ctx context.Context, method, rawURL string, body any) (*http.Response, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", method, rawURL, err)
		}
		payload = b
	}
	for attempt := 0; ; attempt++ {
		req, op, err := f.request(ctx, method, rawURL, payload)
		if err != nil {
			return nil, err
		}
		resp, err := f.hc.Do(req)
		if err != nil {
			return nil, doError(op, err)
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			_ = resp.Body.Close()
			if err := retryWait(ctx, retryAfter(resp.Header.Get("Retry-After"), time.Now())); err != nil {
				return nil, fmt.Errorf("%s: %w", op, err)
			}
			continue
		}
		if err := checkStatus(op, resp); err != nil {
			return nil, err
		}
		return resp, nil
	}
}

// retryAfter reads delay-seconds or an HTTP date.
func retryAfter(h string, now time.Time) time.Duration {
	d := defaultWait
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s >= 0 {
		d = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(h); err == nil {
		d = max(t.Sub(now), 0)
	}
	return min(d, maxRetryAfter)
}

// doError drops the transport's URL, as the other adapters do, so a redirect target never reaches the UI.
func doError(op string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (f *Forge) request(ctx context.Context, method, rawURL string, payload []byte) (*http.Request, string, error) {
	op := method + " " + strings.TrimPrefix(strings.SplitN(rawURL, "?", 2)[0], f.api)
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return nil, op, fmt.Errorf("%s: %w", op, err)
	}
	if f.token != "" {
		req.Header.Set("PRIVATE-TOKEN", f.token)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
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
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr)
	msg := errorMessage(apiErr.Message)
	if msg == "" {
		msg = apiErr.Error
	}
	return statusError(op, resp.StatusCode, msg)
}

// errorMessage reads GitLab's message, which is a string or a {field: [errors]} object on validation failures.
func errorMessage(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var fields map[string][]string
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for field, errs := range fields {
		parts = append(parts, field+" "+strings.Join(errs, ", "))
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}

func statusError(op string, code int, msg string) error {
	action := strings.HasSuffix(op, "/merge") || strings.HasSuffix(op, "/approve")
	var sentinel error
	switch {
	// GitLab answers "you may not merge or approve this" with 401, which is not a bad token.
	case code == http.StatusUnauthorized && action:
		return fmt.Errorf("%s: %w: %s", op, forge.ErrRefused, msg)
	// A 403 on rebase is branch protection or no push access, which the status bar should explain.
	case code == http.StatusForbidden && strings.HasSuffix(op, "/rebase"):
		return fmt.Errorf("%s: %w: %s", op, forge.ErrRefused, msg)
	// A 400 on pipeline creation carries why GitLab will not run it, such as rules that exclude API triggers.
	case code == http.StatusBadRequest && strings.HasSuffix(op, "/pipeline"):
		return fmt.Errorf("%s: %w: %s", op, forge.ErrRefused, msg)
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		sentinel = forge.ErrUnauthorized
	case code == http.StatusNotFound:
		sentinel = forge.ErrNotFound
	case code == http.StatusTooManyRequests:
		sentinel = forge.ErrRateLimited
	case code == http.StatusConflict && strings.HasSuffix(op, "/merge"):
		sentinel = forge.ErrHeadChanged
	case code == http.StatusMethodNotAllowed, code == http.StatusNotAcceptable,
		code == http.StatusConflict, code == http.StatusUnprocessableEntity:
		return fmt.Errorf("%s: %w: %s", op, forge.ErrRefused, msg)
	default:
		return fmt.Errorf("%s: %d: %s", op, code, msg)
	}
	return fmt.Errorf("%s: %w", op, sentinel)
}

// call discards the body when out is nil.
func (f *Forge) call(ctx context.Context, method, path string, body, out any) error {
	return f.callQuery(ctx, method, path, nil, body, out)
}

func (f *Forge) callQuery(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := f.send(ctx, method, f.url(path, q), body)
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

// list follows Link rel="next" because a short page is not the end.
func list[T any](ctx context.Context, f *Forge, path string, q url.Values) ([]T, error) {
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
		var items []T
		err = json.NewDecoder(resp.Body).Decode(&items)
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
