package gitlab

import (
	"context"
	"net/http"
	"slices"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

type pipelineVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"variable_type"`
}

// DispatchWorkflow ignores workflow: GitLab has no per-file dispatch, so it runs r's pipeline on ref.
func (f *Forge) DispatchWorkflow(ctx context.Context, r domain.RepoRef, _, ref string, inputs map[string]string) error {
	body := struct {
		Ref       string             `json:"ref"`
		Variables []pipelineVariable `json:"variables,omitempty"`
	}{Ref: ref}
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		body.Variables = append(body.Variables, pipelineVariable{Key: k, Value: inputs[k], Type: "env_var"})
	}
	return f.call(ctx, http.MethodPost, projectPath(r)+"/pipeline", body, nil)
}
