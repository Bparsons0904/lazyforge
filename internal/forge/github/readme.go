package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// readme is the part of GET /repos/{o}/{r}/readme the adapter reads.
type readme struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// GetReadme returns the default branch's README; GitHub also searches docs/ and .github/.
func (f *Forge) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	var rd readme
	if err := f.call(ctx, http.MethodGet, repoPath(r)+"/readme", nil, &rd); err != nil {
		return domain.Readme{}, fmt.Errorf("README of %s: %w", r, err)
	}
	if rd.Encoding != "base64" {
		return domain.Readme{}, fmt.Errorf("README of %s: encoding %q, want base64", r, rd.Encoding)
	}
	// GitHub wraps base64 lines, so every whitespace byte goes before decoding.
	body, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(rd.Content), ""))
	if err != nil {
		return domain.Readme{}, fmt.Errorf("README of %s: decode: %w", r, err)
	}
	return domain.Readme{Name: rd.Name, Body: string(body)}, nil
}
