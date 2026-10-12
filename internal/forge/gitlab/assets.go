package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var (
	_ forge.AssetReader   = (*Forge)(nil)
	_ forge.AssetResolver = (*Forge)(nil)
)

// ResolveAsset maps GitLab's upload links, /uploads/<secret>/<file> under the project and
// /-/project/<id>/uploads/<secret>/<file> under the web root; every other link is ok=false.
func (f *Forge) ResolveAsset(repo domain.RepoRef, ref *url.URL) (*url.URL, bool) {
	if ref == nil || ref.IsAbs() || ref.Host != "" || ref.Opaque != "" {
		return nil, false
	}
	p := ref.EscapedPath()
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	var base string
	switch {
	case !strings.HasPrefix(p, "/"):
		return nil, false
	case len(segs) == 3 && isUpload(segs):
		base = f.web + "/" + escapeSegments(repo.Owner) + "/" + url.PathEscape(repo.Name)
	case len(segs) == 6 && segs[0] == "-" && segs[1] == "project" && isID(segs[2]) && isUpload(segs[3:]):
		base = f.web
	default:
		return nil, false
	}
	u, err := url.Parse(base + p)
	if err != nil {
		return nil, false
	}
	u.RawQuery = ref.RawQuery
	return u, true
}

// OpenAsset implements forge.AssetReader. An upload goes through the uploads API (GitLab 17.4+), because the
// web route doesn't take a token; an older server answers 404, which is ErrNotFound.
func (f *Forge) OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	web, err := url.Parse(f.web)
	if err != nil {
		return nil, fmt.Errorf("open asset: parse host: %w", err)
	}
	if !forge.SameOrigin(u, web) {
		return nil, fmt.Errorf("open asset %s: off the session's host: %w", u.Host+u.Path, forge.ErrUnsupported)
	}
	if unsafePath(u.EscapedPath()) {
		return nil, fmt.Errorf("open asset %s: dot or empty upload segment: %w", u.Host+u.Path, forge.ErrUnsupported)
	}
	target, op := u.String(), "GET "+u.Path
	if api, ok := uploadAPIURL(web, u); ok {
		target, op = api, "GET "+strings.TrimPrefix(api, f.api)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request", op)
	}
	if f.token != "" {
		req.Header.Set("PRIVATE-TOKEN", f.token)
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, doError(op, err)
	}
	if err := checkStatus(op, resp); err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// uploadAPIURL rewrites an upload's web URL under web to its uploads API URL, keeping the secret and file escaped.
// The project is the path before uploads/<secret>/<file>, minus a trailing "-" segment, or the id in /-/project/<id>/.
func uploadAPIURL(web, u *url.URL) (string, bool) {
	root := strings.TrimRight(web.EscapedPath(), "/")
	rest, ok := strings.CutPrefix(u.EscapedPath(), root+"/")
	if !ok {
		return "", false
	}
	segs := strings.Split(rest, "/")
	n := len(segs)
	if n < 4 || !isUpload(segs[n-3:]) {
		return "", false
	}
	proj := segs[:n-3]
	if proj[len(proj)-1] == "-" {
		proj = proj[:len(proj)-1]
	}
	var id string
	switch {
	case len(proj) == 3 && proj[0] == "-" && proj[1] == "project" && isID(proj[2]):
		id = proj[2]
	case len(proj) == 0 || proj[0] == "api" || proj[0] == "-":
		return "", false
	default:
		parts := make([]string, len(proj))
		for i, s := range proj {
			p, err := url.PathUnescape(s)
			if err != nil || p == "" || p == "-" {
				return "", false
			}
			parts[i] = p
		}
		id = url.PathEscape(strings.Join(parts, "/"))
	}
	return strings.TrimRight(web.String(), "/") + "/api/v4/projects/" + id + "/uploads/" + segs[n-2] + "/" + segs[n-1], true
}

// isUpload reports whether segs is uploads/<secret>/<file>, with neither part empty or a dot segment.
func isUpload(segs []string) bool {
	return len(segs) == 3 && segs[0] == "uploads" && !dotOrEmpty(segs[1]) && !dotOrEmpty(segs[2])
}

// dotOrEmpty reports whether the escaped segment is empty or, once unescaped, "." or "..", which a server
// or proxy would resolve to another path. An undecodable segment counts too.
func dotOrEmpty(seg string) bool {
	d, err := url.PathUnescape(seg)
	return err != nil || d == "" || d == "." || d == ".."
}

// unsafePath reports whether the escaped path has a "." or ".." segment, raw or percent-encoded, or ends in
// uploads/<secret>/<file> with a part empty.
func unsafePath(p string) bool {
	segs := strings.Split(p, "/")
	for _, seg := range segs {
		if seg != "" && dotOrEmpty(seg) {
			return true
		}
	}
	n := len(segs)
	return n >= 3 && segs[n-3] == "uploads" && (segs[n-2] == "" || segs[n-1] == "")
}

func isID(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
