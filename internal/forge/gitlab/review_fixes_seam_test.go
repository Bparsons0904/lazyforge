package gitlab

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestPipelineForPrefersTheMergeRequestRefThenTheSourceBranch(t *testing.T) {
	const sha = "abc"
	mr := mergeRequest{IID: 7, SHA: sha, SourceBranch: "feat"}
	tests := []struct {
		name  string
		pipes []pipeline
		mr    mergeRequest
		want  int64 // 0 means nil
	}{
		{
			name: "merge request ref beats a newer branch pipeline",
			pipes: []pipeline{
				{ID: 30, SHA: sha, Ref: "main"},
				{ID: 29, SHA: sha, Ref: "feat"},
				{ID: 28, SHA: sha, Ref: "refs/merge-requests/7/head"},
				{ID: 27, SHA: sha, Ref: "refs/merge-requests/9/head"},
				{ID: 26, SHA: sha, Ref: "v1.0"},
			},
			mr: mr, want: 28,
		},
		{"branch pipeline when there is no merge request pipeline", []pipeline{{ID: 29, SHA: sha, Ref: "feat"}, {ID: 28, SHA: sha, Ref: "feat"}}, mr, 29},
		{"newest merge request pipeline", []pipeline{{ID: 40, SHA: sha, Ref: "refs/merge-requests/7/head"}, {ID: 35, SHA: sha, Ref: "refs/merge-requests/7/head"}}, mr, 40},
		{"another merge request's ref", []pipeline{{ID: 27, SHA: sha, Ref: "refs/merge-requests/9/head"}}, mr, 0},
		{"a merge request ref of 70 is not 7", []pipeline{{ID: 27, SHA: sha, Ref: "refs/merge-requests/70/head"}}, mr, 0},
		{"wrong ref and a tag", []pipeline{{ID: 30, SHA: sha, Ref: "main"}, {ID: 26, SHA: sha, Ref: "v1.0"}}, mr, 0},
		{"merge request ref on another sha", []pipeline{{ID: 50, SHA: "other", Ref: "refs/merge-requests/7/head"}}, mr, 0},
		{"branch pipeline on another sha", []pipeline{{ID: 50, SHA: "other", Ref: "feat"}}, mr, 0},
		{"empty sha matches nothing", []pipeline{{ID: 1, Ref: "feat"}, {ID: 2, Ref: "refs/merge-requests/7/head"}}, mergeRequest{IID: 7, SourceBranch: "feat"}, 0},
		{"no pipelines", nil, mr, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pipelineFor(tt.pipes, tt.mr)
			switch {
			case tt.want == 0 && got != nil:
				t.Errorf("pipelineFor = %+v, want nil", got)
			case tt.want != 0 && (got == nil || got.ID != tt.want):
				t.Errorf("pipelineFor = %+v, want pipeline %d", got, tt.want)
			}
		})
	}
}

func TestStatusErrorKeepsGitLabsMessageOnEverySentinel(t *testing.T) {
	const merge = "PUT /projects/g%2Fr/merge_requests/1/merge"
	const get = "GET /projects/g%2Fr"
	tests := []struct {
		op   string
		code int
		want error
	}{
		{merge, 401, forge.ErrRefused},
		{merge, 403, forge.ErrRefused},
		{get, 401, forge.ErrUnauthorized},
		{get, 403, forge.ErrUnauthorized},
		{get, 404, forge.ErrNotFound},
		{get, 429, forge.ErrRateLimited},
		{merge, 409, forge.ErrHeadChanged},
		{get, 409, forge.ErrRefused},
		{merge, 405, forge.ErrRefused},
	}
	for _, tt := range tests {
		err := statusError(tt.op, tt.code, "the reason")
		if !errors.Is(err, tt.want) {
			t.Errorf("%s %d: err %v, want %v", tt.op, tt.code, err, tt.want)
			continue
		}
		if want := tt.op + ": " + tt.want.Error() + ": the reason"; err.Error() != want {
			t.Errorf("%s %d: text %q, want %q", tt.op, tt.code, err, want)
		}
		bare := statusError(tt.op, tt.code, "")
		if want := tt.op + ": " + tt.want.Error(); bare.Error() != want || strings.HasSuffix(bare.Error(), ": ") {
			t.Errorf("%s %d with no message: text %q, want %q", tt.op, tt.code, bare, want)
		}
	}
}

func TestStatusErrorMapsA403ByMethod(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		err := statusError(method+" /projects/g%2Fr", 403, "m")
		if !errors.Is(err, forge.ErrUnauthorized) || errors.Is(err, forge.ErrRefused) {
			t.Errorf("%s 403: err %v, want ErrUnauthorized only", method, err)
		}
	}
	for _, method := range []string{"PUT", "POST", "DELETE", "PATCH"} {
		err := statusError(method+" /projects/g%2Fr/issues/1", 403, "m")
		if !errors.Is(err, forge.ErrRefused) || errors.Is(err, forge.ErrUnauthorized) {
			t.Errorf("%s 403: err %v, want ErrRefused only", method, err)
		}
	}
}

func TestLabelsByNameMapsNamesToTheListedLabelsAndKeepsUnknownOnes(t *testing.T) {
	all := []domain.Label{{ID: 1, Name: "bug", Color: "d9534f"}, {ID: 3, Name: "renovate", Color: "0033cc"}}
	got := labelsByName([]string{"renovate", "ghost", "bug"}, all)
	want := []domain.Label{{ID: 3, Name: "renovate", Color: "0033cc"}, {Name: "ghost"}, {ID: 1, Name: "bug", Color: "d9534f"}}
	if !slices.Equal(got, want) {
		t.Errorf("labelsByName = %+v, want %+v", got, want)
	}
	if got := labelsByName(nil, all); len(got) != 0 {
		t.Errorf("labelsByName(nil) = %+v, want none", got)
	}
}
