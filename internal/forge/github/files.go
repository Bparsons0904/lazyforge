package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// treeItem is one entry of GET /repos/{o}/{r}/contents/{dir}.
type treeItem struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Type        string  `json:"type"`
	Size        int64   `json:"size"`
	HTMLURL     string  `json:"html_url"`
	DownloadURL *string `json:"download_url"`
}

// fileBody is GET /repos/{o}/{r}/contents/{path} for a file.
type fileBody struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// ListTree implements forge.TreeReader; GitHub lists at most 1,000 entries per directory.
func (f *Forge) ListTree(ctx context.Context, r domain.RepoRef, dir string) ([]domain.TreeEntry, error) {
	p := repoPath(r) + "/contents"
	if dir != "" {
		p += "/" + escapePath(dir)
	}
	var items []treeItem
	if err := f.call(ctx, http.MethodGet, p, nil, &items); err != nil {
		return nil, fmt.Errorf("list %q in %s: %w", dir, r, err)
	}
	out := make([]domain.TreeEntry, len(items))
	for i, it := range items {
		out[i] = domain.TreeEntry{Name: it.Name, Path: it.Path, Type: entryType(it), Size: it.Size, WebURL: it.HTMLURL}
	}
	return out, nil
}

// ReadFile implements forge.TreeReader; an empty file is empty bytes, while any other encoding is an error.
func (f *Forge) ReadFile(ctx context.Context, r domain.RepoRef, path string) ([]byte, error) {
	var file fileBody
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/contents/"+escapePath(path), nil, &file); err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", path, r, err)
	}
	if file.Encoding != "base64" {
		return nil, fmt.Errorf("read %s in %s: encoding %q, want base64", path, r, file.Encoding)
	}
	// GitHub wraps base64 lines, so every whitespace byte goes before decoding.
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(file.Content), ""))
	if err != nil {
		return nil, fmt.Errorf("read %s in %s: decode: %w", path, r, err)
	}
	return body, nil
}

// entryType maps GitHub's listing type to a domain entry type. GitHub lists a submodule as a file with no download URL.
func entryType(it treeItem) domain.EntryType {
	switch it.Type {
	case "dir":
		return domain.EntryDir
	case "symlink":
		return domain.EntrySymlink
	case "submodule":
		return domain.EntrySubmodule
	case "file":
		if it.DownloadURL == nil {
			return domain.EntrySubmodule
		}
	}
	return domain.EntryFile
}
