package gitlab_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const pipelinePath = rtProject + "/pipeline"

// dispatcher asserts at run time, not compile time, so a missing capability fails one test instead of the package build.
func dispatcher(t *testing.T, f forge.Forge) forge.WorkflowDispatcher {
	t.Helper()
	wd, ok := f.(forge.WorkflowDispatcher)
	if !ok {
		t.Fatal("gitlab.Forge is not a forge.WorkflowDispatcher")
	}
	return wd
}

func createdPipeline(t *testing.T) func(w http.ResponseWriter, r *http.Request, path, body string) {
	created := rtFixture(t, "pipeline_created.json")
	return func(w http.ResponseWriter, _ *http.Request, path, _ string) {
		if path != pipelinePath {
			rtNotFound(w)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(created)
	}
}

func TestDispatchWorkflowStartsPipelineWithVariables(t *testing.T) {
	f, s := newStub(t, createdPipeline(t))
	err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main",
		map[string]string{"RENOVATE_AUTODISCOVER_FILTER": "group/app"})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPost, "/pipeline")
	if len(reqs) != 1 {
		t.Fatalf("got %d POSTs to /pipeline, want 1: %+v", len(reqs), s.requests())
	}
	if !strings.HasSuffix(reqs[0].URI, "/api/v4"+pipelinePath) {
		t.Errorf("URI %q, want the escaped project path then /pipeline", reqs[0].URI)
	}
	const want = `{"ref":"main","variables":[{"key":"RENOVATE_AUTODISCOVER_FILTER","value":"group/app","variable_type":"env_var"}]}`
	if reqs[0].Body != want {
		t.Errorf("body %s, want %s", reqs[0].Body, want)
	}
}

func TestDispatchWorkflowWithoutInputsOmitsVariables(t *testing.T) {
	for name, inputs := range map[string]map[string]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f, s := newStub(t, createdPipeline(t))
			if err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main", inputs); err != nil {
				t.Fatal(err)
			}
			reqs := s.requestsTo(http.MethodPost, "/pipeline")
			if len(reqs) != 1 {
				t.Fatalf("got %d POSTs to /pipeline, want 1", len(reqs))
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(reqs[0].Body), &got); err != nil {
				t.Fatal(err)
			}
			if _, has := got["variables"]; has {
				t.Errorf("body %s carries variables, want none", reqs[0].Body)
			}
			if got["ref"] != "main" {
				t.Errorf("ref = %v, want main", got["ref"])
			}
		})
	}
}

func TestDispatchWorkflowSendsEveryInputSortedByKey(t *testing.T) {
	f, s := newStub(t, createdPipeline(t))
	err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main",
		map[string]string{"ZED": "last", "ALPHA": "first", "MID": "a b/c"})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPost, "/pipeline")
	if len(reqs) != 1 {
		t.Fatalf("got %d POSTs to /pipeline, want 1", len(reqs))
	}
	const want = `{"ref":"main","variables":[` +
		`{"key":"ALPHA","value":"first","variable_type":"env_var"},` +
		`{"key":"MID","value":"a b/c","variable_type":"env_var"},` +
		`{"key":"ZED","value":"last","variable_type":"env_var"}]}`
	if reqs[0].Body != want {
		t.Errorf("body %s, want %s", reqs[0].Body, want)
	}
}

func TestDispatchWorkflowIgnoresWorkflowArgument(t *testing.T) {
	f, s := newStub(t, createdPipeline(t))
	if err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "renovate.yml", "main", nil); err != nil {
		t.Fatalf("a non-empty workflow must not be an error: %v", err)
	}
	reqs := s.requestsTo(http.MethodPost, "/pipeline")
	if len(reqs) != 1 {
		t.Fatalf("got %d POSTs to /pipeline, want 1", len(reqs))
	}
	if strings.Contains(reqs[0].Body, "renovate.yml") || strings.Contains(reqs[0].URI, "renovate.yml") {
		t.Errorf("the workflow leaked into the request: %s %s", reqs[0].URI, reqs[0].Body)
	}
}

func TestDispatchWorkflowRefusedPipelineCarriesGitLabsReason(t *testing.T) {
	refusal := rtFixture(t, "pipeline_400.json")
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(refusal)
	})
	err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main", nil)
	if !errors.Is(err, forge.ErrRefused) {
		t.Fatalf("got %v, want ErrRefused", err)
	}
	if !strings.Contains(err.Error(), "Pipeline will not run for the selected trigger") {
		t.Errorf("error %q lacks GitLab's message", err)
	}
}

func TestDispatchWorkflowMissingProjectIsNotFound(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { rtNotFound(w) })
	err := dispatcher(t, f).DispatchWorkflow(t.Context(), domain.RepoRef{Owner: "group", Name: "gone"}, "", "main", nil)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestBadRequestOnOtherEndpointsIsNotRefused(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"400 Bad request"}`))
	})
	_, err := f.ListRuns(t.Context(), rtRepo, forge.RunFilter{})
	if err == nil {
		t.Fatal("want an error from a 400")
	}
	if errors.Is(err, forge.ErrRefused) {
		t.Errorf("a 400 outside /pipeline must keep its existing mapping, got ErrRefused: %v", err)
	}
}
