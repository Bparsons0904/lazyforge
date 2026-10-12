package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestSeamPipelineState(t *testing.T) {
	want := map[string]domain.CIState{
		"failed": domain.CIFail, "canceled": domain.CICancelled, "canceling": domain.CICancelled,
		"running": domain.CIRunning, "success": domain.CIPass, "skipped": domain.CISkipped,
		"created": domain.CIPending, "waiting_for_resource": domain.CIPending, "preparing": domain.CIPending,
		"waiting_for_callback": domain.CIPending, "pending": domain.CIPending, "scheduled": domain.CIPending,
		"manual": domain.CIPending, "some_future_status": domain.CIPending,
	}
	for status, w := range want {
		if got := pipelineState(status); got != w {
			t.Errorf("%s: got %v, want %v", status, got, w)
		}
	}
	if changeRequest(mergeRequest{}, nil).CI != domain.CINone {
		t.Error("no pipeline must be CINone")
	}
}

func TestSeamPipelineFor(t *testing.T) {
	pipes := []pipeline{{ID: 9, SHA: "b", Ref: "feat"}, {ID: 8, SHA: "a", Ref: "feat"}, {ID: 7, SHA: "a", Ref: "feat"}}
	if p := pipelineFor(pipes, mergeRequest{IID: 1, SHA: "a", SourceBranch: "feat"}); p == nil || p.ID != 8 {
		t.Errorf("got %v, want the newest pipeline for a (8)", p)
	}
	if p := pipelineFor(pipes, mergeRequest{IID: 1, SHA: "c", SourceBranch: "feat"}); p != nil {
		t.Errorf("got %v for an unmatched sha, want nil", p)
	}
	if p := pipelineFor([]pipeline{{ID: 1}}, mergeRequest{}); p != nil {
		t.Errorf("got %v for an empty sha, want nil", p)
	}
}

func TestSeamAccess(t *testing.T) {
	withLevels := func(proj, group int) project {
		var p project
		p.Permissions = &struct {
			ProjectAccess *accessGrant `json:"project_access"`
			GroupAccess   *accessGrant `json:"group_access"`
		}{}
		if proj > 0 {
			p.Permissions.ProjectAccess = &accessGrant{AccessLevel: proj}
		}
		if group > 0 {
			p.Permissions.GroupAccess = &accessGrant{AccessLevel: group}
		}
		return p
	}
	archived := withLevels(50, 0)
	archived.Archived = true
	tests := []struct {
		name     string
		p        project
		writable bool
		want     domain.Access
	}{
		{"owner", withLevels(50, 0), false, domain.AccessAdmin},
		{"maintainer via group beats project developer", withLevels(30, 40), false, domain.AccessAdmin},
		{"developer", withLevels(30, 0), false, domain.AccessWrite},
		{"reporter", withLevels(0, 20), false, domain.AccessRead},
		{"guest", withLevels(10, 0), false, domain.AccessRead},
		{"null permissions in the writable list", project{}, true, domain.AccessWrite},
		{"null permissions not writable", project{}, false, domain.AccessRead},
		{"levels win over the writable list", withLevels(20, 0), true, domain.AccessRead},
		{"archived is read for an owner", archived, false, domain.AccessRead},
	}
	for _, tt := range tests {
		if got := access(tt.p, tt.writable); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSeamMergeStyle(t *testing.T) {
	tests := []struct{ method, squash, want string }{
		{"merge", "always", "squash"},
		{"ff", "default_on", "squash"},
		{"merge", "default_off", "merge"},
		{"rebase_merge", "never", "rebase-merge"},
		{"ff", "", "fast-forward-only"},
	}
	for _, tt := range tests {
		if got := mergeStyle(project{MergeMethod: tt.method, SquashOption: tt.squash}); got != tt.want {
			t.Errorf("%s/%s: got %q, want %q", tt.method, tt.squash, got, tt.want)
		}
	}
}

func TestSeamStatusError(t *testing.T) {
	const merge = "PUT /projects/g%2Fr/merge_requests/1/merge"
	const approve = "POST /projects/g%2Fr/merge_requests/1/approve"
	const get = "GET /projects/g%2Fr"
	tests := []struct {
		op      string
		code    int
		want    error
		notWant error
	}{
		{merge, 401, forge.ErrRefused, forge.ErrUnauthorized},
		{approve, 401, forge.ErrRefused, forge.ErrUnauthorized},
		{get, 401, forge.ErrUnauthorized, nil},
		{get, 403, forge.ErrUnauthorized, forge.ErrRefused},
		{"HEAD /projects/g%2Fr/repository/files/a.txt", 403, forge.ErrUnauthorized, forge.ErrRefused},
		{merge, 403, forge.ErrRefused, forge.ErrUnauthorized},
		{approve, 403, forge.ErrRefused, forge.ErrUnauthorized},
		{"PUT /projects/g%2Fr/merge_requests/1/rebase", 403, forge.ErrRefused, forge.ErrUnauthorized},
		{"POST /projects/g%2Fr/merge_requests/1/notes", 403, forge.ErrRefused, forge.ErrUnauthorized},
		{"DELETE /projects/g%2Fr/issues/1", 403, forge.ErrRefused, forge.ErrUnauthorized},
		{"PUT /projects/g%2Fr/issues/1", 403, forge.ErrRefused, forge.ErrUnauthorized},
		{get, 404, forge.ErrNotFound, nil},
		{get, 429, forge.ErrRateLimited, forge.ErrUnauthorized},
		{merge, 409, forge.ErrHeadChanged, nil},
		{get, 409, forge.ErrRefused, forge.ErrHeadChanged},
		{merge, 405, forge.ErrRefused, nil},
		{merge, 406, forge.ErrRefused, nil},
		{merge, 422, forge.ErrRefused, nil},
	}
	for _, tt := range tests {
		err := statusError(tt.op, tt.code, "msg")
		if !errors.Is(err, tt.want) || (tt.notWant != nil && errors.Is(err, tt.notWant)) {
			t.Errorf("%s %d: got %v, want %v", tt.op, tt.code, err, tt.want)
		}
	}
	if got := errorMessage([]byte(`{"title":["can't be blank"]}`)); got != "title can't be blank" {
		t.Errorf("object message: got %q", got)
	}
}

func TestSeamSubgroupPathAndRateLimitRetry(t *testing.T) {
	var uris []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.RequestURI)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	var waited []time.Duration
	old := retryWait
	retryWait = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	defer func() { retryWait = old }()

	f, err := newForge(srv.URL+"/", "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.GetChangeRequest(context.Background(), domain.RepoRef{Owner: "group/sub", Name: "repo"}, 3)
	if !errors.Is(err, forge.ErrRateLimited) || errors.Is(err, forge.ErrUnauthorized) {
		t.Errorf("got %v, want ErrRateLimited", err)
	}
	if len(uris) != 2 || len(waited) != 1 {
		t.Errorf("got %d requests and %d waits, want one retry", len(uris), len(waited))
	}
	if len(uris) > 0 && !strings.HasPrefix(uris[0], "/api/v4/projects/group%2Fsub%2Frepo/merge_requests/3") {
		t.Errorf("request line %q does not carry the encoded full path", uris[0])
	}
	if got := retryAfter("600", time.Now()); got != maxRetryAfter {
		t.Errorf("Retry-After 600: waited %v, want the cap", got)
	}
}
