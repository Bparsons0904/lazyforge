package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var _ forge.TreeReader = (*Forge)(nil)

// treeItem is one item of GET /projects/:id/repository/tree.
type treeItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

// ListTree leaves Size zero, because GitLab's listing has no sizes; ReadFile enforces the cap instead.
func (f *Forge) ListTree(ctx context.Context, r domain.RepoRef, ref, dir string) ([]domain.TreeEntry, error) {
	if ref == "" {
		def, err := f.defaultBranch(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("list %q in %s: %w", dir, r, err)
		}
		ref = def
	}
	q := url.Values{"ref": {ref}}
	if dir != "" {
		q.Set("path", dir)
	}
	items, err := list[treeItem](ctx, f, projectPath(r)+"/repository/tree", q)
	if err != nil {
		return nil, fmt.Errorf("list %q at %s in %s: %w", dir, ref, r, err)
	}
	out := make([]domain.TreeEntry, len(items))
	for i, it := range items {
		t := entryType(it)
		out[i] = domain.TreeEntry{Name: it.Name, Path: it.Path, Type: t, WebURL: f.entryURL(r, ref, it.Path, t)}
	}
	return out, nil
}

// ReadFile reads the raw bytes, because the JSON form transcodes non-UTF-8 text and would hide binaries.
// A file over forge.MaxReadSize is not downloaded: HEAD's size header answers ErrTooLarge first.
func (f *Forge) ReadFile(ctx context.Context, r domain.RepoRef, ref, path string) ([]byte, error) {
	if ref == "" {
		def, err := f.defaultBranch(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("read %s in %s: %w", path, r, err)
		}
		ref = def
	}
	u := f.url(filePath(r, path), url.Values{"ref": {ref}})
	head, err := f.send(ctx, http.MethodHead, u, nil)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s in %s: %w", path, ref, r, err)
	}
	_ = head.Body.Close()
	if size, err := strconv.ParseInt(head.Header.Get("X-Gitlab-Size"), 10, 64); err == nil && size > forge.MaxReadSize {
		return nil, fmt.Errorf("read %s at %s in %s: %d bytes: %w", path, ref, r, size, forge.ErrTooLarge)
	}
	resp, err := f.send(ctx, http.MethodGet, f.url(filePath(r, path)+"/raw", url.Values{"ref": {ref}}), nil)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s in %s: %w", path, ref, r, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, forge.MaxReadSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s at %s in %s: %w", path, ref, r, err)
	}
	return b, nil
}

func entryType(it treeItem) domain.EntryType {
	switch {
	case it.Type == "tree":
		return domain.EntryDir
	case it.Type == "commit" || it.Mode == "160000":
		return domain.EntrySubmodule
	case it.Mode == "120000":
		return domain.EntrySymlink
	}
	return domain.EntryFile
}

func (f *Forge) entryURL(r domain.RepoRef, ref, path string, t domain.EntryType) string {
	kind := "blob"
	if t == domain.EntryDir {
		kind = "tree"
	}
	return f.web + "/" + r.String() + "/-/" + kind + "/" + escapeSegments(ref) + "/" + escapeSegments(path)
}

// escapeSegments escapes each segment so slashes in a ref or path stay separators in a web URL.
func escapeSegments(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
