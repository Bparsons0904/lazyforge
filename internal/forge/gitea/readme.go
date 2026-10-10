package gitea

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

// readmeEntry is one root listing item or the file response for it; a file response has Content and Encoding.
type readmeEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// GetReadme finds the root README by name, because Forgejo has no README endpoint.
func (f *Forge) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	var entries []readmeEntry
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/contents", nil, &entries); err != nil {
		return domain.Readme{}, fmt.Errorf("find README in %s: %w", r, err)
	}
	best, bestRank := -1, 0
	for i, e := range entries {
		rank := readmeRank(e.Name)
		if e.Type != "file" || rank < 0 {
			continue
		}
		if best < 0 || rank < bestRank {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return domain.Readme{}, fmt.Errorf("README in %s: %w", r, forge.ErrNotFound)
	}
	name := entries[best].Name
	var file readmeEntry
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/contents/"+url.PathEscape(name), nil, &file); err != nil {
		return domain.Readme{}, fmt.Errorf("read %s in %s: %w", name, r, err)
	}
	if file.Encoding == "" && file.Content == "" {
		return domain.Readme{}, fmt.Errorf("read %s in %s: empty response, the file may be too large for the API", name, r)
	}
	if file.Encoding != "base64" {
		return domain.Readme{}, fmt.Errorf("read %s in %s: encoding %q, want base64", name, r, file.Encoding)
	}
	// Gitea wraps base64 lines, so every whitespace byte goes before decoding.
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(file.Content), ""))
	if err != nil {
		return domain.Readme{}, fmt.Errorf("read %s in %s: decode: %w", name, r, err)
	}
	return domain.Readme{Name: name, Body: string(body)}, nil
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
