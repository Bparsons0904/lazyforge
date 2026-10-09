package gitea_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

var (
	_ forge.Forge     = (*gitea.Forge)(nil)
	_ forge.Approver  = (*gitea.Forge)(nil)
	_ forge.RunLister = (*gitea.Forge)(nil)
	_ forge.LogReader = (*gitea.Forge)(nil)
)

const openHead = "5f5b7243858f450342de15babccedd52a632fe43"

var (
	lazyforge = domain.RepoRef{Owner: "deadstyle", Name: "lazyforge"}
	waugzee   = domain.RepoRef{Owner: "deadstyle", Name: "waugzee"}
)

func newForge(ctx context.Context, t *testing.T) (*gitea.Forge, *server) {
	t.Helper()
	srv := newServer(t)
	f, err := gitea.New(ctx, srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	srv.reset()
	return f, srv
}

func TestContract(t *testing.T) {
	forgetest.RunContract(t, func(t *testing.T) (forge.Forge, forgetest.Fixture) {
		f, _ := newForge(t.Context(), t)
		return f, forgetest.Fixture{Repo: lazyforge, OpenCR: 18, OpenCRHead: openHead, OpenIssue: 17, Missing: 9999, Asset: "/attachments/contract"}
	})
}

// stub serves the version and user fixtures plus the given extra handlers; any other path is a 404.
func stub(t *testing.T, version string, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, obj{"version": version})
	})
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "user.json"))
	})
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func stubForge(t *testing.T, routes map[string]http.HandlerFunc) *gitea.Forge {
	t.Helper()
	srv := stub(t, "16.0.5+gitea-1.22.0", routes)
	f, err := gitea.New(context.Background(), srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNew(t *testing.T) {
	srv := newServer(t)
	f, err := gitea.New(context.Background(), srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	got := f.Info()
	want := forge.HostInfo{Kind: forge.KindForgejo, URL: srv.URL, Version: "16.0.5+gitea-1.22.0", User: "deadstyle", ChangeRequestTerm: "PR"}
	if got != want {
		t.Errorf("Info() = %+v, want %+v", got, want)
	}
	reqs := srv.requests()
	if len(reqs) != 2 || reqs[0].Path != "/api/v1/version" || reqs[1].Path != "/api/v1/user" {
		t.Errorf("connect requests = %+v, want GET /version then GET /user", reqs)
	}
}

func TestNewTrailingSlashAndNilClient(t *testing.T) {
	srv := newServer(t)
	f, err := gitea.New(context.Background(), srv.URL+"/", testToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ListRepos(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range srv.requests() {
		if strings.Contains(r.Path, "//") {
			t.Errorf("request path %q has a doubled slash", r.Path)
		}
	}
}

func TestInfoKindAndGate(t *testing.T) {
	const noRunsReason = "actions runs and logs aren't supported on Gitea yet"
	actions := []forge.Action{forge.ActMerge, forge.ActApprove, forge.ActClose, forge.ActComment, forge.ActEditIssue, forge.ActRuns, forge.ActLogs}
	tests := []struct {
		version string
		kind    forge.Kind
		gated   []forge.Action
	}{
		{"16.0.5+gitea-1.22.0", forge.KindForgejo, nil},
		{"1.22.0", forge.KindGitea, []forge.Action{forge.ActRuns, forge.ActLogs}},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			srv := stub(t, tt.version, nil)
			f, err := gitea.New(context.Background(), srv.URL, testToken, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Info(); got.Kind != tt.kind || got.Version != tt.version || got.User != "deadstyle" || got.ChangeRequestTerm != "PR" {
				t.Errorf("Info() = %+v", got)
			}
			for _, a := range actions {
				err := f.Gate(a)
				if slices.Contains(tt.gated, a) {
					if err == nil || !strings.Contains(err.Error(), noRunsReason) {
						t.Errorf("Gate(%d) = %v, want %q", a, err, noRunsReason)
					}
				} else if err != nil {
					t.Errorf("Gate(%d) = %v, want nil", a, err)
				}
			}
		})
	}
}

func TestNewUnauthorized(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, 200, obj{"version": "1.22.0"})
			})
			mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
				writeRaw(w, status, readFixture(t, "error_401.json"))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			_, err := gitea.New(context.Background(), srv.URL, testToken, srv.Client())
			if !errors.Is(err, forge.ErrUnauthorized) {
				t.Errorf("err = %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestStatusMapping(t *testing.T) {
	const headSHA = "abc"
	tests := []struct {
		name     string
		status   int
		body     string
		sentinel error
		contains string
	}{
		{"401", 401, `{"message":"token is required"}`, forge.ErrUnauthorized, ""},
		{"403", 403, `{"message":"forbidden"}`, forge.ErrUnauthorized, ""},
		{"404", 404, `{"message":"The target couldn't be found."}`, forge.ErrNotFound, ""},
		{"429", 429, `{"message":"slow down"}`, forge.ErrRateLimited, ""},
		{"409 head out of date", 409, `{"message":"head out of date","url":"https://git.bobparsons.dev/api/swagger"}`, forge.ErrHeadChanged, ""},
		{"409 other", 409, `{"message":"Merge conflict"}`, forge.ErrRefused, "Merge conflict"},
		{"405", 405, `{"message":"PR already merged"}`, forge.ErrRefused, "PR already merged"},
		{"405 required checks", 405, `{"message":"Not all required status checks successful"}`, forge.ErrRefused, "Not all required status checks successful"},
		{"500", 500, `{"message":"boom"}`, nil, "boom"},
	}
	sentinels := []error{forge.ErrUnauthorized, forge.ErrNotFound, forge.ErrRateLimited, forge.ErrHeadChanged, forge.ErrRefused}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				"POST /api/v1/repos/{o}/{r}/pulls/{n}/merge": func(w http.ResponseWriter, _ *http.Request) {
					writeRaw(w, tt.status, []byte(tt.body))
				},
			})
			err := f.Merge(context.Background(), lazyforge, 18, forge.MergeOpts{HeadSHA: headSHA, Method: "squash"})
			if err == nil {
				t.Fatal("want error")
			}
			for _, s := range sentinels {
				if got, want := errors.Is(err, s), errors.Is(tt.sentinel, s); got != want {
					t.Errorf("errors.Is(%v, %v) = %v, want %v", err, s, got, want)
				}
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err %q does not carry server message %q", err, tt.contains)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("err %q leaks the token", err)
			}
		})
	}
}

func TestListReposAccessAndSort(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	repos, err := f.ListRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0].RepoRef != lazyforge || repos[0].Access != domain.AccessAdmin {
		t.Errorf("repos[0] = %+v, want lazyforge with admin", repos[0])
	}
	if repos[1].RepoRef != waugzee || repos[1].Access != domain.AccessRead {
		t.Errorf("repos[1] = %+v, want waugzee with read", repos[1])
	}
	if repos[0].MergeStyle != "merge" {
		t.Errorf("MergeStyle = %q, want merge", repos[0].MergeStyle)
	}
	if repos[0].WebURL != "https://git.bobparsons.dev/deadstyle/lazyforge" {
		t.Errorf("WebURL = %q", repos[0].WebURL)
	}
	if want := time.Date(2026, 10, 2, 3, 22, 20, 0, time.UTC); !repos[0].LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", repos[0].LastActivity, want)
	}
	reqs := srv.requestsTo("GET", "/user/repos")
	if len(reqs) == 0 {
		t.Fatal("no GET /user/repos request")
	}
	for _, r := range reqs {
		if !strings.Contains(r.Query, "page=") || !strings.Contains(r.Query, "limit=50") {
			t.Errorf("query %q lacks page and limit=50", r.Query)
		}
	}
}

func TestPaginationFollowsTotalCount(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	issues, err := f.ListIssues(context.Background(), lazyforge, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 15 {
		t.Fatalf("got %d issues, want all 15 across pages of 2", len(issues))
	}
	reqs := srv.requestsTo("GET", "/issues")
	if len(reqs) != 8 {
		t.Fatalf("got %d page requests, want 8", len(reqs))
	}
	for i, r := range reqs {
		if !strings.Contains(r.Query, fmt.Sprintf("page=%d", i+1)) || !strings.Contains(r.Query, "limit=50") {
			t.Errorf("request %d query = %q", i, r.Query)
		}
	}
}

func TestPaginationStopsOnEmptyPage(t *testing.T) {
	calls := 0
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/releases": func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				writeJSON(w, 200, []obj{{"tag_name": "v1"}})
				return
			}
			writeJSON(w, 200, []obj{})
		},
	})
	rels, err := f.ListReleases(context.Background(), lazyforge)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || calls != 2 {
		t.Errorf("got %d releases in %d calls, want 1 in 2 (no total header, stop on empty page)", len(rels), calls)
	}
}

func TestListChangeRequests(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		repo  domain.RepoRef
		state domain.State
		want  []int
		query string
	}{
		{"open", lazyforge, domain.StateOpen, []int{18}, "state=open"},
		{"merged", lazyforge, domain.StateMerged, []int{14, 15, 16}, "state=closed"},
		{"closed has none when all are merged", lazyforge, domain.StateClosed, nil, "state=closed"},
		{"closed excludes merged", waugzee, domain.StateClosed, []int{3, 4}, "state=closed"},
		{"merged excludes closed", waugzee, domain.StateMerged, []int{1, 2}, "state=closed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, srv := newForge(ctx, t)
			crs, err := f.ListChangeRequests(ctx, tt.repo, forge.Filter{State: tt.state})
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, c := range crs {
				got = append(got, c.Number)
				if c.State != tt.state {
					t.Errorf("#%d state = %v, want %v", c.Number, c.State, tt.state)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("numbers = %v, want %v", got, tt.want)
			}
			pulls := srv.requestsTo("GET", "/pulls")
			if len(pulls) == 0 || !strings.Contains(pulls[0].Query, tt.query) || !strings.Contains(pulls[0].Query, "sort=recentupdate") ||
				!strings.Contains(pulls[0].Query, "page=1") || !strings.Contains(pulls[0].Query, "limit=50") {
				t.Errorf("pulls query = %+v, want %s&sort=recentupdate&page=1&limit=50", pulls, tt.query)
			}
			if n := len(srv.requestsTo("GET", "/status")); n != len(crs) {
				t.Errorf("%d status requests for %d change requests, want one each", n, len(crs))
			}
		})
	}
}

func TestChangeRequestMapping(t *testing.T) {
	f, _ := newForge(t.Context(), t)
	cr, err := f.GetChangeRequest(context.Background(), lazyforge, 18)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number != 18 || cr.Title != "WIP: docs: plan review and fold-in of its findings" || cr.Author != "deadstyle" ||
		cr.SourceBranch != "docs/plan-review" || cr.TargetBranch != "develop" || cr.HeadSHA != openHead ||
		cr.State != domain.StateOpen || cr.CI != domain.CIPass || cr.Renovate != nil ||
		cr.WebURL != "https://git.bobparsons.dev/deadstyle/lazyforge/pulls/18" || len(cr.Labels) != 0 {
		t.Errorf("cr = %+v", cr)
	}
	if want := []domain.Attachment{{Name: "pr.png", URL: "https://git.bobparsons.dev/attachments/bbbb-2222"}}; !slices.Equal(cr.Attachments, want) {
		t.Errorf("attachments = %+v, want %+v", cr.Attachments, want)
	}
	if cr.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
	if cr.CreatedAt.IsZero() || cr.CreatedAt.After(cr.UpdatedAt) {
		t.Errorf("CreatedAt = %v, want set and not after UpdatedAt %v", cr.CreatedAt, cr.UpdatedAt)
	}
	merged, err := f.GetChangeRequest(context.Background(), lazyforge, 16)
	if err != nil {
		t.Fatal(err)
	}
	if merged.State != domain.StateMerged || merged.CI != domain.CINone {
		t.Errorf("#16 state %v CI %v, want merged and none (state \"\")", merged.State, merged.CI)
	}
	closed, err := f.GetChangeRequest(context.Background(), waugzee, 4)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != domain.StateClosed {
		t.Errorf("waugzee #4 state = %v, want closed", closed.State)
	}
}

func TestCombinedStatusToCI(t *testing.T) {
	tests := []struct {
		name string
		body string
		want domain.CIState
	}{
		{"empty", `{"state":"","total_count":0}`, domain.CINone},
		{"pending", `{"state":"pending"}`, domain.CIPending},
		{"success", `{"state":"success"}`, domain.CIPass},
		{"failure", `{"state":"failure"}`, domain.CIFail},
		{"error", `{"state":"error"}`, domain.CIFail},
		{"warning", `{"state":"warning"}`, domain.CIFail},
		{"skipped", `{"state":"skipped"}`, domain.CISkipped},
		{"unknown", `{"state":"flummoxed"}`, domain.CINone},
		{"recorded success", string(readFixture(t, "actions/commit_status_combined_success.json")), domain.CIPass},
		{"recorded failure", string(readFixture(t, "actions/commit_status_combined_failure.json")), domain.CIFail},
	}
	pr := readFixture(t, "pulls.json")
	var prs []json.RawMessage
	if err := json.Unmarshal(pr, &prs); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stubForge(t, map[string]http.HandlerFunc{
				"GET /api/v1/repos/{o}/{r}/pulls/18": func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 200, prs[0]) },
				"GET /api/v1/repos/{o}/{r}/commits/" + openHead + "/status": func(w http.ResponseWriter, _ *http.Request) {
					writeRaw(w, 200, []byte(tt.body))
				},
			})
			cr, err := f.GetChangeRequest(context.Background(), lazyforge, 18)
			if err != nil {
				t.Fatal(err)
			}
			if cr.CI != tt.want {
				t.Errorf("CI = %v, want %v", cr.CI, tt.want)
			}
		})
	}
}

func decodeBody(t *testing.T, r request) obj {
	t.Helper()
	var v obj
	if err := json.Unmarshal([]byte(r.Body), &v); err != nil {
		t.Fatalf("body %q: %v", r.Body, err)
	}
	return v
}

func TestMergeBody(t *testing.T) {
	ctx := context.Background()
	const mergePath = "/api/v1/repos/deadstyle/lazyforge/pulls/18/merge"
	tests := []struct {
		name     string
		style    string
		method   string
		wantDo   string
		wantRepo bool // whether GET /repos/{o}/{r} is expected
	}{
		{"empty method uses repo default", "squash", "", "squash", true},
		{"empty method and empty default falls back to merge", "", "", "merge", true},
		{"explicit method is sent verbatim", "squash", "rebase", "rebase", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, srv := newForge(ctx, t)
			srv.setMergeStyle("deadstyle/lazyforge", tt.style)
			if err := f.Merge(ctx, lazyforge, 18, forge.MergeOpts{HeadSHA: openHead, Method: tt.method}); err != nil {
				t.Fatal(err)
			}
			posts := srv.requestsTo("POST", "/merge")
			if len(posts) != 1 || posts[0].Path != mergePath {
				t.Fatalf("merge requests = %+v", posts)
			}
			if got, want := decodeBody(t, posts[0]), (obj{"Do": tt.wantDo, "head_commit_id": openHead}); !reflect.DeepEqual(got, want) {
				t.Errorf("body = %v, want %v", got, want)
			}
			repoGets := srv.requestsTo("GET", "/repos/deadstyle/lazyforge")
			if (len(repoGets) == 1) != tt.wantRepo || len(repoGets) > 1 {
				t.Errorf("repo lookups = %d, want lookup=%v", len(repoGets), tt.wantRepo)
			}
		})
	}

	t.Run("empty HeadSHA errors before any request", func(t *testing.T) {
		f, srv := newForge(ctx, t)
		if err := f.Merge(ctx, lazyforge, 18, forge.MergeOpts{Method: "squash"}); err == nil {
			t.Fatal("want error")
		}
		if reqs := srv.requests(); len(reqs) != 0 {
			t.Errorf("requests = %+v, want none", reqs)
		}
	})
}

func TestCloseRoutesByKind(t *testing.T) {
	tests := []struct {
		name     string
		kind     forge.ItemKind
		n        int
		wantPath string
	}{
		{"issue", forge.ItemIssue, 17, "/api/v1/repos/deadstyle/lazyforge/issues/17"},
		{"change request", forge.ItemChangeRequest, 18, "/api/v1/repos/deadstyle/lazyforge/pulls/18"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, srv := newForge(t.Context(), t)
			if err := f.Close(context.Background(), forge.ItemRef{Repo: lazyforge, Kind: tt.kind, Number: tt.n}); err != nil {
				t.Fatal(err)
			}
			reqs := srv.requests()
			if len(reqs) != 1 || reqs[0].Method != "PATCH" || reqs[0].Path != tt.wantPath {
				t.Fatalf("requests = %+v, want one PATCH %s", reqs, tt.wantPath)
			}
			if got := decodeBody(t, reqs[0]); !reflect.DeepEqual(got, obj{"state": "closed"}) {
				t.Errorf("body = %v", got)
			}
		})
	}
}

func TestApprove(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	if err := f.Approve(context.Background(), lazyforge, 18); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/api/v1/repos/deadstyle/lazyforge/pulls/18/reviews" {
		t.Fatalf("requests = %+v", reqs)
	}
	if got := decodeBody(t, reqs[0]); !reflect.DeepEqual(got, obj{"event": "APPROVED"}) {
		t.Errorf("body = %v", got)
	}
}

func TestIssues(t *testing.T) {
	ctx := context.Background()

	t.Run("open sends type=issues and maps fields", func(t *testing.T) {
		f, srv := newForge(ctx, t)
		issues, err := f.ListIssues(ctx, lazyforge, forge.Filter{State: domain.StateOpen})
		if err != nil {
			t.Fatal(err)
		}
		if q := srv.requestsTo("GET", "/issues")[0].Query; !strings.Contains(q, "type=issues") || !strings.Contains(q, "state=open") {
			t.Errorf("query = %q", q)
		}
		i := slices.IndexFunc(issues, func(i domain.Issue) bool { return i.Number == 17 })
		if i < 0 {
			t.Fatal("issue #17 missing")
		}
		got := issues[i]
		if got.Title != "Fold plan-review findings into tickets and docs" || got.Author != "deadstyle" || got.Comments != 1 ||
			got.State != domain.StateOpen || !slices.Equal(got.Labels, []string{"docs", "opus"}) ||
			got.WebURL != "https://git.bobparsons.dev/deadstyle/lazyforge/issues/17" || got.UpdatedAt.IsZero() {
			t.Errorf("issue = %+v", got)
		}
		if want := []domain.Attachment{{Name: "shot.png", URL: "https://git.bobparsons.dev/attachments/aaaa-1111"}}; !slices.Equal(got.Attachments, want) {
			t.Errorf("attachments = %+v, want %+v", got.Attachments, want)
		}
	})

	t.Run("closed sends state=closed", func(t *testing.T) {
		f, srv := newForge(ctx, t)
		issues, err := f.ListIssues(ctx, lazyforge, forge.Filter{State: domain.StateClosed})
		if err != nil {
			t.Fatal(err)
		}
		if len(issues) != 0 {
			t.Errorf("issues = %+v, want none closed", issues)
		}
		if q := srv.requestsTo("GET", "/issues")[0].Query; !strings.Contains(q, "state=closed") || !strings.Contains(q, "type=issues") {
			t.Errorf("query = %q", q)
		}
	})

	t.Run("merged is nil with no request", func(t *testing.T) {
		f, srv := newForge(ctx, t)
		issues, err := f.ListIssues(ctx, lazyforge, forge.Filter{State: domain.StateMerged})
		if err != nil || issues != nil {
			t.Errorf("got %v, %v, want nil, nil", issues, err)
		}
		if reqs := srv.requests(); len(reqs) != 0 {
			t.Errorf("requests = %+v, want none", reqs)
		}
	})

	t.Run("EditIssueBody patches body", func(t *testing.T) {
		f, srv := newForge(ctx, t)
		if err := f.EditIssueBody(ctx, lazyforge, 17, "new body"); err != nil {
			t.Fatal(err)
		}
		reqs := srv.requests()
		if len(reqs) != 1 || reqs[0].Method != "PATCH" || reqs[0].Path != "/api/v1/repos/deadstyle/lazyforge/issues/17" {
			t.Fatalf("requests = %+v", reqs)
		}
		if got := decodeBody(t, reqs[0]); !reflect.DeepEqual(got, obj{"body": "new body"}) {
			t.Errorf("body = %v", got)
		}
	})
}

func TestComments(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []forge.ItemKind{forge.ItemIssue, forge.ItemChangeRequest} {
		t.Run(fmt.Sprintf("kind %d uses the issues path", kind), func(t *testing.T) {
			f, srv := newForge(ctx, t)
			item := forge.ItemRef{Repo: lazyforge, Kind: kind, Number: 17}
			if err := f.Comment(ctx, item, "hello"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.ListComments(ctx, item); err != nil {
				t.Fatal(err)
			}
			for _, r := range srv.requests() {
				if r.Path != "/api/v1/repos/deadstyle/lazyforge/issues/17/comments" {
					t.Errorf("path = %s, want the issues comments path", r.Path)
				}
			}
			posts := srv.requestsTo("POST", "/comments")
			if len(posts) != 1 || !reflect.DeepEqual(decodeBody(t, posts[0]), obj{"body": "hello"}) {
				t.Errorf("posts = %+v", posts)
			}
		})
	}

	t.Run("mapping", func(t *testing.T) {
		f, _ := newForge(ctx, t)
		cs, err := f.ListComments(ctx, forge.ItemRef{Repo: lazyforge, Kind: forge.ItemIssue, Number: 17})
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) != 1 || cs[0].ID != 10696 || cs[0].Author != "deadstyle" || !strings.HasPrefix(cs[0].Body, "🤖 Ticket bodies updated") ||
			!cs[0].CreatedAt.Equal(time.Date(2026, 10, 2, 15, 29, 48, 0, time.UTC)) {
			t.Errorf("comments = %+v", cs)
		}
	})
}

func TestListReleases(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	repo := domain.RepoRef{Owner: "deadstyle", Name: "headroom-kompresser"}
	rels, err := f.ListReleases(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 {
		t.Fatalf("releases = %+v", rels)
	}
	r := rels[0]
	if r.Tag != "v1" || r.Name != "v1" || !strings.HasPrefix(r.Notes, "Kompress model") || r.Draft || r.Prerelease ||
		r.WebURL != "https://git.bobparsons.dev/deadstyle/headroom-kompresser/releases/tag/v1" ||
		!r.PublishedAt.Equal(time.Date(2026, 9, 12, 19, 15, 59, 0, time.UTC)) {
		t.Errorf("release = %+v", r)
	}
	q := srv.requestsTo("GET", "/releases")[0].Query
	if !strings.Contains(q, "page=1") || !strings.Contains(q, "limit=50") {
		t.Errorf("query = %q", q)
	}
}

func TestListRuns(t *testing.T) {
	ctx := context.Background()
	f, srv := newForge(t.Context(), t)

	runs, err := f.ListRuns(ctx, waugzee, forge.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3 across two pages (total_count in body)", len(runs))
	}
	want := domain.Run{
		ID: 1895, Number: 49, Workflow: "build-deploy.yml", Title: "switch to zitadel", Branch: "main",
		Commit: "423e5bea3500fbca234c3b6f0aa0545fd20d7956", Event: "push", Status: domain.CIPass,
		StartedAt: time.Date(2026, 8, 16, 22, 4, 13, 0, time.UTC), Duration: 182 * time.Second,
		WebURL: "https://git.bobparsons.dev/deadstyle/waugzee/actions/runs/49",
	}
	got := runs[0]
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
	got.StartedAt = want.StartedAt
	if got != want {
		t.Errorf("run = %+v, want %+v", got, want)
	}
	first := srv.requestsTo("GET", "/actions/runs")[0].Query
	for _, bad := range []string{"ref=", "head_sha="} {
		if strings.Contains(first, bad) {
			t.Errorf("empty filter query %q contains %s", first, bad)
		}
	}
	if !strings.Contains(first, "page=1") || !strings.Contains(first, "limit=50") {
		t.Errorf("query = %q", first)
	}

	srv.reset()
	if _, err := f.ListRuns(ctx, waugzee, forge.RunFilter{Branch: "main", HeadSHA: "abc123"}); err != nil {
		t.Fatal(err)
	}
	q := srv.requestsTo("GET", "/actions/runs")[0].Query
	if !strings.Contains(q, "ref=refs%2Fheads%2Fmain") && !strings.Contains(q, "ref=refs/heads/main") {
		t.Errorf("query %q lacks ref=refs/heads/main", q)
	}
	if !strings.Contains(q, "head_sha=abc123") {
		t.Errorf("query %q lacks head_sha", q)
	}
}

func TestRunStatusToCI(t *testing.T) {
	tests := []struct {
		status string
		want   domain.CIState
	}{
		{"success", domain.CIPass},
		{"failure", domain.CIFail},
		{"cancelled", domain.CICancelled},
		{"skipped", domain.CISkipped},
		{"running", domain.CIRunning},
		{"waiting", domain.CIPending},
		{"blocked", domain.CIPending},
		{"unknown", domain.CINone},
		{"mystery", domain.CINone},
	}
	var runs, jobs []obj
	for i, tt := range tests {
		runs = append(runs, obj{"id": i + 1, "status": tt.status})
		jobs = append(jobs, obj{"id": i + 1, "run_id": 1, "status": tt.status})
	}
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/actions/runs": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, obj{"workflow_runs": runs, "total_count": len(runs)})
		},
		"GET /api/v1/repos/{o}/{r}/actions/runs/{id}/jobs": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Total-Count", fmt.Sprint(len(jobs)))
			writeJSON(w, 200, jobs)
		},
	})
	gotRuns, err := f.ListRuns(context.Background(), lazyforge, forge.RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	gotJobs, err := f.ListJobs(context.Background(), lazyforge, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotRuns) != len(tests) || len(gotJobs) != len(tests) {
		t.Fatalf("got %d runs and %d jobs, want %d each", len(gotRuns), len(gotJobs), len(tests))
	}
	for i, tt := range tests {
		if gotRuns[i].Status != tt.want {
			t.Errorf("run status %q = %v, want %v", tt.status, gotRuns[i].Status, tt.want)
		}
		if gotJobs[i].Status != tt.want {
			t.Errorf("job status %q = %v, want %v", tt.status, gotJobs[i].Status, tt.want)
		}
	}
}

func TestListJobs(t *testing.T) {
	f, srv := newForge(t.Context(), t)
	jobs, err := f.ListJobs(context.Background(), waugzee, 1895)
	if err != nil {
		t.Fatal(err)
	}
	if reqs := srv.requestsTo("GET", "/jobs"); len(reqs) == 0 || reqs[0].Path != "/api/v1/repos/deadstyle/waugzee/actions/runs/1895/jobs" {
		t.Errorf("requests = %+v, want the run id 1895 in the path", reqs)
	}
	if len(jobs) != 9 {
		t.Fatalf("got %d jobs, want 9", len(jobs))
	}
	if want := (domain.Job{ID: 73, RunID: 9, Name: "build-frontend", Status: domain.CIPass, Attempt: 1}); jobs[0] != want {
		t.Errorf("jobs[0] = %+v, want %+v", jobs[0], want)
	}
	if want := (domain.Job{ID: 80, RunID: 9, Name: "migration", Status: domain.CIFail, Attempt: 1}); jobs[7] != want {
		t.Errorf("jobs[7] = %+v, want %+v", jobs[7], want)
	}
	if want := (domain.Job{ID: 81, RunID: 9, Name: "deploy", Status: domain.CISkipped, Attempt: 0}); jobs[8] != want {
		t.Errorf("jobs[8] = %+v, want %+v", jobs[8], want)
	}
}

func TestJobLog(t *testing.T) {
	ctx := context.Background()
	f, srv := newForge(t.Context(), t)
	rc, err := f.JobLog(ctx, waugzee, 73)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if want := readFixture(t, "actions/job_log.txt"); !bytes.Equal(got, want) {
		t.Errorf("log is %d bytes, want %d identical bytes", len(got), len(want))
	}
	if reqs := srv.requests(); len(reqs) != 1 || reqs[0].Path != "/api/v1/repos/deadstyle/waugzee/actions/jobs/73/logs" {
		t.Errorf("requests = %+v", reqs)
	}

	missing := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/actions/jobs/{id}/logs": func(w http.ResponseWriter, _ *http.Request) {
			writeRaw(w, 404, readFixture(t, "error_404.json"))
		},
	})
	missingRC, err := missing.JobLog(ctx, waugzee, 1)
	if !errors.Is(err, forge.ErrNotFound) || missingRC != nil {
		t.Errorf("got %v, %v, want nil reader and ErrNotFound", missingRC, err)
	}
}

func TestCancelledContext(t *testing.T) {
	srv := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gitea.New(ctx, srv.URL, testToken, srv.Client()); !errors.Is(err, context.Canceled) {
		t.Errorf("New err = %v, want context.Canceled", err)
	}
	f, err := gitea.New(context.Background(), srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ListRepos(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ListRepos err = %v, want context.Canceled", err)
	}
}

func TestAccessLevels(t *testing.T) {
	perm := func(admin, push, pull bool) obj {
		return obj{"admin": admin, "push": push, "pull": pull}
	}
	repos := []obj{
		{"name": "a", "owner": obj{"login": "o"}, "updated_at": "2026-01-04T00:00:00Z", "permissions": perm(true, true, true)},
		{"name": "w", "owner": obj{"login": "o"}, "updated_at": "2026-01-03T00:00:00Z", "permissions": perm(false, true, true)},
		{"name": "r", "owner": obj{"login": "o"}, "updated_at": "2026-01-02T00:00:00Z", "permissions": perm(false, false, true)},
		{"name": "n", "owner": obj{"login": "o"}, "updated_at": "2026-01-01T00:00:00Z", "permissions": perm(false, false, false)},
	}
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/user/repos": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Total-Count", "4")
			writeJSON(w, 200, slices.Clone(repos)) // served oldest first, so only sorting can put "a" first
		},
	})
	slices.Reverse(repos)
	got, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.Access{"a": domain.AccessAdmin, "w": domain.AccessWrite, "r": domain.AccessRead, "n": domain.AccessNone}
	if len(got) != 4 {
		t.Fatalf("got %d repos", len(got))
	}
	if order := []string{got[0].Name, got[1].Name, got[2].Name, got[3].Name}; !slices.Equal(order, []string{"a", "w", "r", "n"}) {
		t.Errorf("order = %v, want most recently updated first", order)
	}
	for _, r := range got {
		if r.Access != want[r.Name] {
			t.Errorf("%s access = %v, want %v", r.Name, r.Access, want[r.Name])
		}
	}
}

func TestLabelsAndClosedIssueState(t *testing.T) {
	pr := loadList(t, "pulls.json")[0]
	pr["labels"] = []obj{{"name": "renovate", "color": "abcdef"}, {"name": "deps"}}
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/pulls/18": func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, pr) },
		"GET /api/v1/repos/{o}/{r}/commits/{ref}/status": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, obj{"state": ""})
		},
		"GET /api/v1/repos/{o}/{r}/issues": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Total-Count", "1")
			writeJSON(w, 200, []obj{{"number": 5, "state": "closed", "user": obj{"login": "x"}}})
		},
	})
	cr, err := f.GetChangeRequest(t.Context(), lazyforge, 18)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cr.Labels, []string{"renovate", "deps"}) {
		t.Errorf("labels = %v", cr.Labels)
	}
	if cr.LabelColors["renovate"] != "abcdef" {
		t.Fatalf("label colors = %v", cr.LabelColors)
	}
	issues, err := f.ListIssues(t.Context(), lazyforge, forge.Filter{State: domain.StateClosed})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].State != domain.StateClosed {
		t.Errorf("issues = %+v, want one closed", issues)
	}
}
