package github_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const dispatchPath = "/api/v3/repos/cli/cli/actions/workflows/renovate.yml/dispatches"

func TestDispatchWorkflowPostsRefAndInputs(t *testing.T) {
	f, s := newForge(t)
	if err := f.DispatchWorkflow(t.Context(), cli, "renovate.yml", "main", map[string]string{"repo": "cli/cli"}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != dispatchPath || reqs[0].Body != `{"ref":"main","inputs":{"repo":"cli/cli"}}` {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestDispatchWorkflowSendsGivenRef(t *testing.T) {
	f, s := newForge(t)
	if err := f.DispatchWorkflow(t.Context(), cli, "renovate.yml", "develop", nil); err != nil {
		t.Fatal(err)
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].Body != `{"ref":"develop"}` {
		t.Fatalf("requests = %+v, want one body {\"ref\":\"develop\"}", reqs)
	}
}

func TestDispatchWorkflowOmitsEmptyInputs(t *testing.T) {
	f, s := newForge(t)
	if err := f.DispatchWorkflow(t.Context(), cli, "renovate.yml", "main", nil); err != nil {
		t.Fatal(err)
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].Body != `{"ref":"main"}` {
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
		{"404 missing repo or workflow", 404, `{"message":"Not Found"}`, forge.ErrNotFound, "not found"},
		{"422 no workflow_dispatch", 422, `{"message":"Workflow does not have 'workflow_dispatch' trigger"}`, nil, "workflow_dispatch"},
		{"403 token lacks Actions write", 403, `{"message":"Resource not accessible by integration"}`, forge.ErrUnauthorized, "unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stub(t, "POST /api/v3/repos/{o}/{r}/actions/workflows/{w}/dispatches", func(w http.ResponseWriter, _ *http.Request) {
				writeRaw(w, tt.status, []byte(tt.body))
			})
			err := f.DispatchWorkflow(t.Context(), cli, "renovate.yml", "main", nil)
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
