package github

import (
	"context"
	"net/http"
	"net/url"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// DispatchWorkflow returns no run identifier, so the run cannot be tracked afterwards.
func (f *Forge) DispatchWorkflow(ctx context.Context, r domain.RepoRef, workflow, ref string, inputs map[string]string) error {
	body := struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs,omitempty"`
	}{ref, inputs}
	resp, err := f.send(ctx, http.MethodPost, f.url(repoPath(r)+"/actions/workflows/"+url.PathEscape(workflow)+"/dispatches", nil), body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
