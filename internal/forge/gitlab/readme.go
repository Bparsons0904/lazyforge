package gitlab

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var _ forge.ReadmeReader = (*Forge)(nil)

// fileBody is GET /projects/:id/repository/files/:path.
type fileBody struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// GetReadme finds the root README by name on the default branch, because GitLab has no README endpoint.
func (f *Forge) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	ref, err := f.defaultBranch(ctx, r)
	if err != nil {
		return domain.Readme{}, fmt.Errorf("find README in %s: %w", r, err)
	}
	entries, err := list[treeItem](ctx, f, projectPath(r)+"/repository/tree", url.Values{"ref": {ref}})
	if err != nil {
		return domain.Readme{}, fmt.Errorf("find README in %s: %w", r, err)
	}
	best, bestRank := "", 0
	for _, e := range entries {
		rank := readmeRank(e.Name)
		if e.Type != "blob" || rank < 0 {
			continue
		}
		if best == "" || rank < bestRank {
			best, bestRank = e.Name, rank
		}
	}
	if best == "" {
		return domain.Readme{}, fmt.Errorf("README in %s: %w", r, forge.ErrNotFound)
	}
	var file fileBody
	q := url.Values{"ref": {ref}}
	if err := f.callQuery(ctx, http.MethodGet, filePath(r, best), q, nil, &file); err != nil {
		return domain.Readme{}, fmt.Errorf("read %s in %s: %w", best, r, err)
	}
	if file.Encoding != "base64" {
		return domain.Readme{}, fmt.Errorf("read %s in %s: encoding %q, want base64", best, r, file.Encoding)
	}
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(file.Content), ""))
	if err != nil {
		return domain.Readme{}, fmt.Errorf("read %s in %s: decode: %w", best, r, err)
	}
	return domain.Readme{Name: best, Body: string(body)}, nil
}

// defaultBranch is the ref every GitLab files and tree call needs; an empty repo has none, so it is ErrNotFound.
func (f *Forge) defaultBranch(ctx context.Context, r domain.RepoRef) (string, error) {
	var p project
	if err := f.call(ctx, http.MethodGet, projectPath(r), nil, &p); err != nil {
		return "", fmt.Errorf("read %s: %w", r, err)
	}
	if p.DefaultBranch == "" {
		return "", fmt.Errorf("default branch of %s: %w", r, forge.ErrNotFound)
	}
	return p.DefaultBranch, nil
}

// readmeRank orders README candidates, lower first; -1 means the name isn't one.
func readmeRank(name string) int {
	ext, ok := strings.CutPrefix(strings.ToLower(name), "readme")
	if !ok {
		return -1
	}
	switch ext {
	case "":
		return 2
	case ".md":
		return 0
	case ".markdown":
		return 1
	}
	if strings.HasPrefix(ext, ".") {
		return 3
	}
	return -1
}

// filePath addresses a file with its whole path encoded, "/" included.
func filePath(r domain.RepoRef, path string) string {
	return projectPath(r) + "/repository/files/" + url.PathEscape(path)
}
