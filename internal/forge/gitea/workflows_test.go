package gitea_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const dispatchPath = "/api/v1/repos/deadstyle/lazyforge/actions/workflows/renovate.yml/dispatches"

func TestDispatchWorkflowPostsRefAndInputs(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	if err := f.DispatchWorkflow(t.Context(), lazyforge, "renovate.yml", "main", map[string]string{"repo": "deadstyle/files"}); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != dispatchPath || reqs[0].Body != `{"ref":"main","inputs":{"repo":"deadstyle/files"}}` {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestDispatchWorkflowSendsGivenRef(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	if err := f.DispatchWorkflow(t.Context(), lazyforge, "renovate.yml", "develop", nil); err != nil {
		t.Fatal(err)
	}
	if reqs := srv.requests(); len(reqs) != 1 || reqs[0].Body != `{"ref":"develop"}` {
		t.Fatalf("requests = %+v, want one body {\"ref\":\"develop\"}", reqs)
	}
}

func TestDispatchWorkflowOmitsEmptyInputs(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	if err := f.DispatchWorkflow(t.Context(), lazyforge, "renovate.yml", "main", nil); err != nil {
		t.Fatal(err)
	}
	if reqs := srv.requests(); len(reqs) != 1 || reqs[0].Body != `{"ref":"main"}` {
		t.Fatalf("requests = %+v, want one body {\"ref\":\"main\"}", reqs)
	}
}

func TestDispatchWorkflowStatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		sentinel error
		contains string
	}{
		{"404 missing repo or workflow", 404, `{"message":"not found"}`, forge.ErrNotFound, "not found"},
		{"422 no workflow_dispatch", 422, `{"message":"workflow has no workflow_dispatch event"}`, nil, "workflow has no workflow_dispatch"},
		{"403 token lacks Actions write", 403, `{"message":"forbidden"}`, forge.ErrUnauthorized, "unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				"POST /api/v1/repos/{o}/{r}/actions/workflows/{w}/dispatches": func(w http.ResponseWriter, _ *http.Request) {
					writeRaw(w, tt.status, []byte(tt.body))
				},
			})
			err := f.DispatchWorkflow(context.Background(), lazyforge, "renovate.yml", "main", nil)
			if err == nil {
				t.Fatal("want error")
			}
			if tt.sentinel != nil && !errors.Is(err, tt.sentinel) {
				t.Errorf("err = %v, want %v", err, tt.sentinel)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err %q does not contain %q", err, tt.contains)
			}
		})
	}
}
