package gitlab

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

type pipelineVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"variable_type"`
}

// DispatchWorkflow ignores workflow: GitLab has no per-file dispatch, so it runs r's pipeline on ref. The repo input
// becomes RENOVATE_AUTODISCOVER_FILTER, the variable Renovate reads; other inputs pass as CI variables verbatim.
func (f *Forge) DispatchWorkflow(ctx context.Context, r domain.RepoRef, _, ref string, inputs map[string]string) error {
	body := struct {
		Ref       string             `json:"ref"`
		Variables []pipelineVariable `json:"variables,omitempty"`
	}{Ref: ref}
	for k, v := range inputs {
		if k == "repo" {
			k = "RENOVATE_AUTODISCOVER_FILTER"
		}
		body.Variables = append(body.Variables, pipelineVariable{Key: k, Value: v, Type: "env_var"})
	}
	slices.SortFunc(body.Variables, func(a, b pipelineVariable) int { return strings.Compare(a.Key, b.Key) })
	return f.call(ctx, http.MethodPost, projectPath(r)+"/pipeline", body, nil)
}
