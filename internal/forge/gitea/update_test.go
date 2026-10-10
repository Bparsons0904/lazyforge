package gitea_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const updatePath = "/api/v1/repos/deadstyle/lazyforge/pulls/18/update"

func TestUpdateStyles(t *testing.T) {
	f, _ := newForge(t.Context(), t)
	want := []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateRebase}
	if got := f.UpdateStyles(); !slices.Equal(got, want) {
		t.Errorf("UpdateStyles() = %v, want %v", got, want)
	}
}

func TestUpdateBranchSendsStyleInQuery(t *testing.T) {
	for _, style := range []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateRebase} {
		t.Run(string(style), func(t *testing.T) {
			f, srv := newForge(t.Context(), t)
			if err := f.UpdateBranch(context.Background(), lazyforge, 18, style); err != nil {
				t.Fatal(err)
			}
			reqs := srv.requests()
			if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != updatePath || reqs[0].Query != "style="+string(style) || reqs[0].Body != "" {
				t.Fatalf("requests = %+v", reqs)
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
		{"409 conflict", 409, `{"message":"merge failed because of conflict"}`, forge.ErrRefused, "merge failed because of conflict"},
		{"403", 403, `{"message":"forbidden"}`, forge.ErrUnauthorized, ""},
		{"500 up to date", 500, `{"message":"HeadBranch of PR 5 is up to date"}`, nil, "500: HeadBranch of PR 5 is up to date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				"POST /api/v1/repos/{o}/{r}/pulls/{n}/update": func(w http.ResponseWriter, _ *http.Request) {
					writeRaw(w, tt.status, []byte(tt.body))
				},
			})
			err := f.UpdateBranch(context.Background(), lazyforge, 5, forge.UpdateMerge)
			if err == nil {
				t.Fatal("want error")
			}
			if tt.sentinel != nil && !errors.Is(err, tt.sentinel) {
				t.Errorf("err = %v, want %v", err, tt.sentinel)
			}
			if tt.sentinel == nil && (errors.Is(err, forge.ErrRefused) || errors.Is(err, forge.ErrUnauthorized)) {
				t.Errorf("err = %v, want no sentinel", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err %q does not contain %q", err, tt.contains)
			}
		})
	}
}

func TestUpdateBranchUnsupportedStyleSendsNothing(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	err := f.UpdateBranch(context.Background(), lazyforge, 18, forge.UpdateStyle("squash"))
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if reqs := srv.requests(); len(reqs) != 0 {
		t.Errorf("requests = %+v, want none", reqs)
	}
}
