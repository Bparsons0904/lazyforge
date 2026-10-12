package gitlab_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const repoRebase = repoProject + "/merge_requests/7/rebase"

func TestUpdateStylesRebaseOnly(t *testing.T) {
	f, _ := newRepoForge(t, nil)
	if got, want := f.UpdateStyles(), []forge.UpdateStyle{forge.UpdateRebase}; !slices.Equal(got, want) {
		t.Errorf("UpdateStyles() = %v, want %v", got, want)
	}
}

func TestUpdateBranchRebasesAndTreats202AsSuccess(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"PUT " + repoRebase: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"rebase_in_progress":true}`))
		},
	})
	if err := f.UpdateBranch(t.Context(), repoRef, 7, forge.UpdateRebase); err != nil {
		t.Fatal(err)
	}
	reqs := api.requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPut || reqs[0].Path != repoRebase {
		t.Fatalf("requests = %+v, want exactly one PUT %s (no polling of the async rebase)", reqs, repoRebase)
	}
}

func TestUpdateBranchMergeStyleIsUnsupportedAndSendsNothing(t *testing.T) {
	for _, style := range []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateStyle("squash")} {
		t.Run(string(style), func(t *testing.T) {
			f, api := newRepoForge(t, nil)
			err := f.UpdateBranch(t.Context(), repoRef, 7, style)
			if !errors.Is(err, forge.ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if reqs := api.requests(); len(reqs) != 0 {
				t.Errorf("requests = %+v, want none", reqs)
			}
		})
	}
}

func TestUpdateBranchRefusalCarriesGitLabsReason(t *testing.T) {
	tests := []struct {
		name   string
		status int
		msg    string
	}{
		{"403 no push access", 403, "Cannot push to source branch"},
		{"403 protected branch", 403, "Source branch is protected from force push"},
		{"409 enqueue failed", 409, "Failed to enqueue the rebase operation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newRepoForge(t, map[string]http.HandlerFunc{"PUT " + repoRebase: repoStatus(tt.status, tt.msg)})
			err := f.UpdateBranch(t.Context(), repoRef, 7, forge.UpdateRebase)
			if !errors.Is(err, forge.ErrRefused) {
				t.Fatalf("err = %v, want ErrRefused", err)
			}
			if errors.Is(err, forge.ErrUnauthorized) {
				t.Errorf("err = %v; a refused rebase is not a bad token", err)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("err %q does not contain GitLab's reason %q", err, tt.msg)
			}
		})
	}
}

func TestUpdateBranchMissingMRIsNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{"PUT " + repoRebase: repoStatus(404, "404 Not found")})
	err := f.UpdateBranch(t.Context(), repoRef, 7, forge.UpdateRebase)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
