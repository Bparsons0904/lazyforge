package github_test

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestUpdateStylesMergeOnly(t *testing.T) {
	f, _ := newForge(t)
	if got, want := f.UpdateStyles(), []forge.UpdateStyle{forge.UpdateMerge}; !slices.Equal(got, want) {
		t.Errorf("UpdateStyles() = %v, want %v", got, want)
	}
}

func TestUpdateBranchPutsUpdateBranch(t *testing.T) {
	f, s := newForge(t)
	if err := f.UpdateBranch(t.Context(), cli, openPR, forge.UpdateMerge); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	want := "/api/v3/repos/cli/cli/pulls/" + strconv.Itoa(openPR) + "/update-branch"
	if len(reqs) != 1 || reqs[0].Method != http.MethodPut || reqs[0].Path != want || reqs[0].Body != "" {
		t.Fatalf("requests = %+v, want one PUT %s with no body", reqs, want)
	}
}

func TestUpdateBranchUnsupportedStyleSendsNothing(t *testing.T) {
	for _, style := range []forge.UpdateStyle{forge.UpdateRebase, forge.UpdateStyle("squash")} {
		t.Run(string(style), func(t *testing.T) {
			f, s := newForge(t)
			err := f.UpdateBranch(t.Context(), cli, openPR, style)
			if !errors.Is(err, forge.ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if reqs := s.requests(); len(reqs) != 0 {
				t.Errorf("requests = %+v, want none", reqs)
			}
		})
	}
}

func TestUpdateBranchStatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		sentinel error
		contains string
	}{
		{"422 conflict", 422, `{"message":"merge conflict between base and head"}`, forge.ErrRefused, "merge conflict between base and head"},
		{"403", 403, `{"message":"Resource not accessible by integration"}`, forge.ErrUnauthorized, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stub(t, "PUT /api/v3/repos/cli/cli/pulls/1/update-branch", func(w http.ResponseWriter, _ *http.Request) {
				writeRaw(w, tt.status, []byte(tt.body))
			})
			err := f.UpdateBranch(t.Context(), cli, 1, forge.UpdateMerge)
			if !errors.Is(err, tt.sentinel) {
				t.Fatalf("err = %v, want %v", err, tt.sentinel)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err %q does not contain %q", err, tt.contains)
			}
		})
	}
}
