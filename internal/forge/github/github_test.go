package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/github"
)

var cli = domain.RepoRef{Owner: "cli", Name: "cli"}

func newForge(t *testing.T) (*github.Forge, *server) {
	t.Helper()
	s := newServer(t)
	f, err := github.New(t.Context(), s.URL, testToken, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	s.reset()
	return f, s
}

func TestContract(t *testing.T) {
	forgetest.RunContract(t, func(t *testing.T) (forge.Forge, forgetest.Fixture) {
		f, _ := newForge(t)
		return f, forgetest.Fixture{Repo: cli, OpenCR: openPR, OpenCRHead: openHead, OpenIssue: openIssue, Missing: 1}
	})
}

func TestNewReadsUserAndGHESVersion(t *testing.T) {
	f, s := newForge(t)
	want := forge.HostInfo{Kind: forge.KindGitHub, URL: s.URL, Version: "3.17.4", User: "Bparsons0904", ChangeRequestTerm: "PR"}
	if got := f.Info(); got != want {
		t.Fatalf("info %+v, want %+v", got, want)
	}
}

func TestNewBadTokenIsUnauthorized(t *testing.T) {
	s := newServer(t)
	_, err := github.New(t.Context(), s.URL, "wrong-token", s.Client())
	if !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

// recorder is a Transport that answers every request from a fixed table and records the URLs, so no network is used.
type recorder struct {
	mu   sync.Mutex
	urls []string
	body map[string]string // by path
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	b, ok := r.body[req.URL.Path]
	code := 200
	if !ok {
		code, b = 404, `{"message":"Not Found"}`
	}
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(b)), Request: req}, nil
}

func TestAPIBaseMapping(t *testing.T) {
	tests := []struct {
		web, wantURL string
		wantCalls    []string
		wantVersion  string
	}{
		{"https://github.com", "https://github.com", []string{"https://api.github.com/user"}, ""},
		{"https://GitHub.com/", "https://GitHub.com", []string{"https://api.github.com/user"}, ""},
		{"https://ghe.example.com/", "https://ghe.example.com", []string{"https://ghe.example.com/api/v3/user", "https://ghe.example.com/api/v3/meta"}, "3.17.4"},
	}
	for _, tt := range tests {
		t.Run(tt.web, func(t *testing.T) {
			rt := &recorder{body: map[string]string{
				"/user":        `{"login":"Bparsons0904"}`,
				"/api/v3/user": `{"login":"Bparsons0904"}`,
				"/api/v3/meta": `{"verifiable_password_authentication":false,"installed_version":"3.17.4"}`,
			}}
			f, err := github.New(context.Background(), tt.web, testToken, &http.Client{Transport: rt})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(rt.urls, tt.wantCalls) {
				t.Errorf("calls %v, want %v", rt.urls, tt.wantCalls)
			}
			if got := f.Info(); got.URL != tt.wantURL || got.Version != tt.wantVersion {
				t.Errorf("info %+v, want URL %q version %q", got, tt.wantURL, tt.wantVersion)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		body     string
		kind     forge.Kind
		notFound bool
	}{
		{"github.com", 200, string(readFixture(t, "meta.json")), forge.KindGitHub, false},
		{"ghes", 200, string(readFixture(t, "meta_ghes.json")), forge.KindGitHub, false},
		{"private mode", 401, `{"message":"Must authenticate"}`, "", false},
		{"404", 404, `{"message":"Not Found"}`, "", true},
		{"html", 200, `<html></html>`, "", true},
		{"other json", 200, `{"version":"16.0.5+gitea-1.22.0"}`, "", true},
		{"empty", 200, ``, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				if r.Header.Get("Authorization") != "" {
					t.Error("probe sent Authorization")
				}
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			kind, err := github.Probe(context.Background(), srv.URL, nil)
			if tt.notFound != errors.Is(err, forge.ErrNotFound) || (!tt.notFound && err != nil) || kind != tt.kind {
				t.Fatalf("got %q, %v", kind, err)
			}
			if path != "/api/v3/meta" {
				t.Errorf("probed %q, want /api/v3/meta", path)
			}
		})
	}
}

func TestListReposAccessAndOrder(t *testing.T) {
	f, s := newForge(t)
	repos, err := f.ListRepos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range repos {
		got = append(got, r.String()+":"+strconv.Itoa(int(r.Access))+":"+r.MergeStyle)
	}
	// Fixture order is lazyforge, rootcamp, cli/cli; pushed_at puts cli/cli (14:14) between lazyforge (14:26) and rootcamp (Sept).
	want := []string{"Bparsons0904/lazyforge:3:", "cli/cli:2:", "Bparsons0904/rootcamp:3:"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	reqs := s.requestsTo(http.MethodGet, "/user/repos")
	if len(reqs) != 2 || !strings.Contains(reqs[0].Query, "affiliation=owner%2Ccollaborator%2Corganization_member") || !strings.Contains(reqs[0].Query, "per_page=100") {
		t.Fatalf("requests %+v", reqs)
	}
}

func TestListChangeRequests(t *testing.T) {
	f, _ := newForge(t)
	tests := []struct {
		state domain.State
		want  []int
	}{
		{domain.StateOpen, []int{14634, 14629}},
		{domain.StateMerged, []int{14633, 14632, 14631}},
		{domain.StateClosed, []int{14630}},
	}
	for _, tt := range tests {
		crs, err := f.ListChangeRequests(t.Context(), cli, forge.Filter{State: tt.state})
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
		if !slices.Equal(got, tt.want) {
			t.Errorf("state %v: got %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestChangeRequestCIFoldsBothSources(t *testing.T) {
	f, s := newForge(t)
	tests := []struct {
		n    int
		want domain.CIState
	}{
		{openPR, domain.CIPass}, // check runs success+skipped; combined status total_count 0 says "pending" and must not count
		{14629, domain.CIFail},  // no check runs; combined status failure
	}
	for _, tt := range tests {
		cr, err := f.GetChangeRequest(t.Context(), cli, tt.n)
		if err != nil {
			t.Fatal(err)
		}
		if cr.CI != tt.want {
			t.Errorf("#%d CI %v, want %v", tt.n, cr.CI, tt.want)
		}
	}
	if len(s.requestsTo(http.MethodGet, "/check-runs")) < 3 {
		t.Error("check runs (4 items, 2 per page) were not walked across pages")
	}
}

func TestChangeRequestCIMissingHeadFoldsToNone(t *testing.T) {
	tests := []struct {
		code    int
		wantErr error // nil: the list succeeds with the PR's CI as CINone
	}{
		{404, nil},
		{422, nil}, // GitHub's "No commit found for SHA"
		{401, forge.ErrUnauthorized},
		{429, forge.ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			f, s := newForge(t)
			s.failCI(failingHead, tt.code)
			crs, err := f.ListChangeRequests(t.Context(), cli, forge.Filter{State: domain.StateOpen})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[int]domain.CIState{}
			for _, c := range crs {
				got[c.Number] = c.CI
			}
			want := map[int]domain.CIState{openPR: domain.CIPass, 14629: domain.CINone}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestListIssuesDropsPRs(t *testing.T) {
	f, s := newForge(t)
	issues, err := f.ListIssues(t.Context(), cli, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, is := range issues {
		got = append(got, is.Number)
	}
	if !slices.Equal(got, []int{14627, 14621}) {
		t.Fatalf("got %v, want the two issues without the PRs 14634 and 14629", got)
	}
	s.reset()
	merged, err := f.ListIssues(t.Context(), cli, forge.Filter{State: domain.StateMerged})
	if err != nil || merged != nil || len(s.requests()) != 0 {
		t.Fatalf("merged: %v, %v, %d requests", merged, err, len(s.requests()))
	}
}

func TestMergeMethod(t *testing.T) {
	tests := []struct {
		name                   string
		merge, squash, rebase  any
		method, want           string
		wantRepoGET, wantField bool
	}{
		{"all allowed picks merge", true, true, true, "", "merge", true, true},
		{"no merge commits picks squash", false, true, true, "", "squash", true, true},
		{"rebase only", false, false, true, "", "rebase", true, true},
		{"flags unreadable sends none", nil, nil, nil, "", "", true, false},
		{"explicit method passes through", false, false, true, "squash", "squash", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForge(t)
			s.setMergeFlags("cli/cli", tt.merge, tt.squash, tt.rebase)
			if err := f.Merge(t.Context(), cli, openPR, forge.MergeOpts{HeadSHA: openHead, Method: tt.method}); err != nil {
				t.Fatal(err)
			}
			puts := s.requestsTo(http.MethodPut, "/merge")
			if len(puts) != 1 {
				t.Fatalf("%d merge requests", len(puts))
			}
			var body map[string]string
			if err := json.Unmarshal([]byte(puts[0].Body), &body); err != nil {
				t.Fatal(err)
			}
			got, has := body["merge_method"]
			if got != tt.want || has != tt.wantField || body["sha"] != openHead {
				t.Errorf("body %v, want merge_method %q (present %v) and sha", body, tt.want, tt.wantField)
			}
			if gets := len(s.requestsTo(http.MethodGet, "/repos/cli/cli")); (gets == 1) != tt.wantRepoGET {
				t.Errorf("%d repo GETs, want one: %v", gets, tt.wantRepoGET)
			}
		})
	}
}

func TestApproveSendsAPPROVE(t *testing.T) {
	f, s := newForge(t)
	if err := f.Approve(t.Context(), cli, openPR); err != nil {
		t.Fatal(err)
	}
	posts := s.requestsTo(http.MethodPost, "/pulls/14634/reviews")
	if len(posts) != 1 || posts[0].Body != `{"event":"APPROVE"}` {
		t.Fatalf("requests %+v", posts)
	}
}

func TestConditionalRequestReplays304(t *testing.T) {
	f, s := newForge(t)
	first, err := f.ListReleases(t.Context(), cli)
	if err != nil {
		t.Fatal(err)
	}
	s.reset()
	second, err := f.ListReleases(t.Context(), cli)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || !reflect.DeepEqual(first, second) {
		t.Fatalf("first %v, second %v", first, second)
	}
	reqs := s.requests()
	if len(reqs) == 0 {
		t.Fatal("second list made no request")
	}
	for _, r := range reqs {
		if r.IfNoneMatch == "" || r.Status != http.StatusNotModified {
			t.Errorf("request %s?%s: If-None-Match %q status %d, want a 304 revalidation", r.Path, r.Query, r.IfNoneMatch, r.Status)
		}
	}
}

func TestConditionalRequestRefetchesChanges(t *testing.T) {
	f, _ := newForge(t)
	item := forge.ItemRef{Repo: cli, Kind: forge.ItemIssue, Number: openIssue}
	before, err := f.ListComments(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Comment(t.Context(), item, "new"); err != nil {
		t.Fatal(err)
	}
	after, err := f.ListComments(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 || after[len(after)-1].Body != "new" {
		t.Fatalf("before %d, after %v", len(before), after)
	}
}

func TestConcurrentUse(t *testing.T) {
	f, _ := newForge(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := f.ListChangeRequests(context.Background(), cli, forge.Filter{State: domain.StateOpen}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

// stub serves user and meta plus one handler at the given pattern, under /api/v3.
func stub(t *testing.T, pattern string, h http.HandlerFunc) *github.Forge {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 200, readFixture(t, "user.json")) })
	mux.HandleFunc("GET /api/v3/meta", func(w http.ResponseWriter, _ *http.Request) { writeRaw(w, 200, readFixture(t, "meta_ghes.json")) })
	mux.HandleFunc(pattern, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f, err := github.New(t.Context(), srv.URL, testToken, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name      string
		code      int
		remaining string
		body      string
		want      error
		notWant   error
	}{
		{"primary rate limit 403", 403, "0", "rate_limit_403.json", forge.ErrRateLimited, forge.ErrUnauthorized},
		{"secondary rate limit 403", 403, "4999", `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`, forge.ErrRateLimited, forge.ErrUnauthorized},
		{"plain 403", 403, "4999", `{"message":"Resource not accessible by personal access token"}`, forge.ErrUnauthorized, forge.ErrRateLimited},
		{"429", 429, "", `{"message":"Too many requests"}`, forge.ErrRateLimited, nil},
		{"401", 401, "", "error_401.json", forge.ErrUnauthorized, nil},
		{"404", 404, "", "error_404.json", forge.ErrNotFound, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stub(t, "GET /api/v3/repos/cli/cli/releases", func(w http.ResponseWriter, _ *http.Request) {
				if tt.remaining != "" {
					w.Header().Set("X-RateLimit-Remaining", tt.remaining)
				}
				b := []byte(tt.body)
				if strings.HasSuffix(tt.body, ".json") {
					b = readFixture(t, tt.body)
				}
				writeRaw(w, tt.code, b)
			})
			_, err := f.ListReleases(t.Context(), cli)
			if !errors.Is(err, tt.want) || (tt.notWant != nil && errors.Is(err, tt.notWant)) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestMergeErrors(t *testing.T) {
	tests := []struct {
		name, fixture string
		code          int
		want          error
	}{
		{"stale head", "merge_409.json", 409, forge.ErrHeadChanged},
		{"not mergeable", "merge_405.json", 405, forge.ErrRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := stub(t, "PUT /api/v3/repos/cli/cli/pulls/1/merge", func(w http.ResponseWriter, _ *http.Request) {
				writeRaw(w, tt.code, readFixture(t, tt.fixture))
			})
			err := f.Merge(t.Context(), cli, 1, forge.MergeOpts{HeadSHA: "abc", Method: "merge"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestApproveOwnPRIsRefusedWithReason(t *testing.T) {
	f := stub(t, "POST /api/v3/repos/cli/cli/pulls/1/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 422, obj{"message": "Unprocessable Entity", "errors": []string{"Review Can not approve your own pull request"}})
	})
	err := f.Approve(t.Context(), cli, 1)
	if !errors.Is(err, forge.ErrRefused) || !strings.Contains(err.Error(), "Can not approve your own pull request") {
		t.Fatalf("got %v", err)
	}
}

func TestLinkPagingFollowsNextPastShortPages(t *testing.T) {
	var srvURL string
	pages := map[string]struct {
		tags []string
		next string
	}{
		"":  {[]string{"v5", "v4"}, "2"},
		"2": {[]string{"v3"}, "3"}, // short page that still has a next link
		"3": {[]string{"v2", "v1"}, ""},
	}
	var calls int
	f := stub(t, "GET /api/v3/repos/cli/cli/releases", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("query %q lacks per_page=100", r.URL.RawQuery)
		}
		p := pages[r.URL.Query().Get("page")]
		if p.next != "" {
			w.Header().Set("Link", `<`+srvURL+`/api/v3/repos/cli/cli/releases?per_page=100&page=`+p.next+`>; rel="next", <`+srvURL+`/api/v3/repos/cli/cli/releases?per_page=100&page=3>; rel="last"`)
		}
		var items []obj
		for _, tag := range p.tags {
			items = append(items, obj{"tag_name": tag})
		}
		writeJSON(w, 200, items)
	})
	srvURL = f.Info().URL
	rs, err := f.ListReleases(t.Context(), cli)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rs {
		got = append(got, r.Tag)
	}
	if !slices.Equal(got, []string{"v5", "v4", "v3", "v2", "v1"}) || calls != 3 {
		t.Fatalf("got %v in %d calls", got, calls)
	}
}

func TestLinkOffHostIsNotFollowed(t *testing.T) {
	f := stub(t, "GET /api/v3/repos/cli/cli/releases", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://elsewhere.example/steal?page=2>; rel="next"`)
		writeJSON(w, 200, []obj{{"tag_name": "v1"}})
	})
	if _, err := f.ListReleases(t.Context(), cli); err == nil || !strings.Contains(err.Error(), "off the API host") {
		t.Fatalf("got %v, want an off-host refusal", err)
	}
}
