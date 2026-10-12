package gitlab_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitlab"
)

var (
	_ forge.Forge    = (*gitlab.Forge)(nil)
	_ forge.Approver = (*gitlab.Forge)(nil)
)

var (
	sub   = domain.RepoRef{Owner: "group/sub", Name: "repo"}
	other = domain.RepoRef{Owner: "group", Name: "other"}
	old   = domain.RepoRef{Owner: "solo", Name: "old"}
)

const encodedSub = "/projects/group%2Fsub%2Frepo"

func newForge(t *testing.T) (*gitlab.Forge, *server) {
	t.Helper()
	s := newServer(t)
	f, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	s.reset()
	return f, s
}

func jsonBody(t *testing.T, r request) obj {
	t.Helper()
	var v obj
	if err := json.Unmarshal([]byte(r.Body), &v); err != nil {
		t.Fatalf("request body %q is not JSON: %v", r.Body, err)
	}
	return v
}

// waits swaps in a retryWait that records the requested wait and returns immediately.
func waits(t *testing.T) *[]time.Duration {
	t.Helper()
	var got []time.Duration
	gitlab.SetRetryWait(t, func(_ context.Context, d time.Duration) error {
		got = append(got, d)
		return nil
	})
	return &got
}

func TestContract(t *testing.T) {
	forgetest.RunContract(t, func(t *testing.T) (forge.Forge, forgetest.Fixture) {
		f, _ := newForge(t)
		return f, forgetest.Fixture{Repo: sub, OpenCR: openMR, OpenCRHead: openHead, OpenIssue: openIss, Missing: missing}
	})
}

func TestNewReadsUserAndVersion(t *testing.T) {
	f, s := newForge(t)
	want := forge.HostInfo{Kind: forge.KindGitLab, URL: s.URL, Version: "17.5.0-ee", User: "alice", ChangeRequestTerm: "MR"}
	if got := f.Info(); got != want {
		t.Fatalf("info %+v, want %+v", got, want)
	}
}

func TestNewAnswersUserThenMetadataWithToken(t *testing.T) {
	s := newServer(t)
	if _, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client()); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 || !strings.HasSuffix(reqs[0].URI, "/api/v4/user") || !strings.HasSuffix(reqs[1].URI, "/api/v4/metadata") {
		t.Fatalf("requests %+v, want GET /user then GET /metadata", reqs)
	}
	for _, r := range reqs {
		if r.Method != http.MethodGet || r.Token != testToken {
			t.Errorf("request %+v, want a GET with PRIVATE-TOKEN %q", r, testToken)
		}
	}
}

func TestInfoURLIsTheWebRootWithoutTrailingSlash(t *testing.T) {
	s := newServer(t)
	f, err := gitlab.New(t.Context(), s.webURL()+"/", testToken, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Info().URL; got != s.URL {
		t.Fatalf("URL %q, want %q", got, s.URL)
	}
}

func TestSelfManagedUnderARelativeURLRoot(t *testing.T) {
	s := newServerAt(t, "/gitlab")
	f, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Info().URL; got != s.URL+"/gitlab" {
		t.Fatalf("URL %q, want the web root %q", got, s.URL+"/gitlab")
	}
	if _, err := f.GetChangeRequest(t.Context(), sub, openMR); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodGet, "/merge_requests/7")
	if len(reqs) != 1 || !strings.HasPrefix(reqs[0].URI, "/gitlab/api/v4/projects/") {
		t.Fatalf("requests %+v, want one under /gitlab/api/v4", reqs)
	}
}

func TestNewBadTokenIsUnauthorizedAndDoesNotLeakIt(t *testing.T) {
	s := newServer(t)
	_, err := gitlab.New(t.Context(), s.webURL(), "wrong-token", s.Client())
	if !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestProbe(t *testing.T) {
	json200 := reply{Status: 200, Body: `{"version":"17.5.0","revision":"abc"}`}
	tests := []struct {
		name    string
		replies []reply
		want    forge.Kind
		wantErr error
	}{
		{"anonymous request is a GitLab 401", nil, forge.KindGitLab, nil},
		{"200 with a version field", []reply{json200}, forge.KindGitLab, nil},
		{"401 with another message", []reply{{Status: 401, Body: `{"message":"Bad credentials"}`}}, "", forge.ErrNotFound},
		{"401 that is not JSON", []reply{{Status: 401, Body: `Unauthorized`}}, "", forge.ErrNotFound},
		{"200 without a version field", []reply{{Status: 200, Body: `{"login":"x"}`}}, "", forge.ErrNotFound},
		{"200 HTML page", []reply{{Status: 200, Body: `<html></html>`}}, "", forge.ErrNotFound},
		{"404", []reply{{Status: 404, Body: `{"message":"404 Not Found"}`}}, "", forge.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(t)
			s.answer(http.MethodGet, "/api/v4/metadata", tt.replies...)
			got, err := gitlab.Probe(t.Context(), s.webURL()+"/", s.Client())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got kind %q err %v, want %v", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v, want %q", got, err, tt.want)
			}
			reqs := s.requests()
			if len(reqs) != 1 || reqs[0].Token != "" || !strings.HasSuffix(reqs[0].URI, "/api/v4/metadata") {
				t.Fatalf("requests %+v, want one unauthenticated GET /api/v4/metadata", reqs)
			}
		})
	}
}

func TestGateAllowsEveryAction(t *testing.T) {
	f, _ := newForge(t)
	for a := forge.ActMerge; a <= forge.ActDispatchWorkflow; a++ {
		if err := f.Gate(a); err != nil {
			t.Errorf("Gate(%d) = %v, want nil", a, err)
		}
	}
}

func TestListReposAccess(t *testing.T) {
	f, s := newForge(t)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[domain.RepoRef]domain.Repo{}
	for _, r := range repos {
		byRef[r.RepoRef] = r
	}
	frozen := domain.RepoRef{Owner: "team", Name: "frozen"}
	want := map[domain.RepoRef]domain.Access{
		sub:    domain.AccessAdmin, // project_access 40
		other:  domain.AccessWrite, // both null, listed by min_access_level=30
		old:    domain.AccessRead,  // both null, not listed
		frozen: domain.AccessRead,  // archived, despite project_access 50
	}
	for ref, access := range want {
		got, ok := byRef[ref]
		if !ok {
			t.Errorf("repo %s missing from %v", ref, repos)
			continue
		}
		if got.Access != access {
			t.Errorf("%s access %v, want %v", ref, got.Access, access)
		}
	}
	q := queryOf(t, s.requestsTo(http.MethodGet, "/projects?")[0].URI)
	for k, v := range map[string]string{"membership": "true", "archived": "false", "order_by": "last_activity_at", "sort": "desc", "per_page": "100"} {
		if q.Get(k) != v {
			t.Errorf("ListRepos query %s=%q, want %q", k, q.Get(k), v)
		}
	}
}

func TestListReposMapsFields(t *testing.T) {
	f, _ := newForge(t)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r := repos[0]
	if r.RepoRef != sub || r.DefaultBranch != "main" || r.Description != "A project in a subgroup" ||
		r.WebURL != "https://gitlab.example.com/group/sub/repo" || r.MergeStyle != "merge" ||
		!r.LastActivity.Equal(time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("repo %+v does not match the first project fixture", r)
	}
}

func TestListReposAccessLevels(t *testing.T) {
	level := func(n int) any { return obj{"access_level": n} }
	tests := []struct {
		name         string
		project, grp any
		want         domain.Access
	}{
		{"owner", level(50), nil, domain.AccessAdmin},
		{"maintainer", level(40), nil, domain.AccessAdmin},
		{"developer", level(30), nil, domain.AccessWrite},
		{"reporter", level(20), nil, domain.AccessRead},
		{"guest", level(10), nil, domain.AccessRead},
		{"group access only", nil, level(40), domain.AccessAdmin},
		{"group beats project", level(20), level(30), domain.AccessWrite},
		{"project beats group", level(40), level(20), domain.AccessAdmin},
	}
	var ps []obj
	for i, tt := range tests {
		ps = append(ps, obj{
			"id": 100 + i, "name": tt.name, "path_with_namespace": "lvl/" + strings.ReplaceAll(tt.name, " ", "-"),
			"last_activity_at": "2026-10-01T00:00:00.000Z", "merge_method": "merge", "squash_option": "default_off",
			"permissions": obj{"project_access": tt.project, "group_access": tt.grp},
		})
	}
	f, s := newForge(t)
	s.setProjects(ps)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i, tt := range tests {
		ref := domain.RepoRef{Owner: "lvl", Name: strings.ReplaceAll(tt.name, " ", "-")}
		idx := slices.IndexFunc(repos, func(r domain.Repo) bool { return r.RepoRef == ref })
		if idx < 0 {
			t.Fatalf("case %d: %s missing", i, ref)
		}
		if repos[idx].Access != tt.want {
			t.Errorf("%s: access %v, want %v", tt.name, repos[idx].Access, tt.want)
		}
	}
	if n := len(s.requestsTo(http.MethodGet, "min_access_level")); n != 0 {
		t.Errorf("%d min_access_level requests, want none when no project has null permissions", n)
	}
}

func TestListReposNullPermissionsFallbackIsOnePagedListing(t *testing.T) {
	f, s := newForge(t)
	if _, err := f.ListRepos(t.Context()); err != nil {
		t.Fatal(err)
	}
	fallback := s.requestsTo(http.MethodGet, "min_access_level=30")
	if len(fallback) == 0 {
		t.Fatal("no GET /projects with min_access_level=30")
	}
	if q := queryOf(t, fallback[0].URI); q.Get("membership") != "true" || q.Get("simple") != "true" {
		t.Errorf("fallback query %v, want membership=true and simple=true", q)
	}
}

func TestListReposMergeStyle(t *testing.T) {
	tests := []struct{ method, squash, want string }{
		{"merge", "default_off", "merge"},
		{"merge", "never", "merge"},
		{"rebase_merge", "default_off", "rebase-merge"},
		{"ff", "default_off", "fast-forward-only"},
		{"ff", "never", "fast-forward-only"},
		{"merge", "always", "squash"},
		{"merge", "default_on", "squash"},
		{"rebase_merge", "always", "squash"},
		{"ff", "default_on", "squash"},
	}
	f, s := newForge(t)
	for _, tt := range tests {
		t.Run(tt.method+"/"+tt.squash, func(t *testing.T) {
			s.setProject("group/sub/repo", "merge_method", tt.method)
			s.setProject("group/sub/repo", "squash_option", tt.squash)
			repos, err := f.ListRepos(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(repos, func(r domain.Repo) bool { return r.RepoRef == sub })
			if i < 0 || repos[i].MergeStyle != tt.want {
				t.Fatalf("MergeStyle for %s/%s = %v, want %q", tt.method, tt.squash, repos, tt.want)
			}
		})
	}
}

func TestListReposFollowsLinkAcrossThreePages(t *testing.T) {
	var ps []obj
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		ps = append(ps, obj{
			"id": 200 + len(ps), "name": n, "path_with_namespace": "pg/" + n, "last_activity_at": "2026-10-01T00:00:00.000Z",
			"merge_method": "merge", "squash_option": "default_off",
			"permissions": obj{"project_access": obj{"access_level": 30}},
		})
	}
	f, s := newForge(t)
	s.setProjects(ps)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 5 {
		t.Fatalf("got %d repos, want all 5 from three pages of at most 2", len(repos))
	}
	pages := s.requestsTo(http.MethodGet, "/projects?")
	if len(pages) != 3 {
		t.Fatalf("%d listing requests, want 3 (a short page must not end the walk)", len(pages))
	}
	for i, r := range pages {
		q := queryOf(t, r.URI)
		if q.Get("per_page") != "100" {
			t.Errorf("request %d per_page=%q, want 100", i, q.Get("per_page"))
		}
		if i > 0 && q.Get("page") != string(rune('1'+i)) {
			t.Errorf("request %d page=%q, want %d", i, q.Get("page"), i+1)
		}
	}
}

func TestPagerRefusesANextLinkOffTheAPIOrigin(t *testing.T) {
	var hits atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(elsewhere.Close)
	f, s := newForge(t)
	s.answer(http.MethodGet, "/projects?", reply{
		Status: 200,
		Header: map[string]string{"Link": "<" + elsewhere.URL + `/api/v4/projects?page=2>; rel="next"`},
		Body:   `[]`,
	})
	if _, err := f.ListRepos(t.Context()); err == nil {
		t.Fatal("got nil error, want one for a next link on another origin")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests reached the other origin, want none: the token would go with them", n)
	}
}

func TestNewRejectsABadTokenAtUserEvenIfMetadataAnswers(t *testing.T) {
	s := newServer(t)
	s.answer(http.MethodGet, "/api/v4/user", reply{Status: 401, Body: `{"message":"401 Unauthorized"}`})
	if _, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client()); !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized from GET /user", err)
	}
}

func TestListReposIsSortedByActivityWhateverOrderTheServerSends(t *testing.T) {
	f, s := newForge(t)
	ps := loadList(t, "projects.json")
	slices.Reverse(ps)
	s.setProjects(ps)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSortedFunc(repos, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) }) {
		t.Errorf("repos %v are not sorted by LastActivity descending", repos)
	}
}

func TestRetryAfterSecondsDecideTheWait(t *testing.T) {
	got := waits(t)
	f, s := newForge(t)
	s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 429, Header: map[string]string{"Retry-After": "7"}})
	if _, err := f.GetChangeRequest(t.Context(), sub, openMR); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*got, []time.Duration{7 * time.Second}) {
		t.Errorf("waits %v, want 7s from Retry-After", *got)
	}
}

func TestProjectPathKeepsEncodedSlashesOnTheRequestLine(t *testing.T) {
	f, s := newForge(t)
	if _, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodGet, "/merge_requests")
	if len(reqs) == 0 || !strings.HasPrefix(reqs[0].URI, "/api/v4"+encodedSub+"/merge_requests?") {
		t.Fatalf("requests %+v, want the request line to carry %s", reqs, encodedSub)
	}
	if reqs[0].Token != testToken {
		t.Errorf("PRIVATE-TOKEN %q, want %q", reqs[0].Token, testToken)
	}
}

func TestListChangeRequestsStateParam(t *testing.T) {
	tests := []struct {
		state domain.State
		param string
		want  []int
	}{
		{domain.StateOpen, "opened", []int{7, 8}},
		{domain.StateClosed, "closed", []int{4}},
		{domain.StateMerged, "merged", []int{5}},
	}
	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			f, s := newForge(t)
			crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: tt.state})
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, c := range crs {
				got = append(got, c.Number)
				if c.State != tt.state {
					t.Errorf("#%d state %v, want %v", c.Number, c.State, tt.state)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("numbers %v, want %v", got, tt.want)
			}
			list := s.requestsTo(http.MethodGet, "/merge_requests?")
			if len(list) == 0 || queryOf(t, list[0].URI).Get("state") != tt.param {
				t.Errorf("list requests %+v, want state=%s on the first", list, tt.param)
			}
		})
	}
}

func TestChangeRequestMapsFields(t *testing.T) {
	f, _ := newForge(t)
	cr, err := f.GetChangeRequest(t.Context(), sub, openMR)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number != 7 || cr.Title != "Bump example.com/lib to v1.4.0" || cr.Body != "Renovate update table" ||
		cr.Author != "renovate-bot" || cr.State != domain.StateOpen || cr.SourceBranch != "renovate/example-lib-1.x" ||
		cr.TargetBranch != "main" || cr.HeadSHA != openHead ||
		cr.WebURL != "https://gitlab.example.com/group/sub/repo/-/merge_requests/7" ||
		!slices.Equal(cr.Labels, []string{"dependencies", "renovate"}) ||
		!cr.UpdatedAt.Equal(time.Date(2026, 10, 10, 7, 30, 0, 0, time.UTC)) ||
		!cr.CreatedAt.Equal(time.Date(2026, 10, 8, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("change request %+v does not match the fixture", cr)
	}
	if cr.Attachments != nil {
		t.Errorf("Attachments %v, want nil", cr.Attachments)
	}
}

func TestGetChangeRequestStates(t *testing.T) {
	f, _ := newForge(t)
	for n, want := range map[int]domain.State{5: domain.StateMerged, 4: domain.StateClosed, 7: domain.StateOpen} {
		cr, err := f.GetChangeRequest(t.Context(), sub, n)
		if err != nil {
			t.Fatal(err)
		}
		if cr.State != want {
			t.Errorf("#%d state %v, want %v", n, cr.State, want)
		}
	}
}

// ciCases is the plan's fold table; both the list and the single GET must agree with it.
var ciCases = []struct {
	status string
	want   domain.CIState
}{
	{"failed", domain.CIFail},
	{"canceled", domain.CICancelled},
	{"canceling", domain.CICancelled},
	{"running", domain.CIRunning},
	{"created", domain.CIPending},
	{"waiting_for_resource", domain.CIPending},
	{"preparing", domain.CIPending},
	{"waiting_for_callback", domain.CIPending},
	{"pending", domain.CIPending},
	{"scheduled", domain.CIPending},
	{"manual", domain.CIPending},
	{"success", domain.CIPass},
	{"skipped", domain.CISkipped},
	{"a_status_gitlab_adds_later", domain.CIPending},
}

func TestGetChangeRequestFoldsHeadPipeline(t *testing.T) {
	for _, tt := range ciCases {
		t.Run(tt.status, func(t *testing.T) {
			f, s := newForge(t)
			s.setHead(openMR, tt.status)
			cr, err := f.GetChangeRequest(t.Context(), sub, openMR)
			if err != nil {
				t.Fatal(err)
			}
			if cr.CI != tt.want {
				t.Errorf("CI %v, want %v", cr.CI, tt.want)
			}
			if n := len(s.requestsTo(http.MethodGet, "/pipelines")); n != 0 {
				t.Errorf("%d pipelines requests, want none: GetChangeRequest uses head_pipeline", n)
			}
		})
	}
}

func TestGetChangeRequestWithoutPipelineHasNoCI(t *testing.T) {
	f, s := newForge(t)
	s.setHead(openMR, "")
	cr, err := f.GetChangeRequest(t.Context(), sub, openMR)
	if err != nil {
		t.Fatal(err)
	}
	if cr.CI != domain.CINone {
		t.Errorf("CI %v, want CINone", cr.CI)
	}
}

func TestListChangeRequestsFoldsMatchedPipeline(t *testing.T) {
	for _, tt := range ciCases {
		t.Run(tt.status, func(t *testing.T) {
			f, s := newForge(t)
			s.setPipelines([]obj{{"id": 300, "sha": openHead, "status": tt.status}})
			crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen})
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(crs, func(c domain.ChangeRequest) bool { return c.Number == openMR })
			if i < 0 || crs[i].CI != tt.want {
				t.Fatalf("change requests %+v, want #%d with CI %v", crs, openMR, tt.want)
			}
		})
	}
}

func TestListChangeRequestsCIFromOnePipelineListing(t *testing.T) {
	f, s := newForge(t)
	crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	ci := map[int]domain.CIState{}
	for _, c := range crs {
		ci[c.Number] = c.CI
	}
	// Two pipelines carry #7's sha; the newest (running) wins over the older success.
	if ci[openMR] != domain.CIRunning {
		t.Errorf("#7 CI %v, want running from the newest matching pipeline", ci[openMR])
	}
	// #8's pipeline is a merged-results pipeline with another sha, so only its head_pipeline has it.
	if ci[draftMR] != domain.CIFail {
		t.Errorf("#8 CI %v, want fail from the head_pipeline fallback", ci[draftMR])
	}
	pipes := s.requestsTo(http.MethodGet, "/pipelines")
	if len(pipes) != 1 {
		t.Fatalf("%d pipelines requests, want exactly 1 per repo", len(pipes))
	}
	q := queryOf(t, pipes[0].URI)
	if q.Get("per_page") != "100" || q.Get("order_by") != "id" || q.Get("sort") != "desc" {
		t.Errorf("pipelines query %v, want per_page=100 order_by=id sort=desc", q)
	}
	if n := len(s.requestsTo(http.MethodGet, encodedSub+"/merge_requests/7")); n != 0 {
		t.Errorf("%d single GETs for #7, want none: its sha matched", n)
	}
	if n := len(s.requestsTo(http.MethodGet, encodedSub+"/merge_requests/8")); n != 1 {
		t.Errorf("%d single GETs for #8, want exactly 1", n)
	}
}

func TestListChangeRequestsWithNoPipelineAnywhereHasNoCI(t *testing.T) {
	f, s := newForge(t)
	s.setPipelines(nil)
	s.setHead(openMR, "")
	s.setHead(draftMR, "")
	crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range crs {
		if c.CI != domain.CINone {
			t.Errorf("#%d CI %v, want CINone", c.Number, c.CI)
		}
	}
}

func TestListChangeRequestsToleratesUnavailablePipelines(t *testing.T) {
	for _, status := range []int{403, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f, s := newForge(t)
			s.answer(http.MethodGet, "/pipelines", reply{Status: status, Body: `{"message":"nope"}`})
			crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen})
			if err != nil {
				t.Fatalf("got %v, want the list to survive a %d on pipelines", err, status)
			}
			ci := map[int]domain.CIState{}
			for _, c := range crs {
				ci[c.Number] = c.CI
			}
			if len(crs) != 2 || ci[draftMR] != domain.CIFail {
				t.Errorf("change requests %+v, want both MRs with #8's CI from its head_pipeline fallback", crs)
			}
		})
	}
}

func TestListChangeRequestsMergedAndClosedSkipHeadPipelineFallback(t *testing.T) {
	for _, state := range []domain.State{domain.StateMerged, domain.StateClosed} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			f, s := newForge(t)
			s.setPipelines(nil)
			crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: state})
			if err != nil {
				t.Fatal(err)
			}
			if len(crs) != 1 || crs[0].CI != domain.CINone {
				t.Errorf("change requests %+v, want one with CINone", crs)
			}
			for _, r := range s.requests() {
				if r.Method == http.MethodGet && strings.Contains(r.URI, "/merge_requests/") {
					t.Errorf("single-MR GET %s, want none for a %v list", r.URI, state)
				}
			}
		})
	}
}

func TestMergeSendsConfirmedHeadAndMerges(t *testing.T) {
	f, s := newForge(t)
	if err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPut, encodedSub+"/merge_requests/7/merge")
	if len(reqs) != 1 {
		t.Fatalf("%d merge requests, want 1", len(reqs))
	}
	if got := jsonBody(t, reqs[0])["sha"]; got != openHead {
		t.Errorf("merge sha %v, want %s", got, openHead)
	}
	if st := s.mrState(openMR); st != "merged" {
		t.Errorf("state %q, want merged", st)
	}
}

func TestMergeSquashFollowsTheProjectSquashOption(t *testing.T) {
	tests := []struct {
		option string
		want   bool
	}{
		{"always", true},
		{"default_on", true},
		{"default_off", false},
		{"never", false},
	}
	for _, tt := range tests {
		t.Run(tt.option, func(t *testing.T) {
			f, s := newForge(t)
			s.setProject("group/sub/repo", "squash_option", tt.option)
			if err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead}); err != nil {
				t.Fatal(err)
			}
			reqs := s.requestsTo(http.MethodPut, "/merge_requests/7/merge")
			if len(reqs) != 1 {
				t.Fatalf("%d merge requests, want 1", len(reqs))
			}
			if got, _ := jsonBody(t, reqs[0])["squash"].(bool); got != tt.want {
				t.Errorf("squash %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeMethodMustMatchTheProjects(t *testing.T) {
	f, s := newForge(t)
	err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead, Method: "rebase-merge"})
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported for a method other than the project's", err)
	}
	if n := len(s.requestsTo(http.MethodPut, "/merge")); n != 0 {
		t.Errorf("%d merge PUTs, want none", n)
	}
	for _, m := range []string{"", "merge"} {
		if err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead, Method: m}); err != nil {
			t.Errorf("Method %q: %v, want nil", m, err)
		}
		s.setMRs(loadList(t, "merge_requests.json"))
	}
}

func TestMergeStaleHeadIsErrHeadChanged(t *testing.T) {
	f, _ := newForge(t)
	err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead + "-stale"})
	if !errors.Is(err, forge.ErrHeadChanged) {
		t.Fatalf("got %v, want ErrHeadChanged", err)
	}
}

func TestMergeDraftIsRefused(t *testing.T) {
	f, _ := newForge(t)
	err := f.Merge(t.Context(), sub, draftMR, forge.MergeOpts{HeadSHA: draftSHA})
	if !errors.Is(err, forge.ErrRefused) {
		t.Fatalf("got %v, want ErrRefused", err)
	}
}

func TestMergeAnd401IsRefusedWithGitLabsMessage(t *testing.T) {
	f, s := newForge(t)
	s.answer(http.MethodPut, "/merge", reply{Status: 401, Body: `{"message":"401 Unauthorized"}`})
	err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
	if !errors.Is(err, forge.ErrRefused) {
		t.Fatalf("got %v, want ErrRefused: GitLab uses 401 for no permission to merge", err)
	}
	if errors.Is(err, forge.ErrUnauthorized) {
		t.Errorf("%v also matches ErrUnauthorized, which would read as a bad token", err)
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Errorf("error %q does not carry GitLab's message", err)
	}
}

func TestApproveAnd401IsRefused(t *testing.T) {
	f, s := newForge(t)
	s.answer(http.MethodPost, "/approve", reply{Status: 401, Body: `{"message":"401 Unauthorized"}`})
	if err := f.Approve(t.Context(), sub, openMR); !errors.Is(err, forge.ErrRefused) {
		t.Fatalf("got %v, want ErrRefused", err)
	}
}

func TestMergeErrorStatuses(t *testing.T) {
	tests := []struct {
		name   string
		reply  reply
		want   error
		inText string
	}{
		{"405 not mergeable", reply{Status: 405, Body: `{"message":"405 Method Not Allowed"}`}, forge.ErrRefused, "405 Method Not Allowed"},
		{"406", reply{Status: 406, Body: `{"message":"Branch has conflicts"}`}, forge.ErrRefused, "Branch has conflicts"},
		{"422", reply{Status: 422, Body: `{"message":"Branch cannot be merged"}`}, forge.ErrRefused, "Branch cannot be merged"},
		{"object message", reply{Status: 422, Body: `{"message":{"base":["Source branch is invalid"]}}`}, forge.ErrRefused, "Source branch is invalid"},
		{"409 sha mismatch", reply{Status: 409, Body: `{"message":"SHA does not match HEAD of source branch"}`}, forge.ErrHeadChanged, ""},
		{"403", reply{Status: 403, Body: `{"message":"403 Forbidden"}`}, forge.ErrUnauthorized, ""},
		{"404", reply{Status: 404, Body: `{"message":"404 Not found"}`}, forge.ErrNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForge(t)
			s.answer(http.MethodPut, "/merge", tt.reply)
			err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if tt.inText != "" && !strings.Contains(err.Error(), tt.inText) {
				t.Errorf("error %q does not carry %q", err, tt.inText)
			}
		})
	}
}

func TestOtherConflictsAreRefusedNotHeadChanged(t *testing.T) {
	f, s := newForge(t)
	s.answer(http.MethodPut, "/issues/3", reply{Status: 409, Body: `{"message":"Another user is editing this"}`})
	err := f.Close(t.Context(), forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss})
	if !errors.Is(err, forge.ErrRefused) || errors.Is(err, forge.ErrHeadChanged) {
		t.Fatalf("got %v, want ErrRefused and not ErrHeadChanged", err)
	}
}

func TestReadErrorStatuses(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{401, forge.ErrUnauthorized},
		{403, forge.ErrUnauthorized},
		{404, forge.ErrNotFound},
	}
	for _, tt := range tests {
		f, s := newForge(t)
		s.answer(http.MethodGet, "/merge_requests/7", reply{Status: tt.status, Body: `{"message":"nope"}`})
		_, err := f.GetChangeRequest(t.Context(), sub, openMR)
		if !errors.Is(err, tt.want) {
			t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
		}
	}
}

func TestRateLimitRetriesOnceAfterRetryAfter(t *testing.T) {
	got := waits(t)
	f, s := newForge(t)
	s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 429, Header: map[string]string{"Retry-After": "3"}, Body: "Retry later"})
	cr, err := f.GetChangeRequest(t.Context(), sub, openMR)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Number != openMR {
		t.Errorf("got #%d after the retry", cr.Number)
	}
	if !slices.Equal(*got, []time.Duration{3 * time.Second}) {
		t.Errorf("waits %v, want one wait of 3s", *got)
	}
	if n := len(s.requestsTo(http.MethodGet, "/merge_requests/7")); n != 2 {
		t.Errorf("%d requests, want 2 (the 429 and one retry)", n)
	}
}

func TestRateLimitTwiceIsErrRateLimitedAfterOneRetry(t *testing.T) {
	got := waits(t)
	f, s := newForge(t)
	limited := reply{Status: 429, Header: map[string]string{"Retry-After": "1"}, Body: "Retry later"}
	s.answer(http.MethodGet, "/merge_requests/7", limited, limited, limited)
	_, err := f.GetChangeRequest(t.Context(), sub, openMR)
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}
	if errors.Is(err, forge.ErrUnauthorized) {
		t.Errorf("%v also matches ErrUnauthorized, which would read as a bad token", err)
	}
	if len(*got) != 1 {
		t.Errorf("%d waits, want exactly 1", len(*got))
	}
	if n := len(s.requestsTo(http.MethodGet, "/merge_requests/7")); n != 2 {
		t.Errorf("%d requests, want 2: retry once, never loop", n)
	}
}

func TestRateLimitWaitIsCappedAtSixtySeconds(t *testing.T) {
	got := waits(t)
	f, s := newForge(t)
	s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 429, Header: map[string]string{"Retry-After": "600"}})
	if _, err := f.GetChangeRequest(t.Context(), sub, openMR); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*got, []time.Duration{60 * time.Second}) {
		t.Errorf("waits %v, want one wait of 60s", *got)
	}
}

func TestRateLimitedWriteResendsItsBodyOnce(t *testing.T) {
	waits(t)
	f, s := newForge(t)
	s.answer(http.MethodPost, "/notes", reply{Status: 429, Header: map[string]string{"Retry-After": "1"}})
	item := forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss}
	if err := f.Comment(t.Context(), item, "after the limit"); err != nil {
		t.Fatal(err)
	}
	posts := s.requestsTo(http.MethodPost, "/notes")
	if len(posts) != 2 {
		t.Fatalf("%d POSTs, want 2", len(posts))
	}
	for _, p := range posts {
		if got := jsonBody(t, p)["body"]; got != "after the limit" {
			t.Errorf("POST body %q, want the comment on both attempts", p.Body)
		}
	}
}

func TestRetryWaitReturnsTheContextErrorWhenItEndsFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gitlab.RetryWait(ctx, 50*time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if err := gitlab.RetryWait(t.Context(), time.Millisecond); err != nil {
		t.Fatalf("got %v, want nil once the wait elapses", err)
	}
}

func TestApproveSendsNoBody(t *testing.T) {
	f, s := newForge(t)
	if err := f.Approve(t.Context(), sub, openMR); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPost, encodedSub+"/merge_requests/7/approve")
	if len(reqs) != 1 {
		t.Fatalf("%d approve requests, want 1", len(reqs))
	}
	if reqs[0].Body != "" {
		t.Errorf("approve body %q, want empty (no sha)", reqs[0].Body)
	}
}

func TestListIssuesOnlyIssues(t *testing.T) {
	f, s := newForge(t)
	issues, err := f.ListIssues(t.Context(), sub, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	var nums []int
	for _, i := range issues {
		nums = append(nums, i.Number)
	}
	slices.Sort(nums)
	if !slices.Equal(nums, []int{2, 3}) {
		t.Errorf("issue numbers %v, want [2 3]: no incidents, test cases or tasks", nums)
	}
	q := queryOf(t, s.requestsTo(http.MethodGet, encodedSub+"/issues?")[0].URI)
	if q.Get("issue_type") != "issue" || q.Get("state") != "opened" {
		t.Errorf("query %v, want issue_type=issue and state=opened", q)
	}
}

func TestListIssuesClosedAndMerged(t *testing.T) {
	f, s := newForge(t)
	closed, err := f.ListIssues(t.Context(), sub, forge.Filter{State: domain.StateClosed})
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0].Number != 1 || closed[0].State != domain.StateClosed {
		t.Errorf("closed issues %+v, want only #1", closed)
	}
	if q := queryOf(t, s.requestsTo(http.MethodGet, "/issues?")[0].URI); q.Get("state") != "closed" {
		t.Errorf("state=%q, want closed", q.Get("state"))
	}
	s.reset()
	merged, err := f.ListIssues(t.Context(), sub, forge.Filter{State: domain.StateMerged})
	if err != nil {
		t.Fatal(err)
	}
	if merged != nil || len(s.requests()) != 0 {
		t.Errorf("merged filter returned %v after %d requests, want nil and none", merged, len(s.requests()))
	}
}

func TestIssueMapsFields(t *testing.T) {
	f, _ := newForge(t)
	issues, err := f.ListIssues(t.Context(), sub, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	i := issues[slices.IndexFunc(issues, func(i domain.Issue) bool { return i.Number == openIss })]
	if i.Title != "Crash on empty config" || i.Body != "Steps to reproduce" || i.Author != "bob" || i.State != domain.StateOpen ||
		!slices.Equal(i.Labels, []string{"bug"}) || i.WebURL != "https://gitlab.example.com/group/sub/repo/-/issues/3" ||
		!i.UpdatedAt.Equal(time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("issue %+v does not match the fixture", i)
	}
	if i.Attachments != nil {
		t.Errorf("Attachments %v, want nil", i.Attachments)
	}
}

func TestEditIssueBodyPutsDescription(t *testing.T) {
	f, s := newForge(t)
	if err := f.EditIssueBody(t.Context(), sub, openIss, "new body"); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPut, encodedSub+"/issues/3")
	if len(reqs) != 1 {
		t.Fatalf("%d PUTs, want 1", len(reqs))
	}
	if got := jsonBody(t, reqs[0])["description"]; got != "new body" {
		t.Errorf("description %v, want %q", got, "new body")
	}
}

func TestListCommentsDropsSystemNotesAndSortsAscending(t *testing.T) {
	f, s := newForge(t)
	item := forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss}
	comments, err := f.ListComments(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, c := range comments {
		bodies = append(bodies, c.Body)
	}
	want := []string{"Happens with an empty file", "I can reproduce this", "Fixed in the next release"}
	if !slices.Equal(bodies, want) {
		t.Errorf("bodies %q, want %q (oldest first, no system notes)", bodies, want)
	}
	if comments[0].ID != 301 || comments[0].Author != "bob" || !comments[0].CreatedAt.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("first comment %+v does not match note 301", comments[0])
	}
	reqs := s.requestsTo(http.MethodGet, encodedSub+"/issues/3/notes?")
	if len(reqs) == 0 {
		t.Fatal("no GET on the issue's notes")
	}
	if q := queryOf(t, reqs[0].URI); q.Get("sort") != "asc" || q.Get("order_by") != "created_at" || q.Get("per_page") != "100" {
		t.Errorf("notes query %v, want sort=asc order_by=created_at per_page=100", q)
	}
	if len(reqs) != 2 {
		t.Errorf("%d notes pages, want 2: four notes at two per page", len(reqs))
	}
}

func TestChangeRequestCommentsUseTheMergeRequestNotes(t *testing.T) {
	f, s := newForge(t)
	item := forge.ItemRef{Repo: sub, Kind: forge.ItemChangeRequest, Number: openMR}
	comments, err := f.ListComments(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "Looks good to me" {
		t.Errorf("comments %+v, want only the user note", comments)
	}
	if n := len(s.requestsTo(http.MethodGet, encodedSub+"/merge_requests/7/notes?")); n == 0 {
		t.Error("no GET on the merge request's notes")
	}
	if err := f.Comment(t.Context(), item, "ship it"); err != nil {
		t.Fatal(err)
	}
	posts := s.requestsTo(http.MethodPost, encodedSub+"/merge_requests/7/notes")
	if len(posts) != 1 || jsonBody(t, posts[0])["body"] != "ship it" {
		t.Errorf("posts %+v, want one with body %q", posts, "ship it")
	}
}

func TestCloseSendsStateEvent(t *testing.T) {
	f, s := newForge(t)
	if err := f.Close(t.Context(), forge.ItemRef{Repo: sub, Kind: forge.ItemChangeRequest, Number: draftMR}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPut, encodedSub+"/merge_requests/8")
	if len(reqs) != 1 || jsonBody(t, reqs[0])["state_event"] != "close" {
		t.Fatalf("requests %+v, want one PUT with state_event=close", reqs)
	}
	if st := s.mrState(draftMR); st != "closed" {
		t.Errorf("state %q, want closed", st)
	}
	if err := f.Close(t.Context(), forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.requestsTo(http.MethodPut, encodedSub+"/issues/3")); n != 1 {
		t.Errorf("%d PUTs on the issue, want 1", n)
	}
}

func TestListReleasesFallsBackToCreatedAt(t *testing.T) {
	f, _ := newForge(t)
	rels, err := f.ListReleases(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("%d releases, want 2", len(rels))
	}
	byTag := map[string]domain.Release{}
	for _, r := range rels {
		byTag[r.Tag] = r
	}
	if got := byTag["v1.1.0"].PublishedAt; !got.Equal(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("v1.1.0 PublishedAt %v, want released_at", got)
	}
	if got := byTag["v1.0.0"].PublishedAt; !got.Equal(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("v1.0.0 PublishedAt %v, want created_at as released_at is missing", got)
	}
	if byTag["v1.1.0"].Name != "Release 1.1.0" || byTag["v1.1.0"].Notes != "Adds retries" {
		t.Errorf("release %+v, want Name and Notes from name and description", byTag["v1.1.0"])
	}
}
