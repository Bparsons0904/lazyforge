package gitlab_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitlab"
)

// mrObj is a merge request as the list endpoint returns it.
func mrObj(iid int, state, sha, branch string) obj {
	return obj{
		"id": 300 + iid, "iid": iid, "title": fmt.Sprintf("MR %d", iid), "state": state, "draft": false,
		"created_at": "2026-10-08T07:00:00.000Z", "updated_at": "2026-10-10T07:30:00.000Z",
		"author": obj{"username": "alice"}, "source_branch": branch, "target_branch": "main", "sha": sha,
		"web_url": fmt.Sprintf("https://gitlab.example.com/group/sub/repo/-/merge_requests/%d", iid),
	}
}

func pipeObj(id int, sha, ref, status string) obj {
	return obj{"id": id, "sha": sha, "ref": ref, "status": status, "source": "push"}
}

func listOpen(t *testing.T, f *gitlab.Forge) []domain.ChangeRequest {
	t.Helper()
	crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	return crs
}

func TestListChangeRequestsPicksThePipelineByShaAndRef(t *testing.T) {
	const sha = "abc"
	tests := []struct {
		name      string
		pipes     []obj
		head      string // head_pipeline status for the per-MR fallback; "" means the fallback is never reached
		want      domain.CIState
		wantFetch int
	}{
		{
			name: "merge request pipeline beats a newer branch pipeline",
			pipes: []obj{
				pipeObj(30, sha, "main", "pending"), pipeObj(29, sha, "feat", "failed"),
				pipeObj(28, sha, "refs/merge-requests/7/head", "success"),
				pipeObj(27, sha, "refs/merge-requests/9/head", "running"), pipeObj(26, sha, "v1.0", "canceled"),
			},
			want: domain.CIPass,
		},
		{
			name:  "wrong branch and a tag give no list match",
			pipes: []obj{pipeObj(30, sha, "main", "success"), pipeObj(26, sha, "v1.0", "success")},
			head:  "running", want: domain.CIRunning, wantFetch: 1,
		},
		{
			name:  "a branch pipeline is used when there is no merge request pipeline",
			pipes: []obj{pipeObj(29, sha, "feat", "failed")},
			want:  domain.CIFail,
		},
		{
			name:  "another merge request's pipeline is not used",
			pipes: []obj{pipeObj(27, sha, "refs/merge-requests/9/head", "success")},
			head:  "running", want: domain.CIRunning, wantFetch: 1,
		},
		{
			name: "the newest of two merge request pipelines wins",
			pipes: []obj{
				pipeObj(40, sha, "refs/merge-requests/7/head", "success"),
				pipeObj(35, sha, "refs/merge-requests/7/head", "failed"),
			},
			want: domain.CIPass,
		},
		{
			name: "a pipeline for another sha on the merge request ref is not used",
			pipes: []obj{
				pipeObj(50, "other", "refs/merge-requests/7/head", "success"), pipeObj(29, sha, "feat", "failed"),
			},
			want: domain.CIFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForge(t)
			s.setMRs([]obj{mrObj(7, "opened", sha, "feat")})
			s.setPipelines(tt.pipes)
			if tt.head != "" {
				s.setHead(7, tt.head)
			}
			crs := listOpen(t, f)
			if len(crs) != 1 || crs[0].CI != tt.want {
				t.Fatalf("change requests %+v, want one with CI %v", crs, tt.want)
			}
			if n := len(s.requestsTo(http.MethodGet, encodedSub+"/merge_requests/7")); n != tt.wantFetch {
				t.Errorf("%d per-MR GETs for #7, want %d", n, tt.wantFetch)
			}
		})
	}
}

// ciStub serves a project with the given open merge requests, a non-empty pipelines listing that matches none of them,
// and per-MR GETs that hold for hold, so the peak number in flight is observable.
type ciStub struct {
	inflight, peak atomic.Int32
}

var reMRGet = regexp.MustCompile(`/merge_requests/(\d+)$`)

func (c *ciStub) handler(_ *testing.T, mrs []obj, heads map[int]string, failIID int, hold time.Duration) func(http.ResponseWriter, *http.Request, string, string) {
	return func(w http.ResponseWriter, _ *http.Request, path, _ string) {
		switch {
		case path == rtProject+"/merge_requests":
			_ = json.NewEncoder(w).Encode(mrs)
		case path == rtProject+"/pipelines":
			_ = json.NewEncoder(w).Encode([]obj{pipeObj(1, "unrelated", "main", "success")})
		case reMRGet.MatchString(path):
			n := c.inflight.Add(1)
			defer c.inflight.Add(-1)
			for {
				p := c.peak.Load()
				if n <= p || c.peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(hold)
			var iid int
			_, _ = fmt.Sscan(reMRGet.FindStringSubmatch(path)[1], &iid)
			if iid == failIID {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			mr := mrObj(iid, "opened", fmt.Sprintf("sha%d", iid), "b")
			mr["head_pipeline"] = obj{"id": 900 + iid, "status": heads[iid]}
			_ = json.NewEncoder(w).Encode(mr)
		default:
			rtNotFound(w)
		}
	}
}

func perMRGets(s *stubAPI) int {
	n := 0
	for _, r := range s.requestsTo(http.MethodGet, "/merge_requests/") {
		if reMRGet.MatchString(strings.SplitN(r.URI, "?", 2)[0]) {
			n++
		}
	}
	return n
}

func TestListChangeRequestsFallbackIsBoundedConcurrentAndKeepsOrder(t *testing.T) {
	order := []int{4, 1, 6, 2, 5, 3}
	heads := map[int]string{1: "success", 2: "failed", 3: "running", 4: "canceled", 5: "skipped", 6: "pending"}
	wantCI := map[int]domain.CIState{
		1: domain.CIPass, 2: domain.CIFail, 3: domain.CIRunning, 4: domain.CICancelled, 5: domain.CISkipped, 6: domain.CIPending,
	}
	var mrs []obj
	for _, iid := range order {
		mrs = append(mrs, mrObj(iid, "opened", fmt.Sprintf("sha%d", iid), "b"))
	}
	var c ciStub
	f, s := newStub(t, c.handler(t, mrs, heads, 0, 60*time.Millisecond))

	crs := listOpen(t, f)
	if len(crs) != len(order) {
		t.Fatalf("%d change requests, want %d", len(crs), len(order))
	}
	for i, cr := range crs {
		if cr.Number != order[i] || cr.CI != wantCI[order[i]] {
			t.Errorf("position %d = #%d CI %v, want #%d CI %v: the list order must hold", i, cr.Number, cr.CI, order[i], wantCI[order[i]])
		}
	}
	if n := perMRGets(s); n != 6 {
		t.Errorf("%d per-MR GETs, want 6", n)
	}
	if got := c.peak.Load(); got > 4 {
		t.Errorf("%d per-MR GETs in flight at once, want at most 4", got)
	}
	if got := c.peak.Load(); got < 2 {
		t.Errorf("peak %d in flight, want the lookups to run concurrently", got)
	}
}

func TestListChangeRequestsFailedFallbackFailsTheList(t *testing.T) {
	var mrs []obj
	heads := map[int]string{}
	for iid := 1; iid <= 6; iid++ {
		mrs = append(mrs, mrObj(iid, "opened", fmt.Sprintf("sha%d", iid), "b"))
		heads[iid] = "success"
	}
	var c ciStub
	f, _ := newStub(t, c.handler(t, mrs, heads, 3, 10*time.Millisecond))

	crs, err := f.ListChangeRequests(t.Context(), rtRepo, forge.Filter{State: domain.StateOpen})
	if err == nil {
		t.Fatalf("got %+v and no error, want the 500 on #3 to fail the list", crs)
	}
	if crs != nil {
		t.Errorf("got %+v alongside the error, want no list", crs)
	}
}

func TestListChangeRequestsEmptyListingStillFallsBackPerOpenMR(t *testing.T) {
	f, s := newForge(t)
	s.setPipelines([]obj{})
	s.setHead(openMR, "success")
	s.setHead(draftMR, "failed")
	ci := map[int]domain.CIState{}
	for _, c := range listOpen(t, f) {
		ci[c.Number] = c.CI
	}
	if ci[openMR] != domain.CIPass || ci[draftMR] != domain.CIFail {
		t.Errorf("CI by MR %v, want #7 pass and #8 fail from their head pipelines", ci)
	}
	for _, iid := range []int{openMR, draftMR} {
		if n := len(s.requestsTo(http.MethodGet, fmt.Sprintf("%s/merge_requests/%d", encodedSub, iid))); n != 1 {
			t.Errorf("%d per-MR GETs for #%d, want exactly 1: a fork MR's pipeline never shows in the listing", n, iid)
		}
	}
}

func TestListChangeRequestsUnavailableListingSkipsEveryPerMRGet(t *testing.T) {
	f, s := newForge(t)
	s.answer(http.MethodGet, "/pipelines", reply{Status: 403, Body: `{"message":"403 Forbidden"}`})
	crs := listOpen(t, f)
	if len(crs) != 2 {
		t.Fatalf("change requests %+v, want both open MRs", crs)
	}
	for _, c := range crs {
		if c.CI != domain.CINone {
			t.Errorf("#%d CI %v, want CINone", c.Number, c.CI)
		}
	}
	if gets := s.requestsTo(http.MethodGet, "/merge_requests/"); len(gets) != 0 {
		t.Errorf("per-MR GETs %+v, want none", gets)
	}
}

func TestListChangeRequestsMergedWithNoMatchSkipsTheGetOnANonEmptyListing(t *testing.T) {
	f, s := newForge(t)
	s.setMRs([]obj{mrObj(5, "merged", "sha5", "docs"), mrObj(4, "closed", "sha4", "spike")})
	s.setPipelines([]obj{pipeObj(1, "unrelated", "main", "success")})
	for _, state := range []domain.State{domain.StateMerged, domain.StateClosed} {
		crs, err := f.ListChangeRequests(t.Context(), sub, forge.Filter{State: state})
		if err != nil {
			t.Fatal(err)
		}
		if len(crs) != 1 || crs[0].CI != domain.CINone {
			t.Errorf("state %v: change requests %+v, want one with CINone", state, crs)
		}
	}
	for _, r := range s.requests() {
		if r.Method == http.MethodGet && regexp.MustCompile(`/merge_requests/\d+$`).MatchString(strings.SplitN(r.URI, "?", 2)[0]) {
			t.Errorf("per-MR GET %s, want none for merged and closed MRs", r.URI)
		}
	}
}

func TestMergeStillReadsTheProject(t *testing.T) {
	f, s := newForge(t)
	if err := f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead}); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, r := range s.requestsTo(http.MethodGet, encodedSub) {
		if strings.HasSuffix(r.URI, encodedSub) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d GET %s, want 1: Merge reads the project for its merge method", n, encodedSub)
	}
}

func TestForbiddenWritesAreRefusedNotUnauthorized(t *testing.T) {
	const msg = "403 Forbidden: you may not do that"
	forbid := reply{Status: 403, Body: `{"message":"` + msg + `"}`}
	tests := []struct {
		name string
		do   func(f *gitlab.Forge, s *server) error
	}{
		{"merge", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPut, "/merge", forbid)
			return f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
		}},
		{"approve", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPost, "/approve", forbid)
			return f.Approve(t.Context(), sub, openMR)
		}},
		{"comment", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPost, "/notes", forbid)
			return f.Comment(t.Context(), forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss}, "hi")
		}},
		{"close", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPut, "/issues/3", forbid)
			return f.Close(t.Context(), forge.ItemRef{Repo: sub, Kind: forge.ItemIssue, Number: openIss})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForge(t)
			err := tt.do(f, s)
			if !errors.Is(err, forge.ErrRefused) || errors.Is(err, forge.ErrUnauthorized) {
				t.Fatalf("err = %v, want ErrRefused and not ErrUnauthorized", err)
			}
			if !strings.HasSuffix(err.Error(), ": "+msg) {
				t.Errorf("err %q does not end with GitLab's message", err)
			}
		})
	}
}

func TestForbiddenReadsStayUnauthorized(t *testing.T) {
	t.Run("GET", func(t *testing.T) {
		f, s := newForge(t)
		s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 403, Body: `{"message":"403 Forbidden"}`})
		if _, err := f.GetChangeRequest(t.Context(), sub, openMR); !errors.Is(err, forge.ErrUnauthorized) || errors.Is(err, forge.ErrRefused) {
			t.Errorf("err = %v, want ErrUnauthorized and not ErrRefused", err)
		}
	})
	t.Run("HEAD through ReadFile", func(t *testing.T) {
		f, _ := newRepoForge(t, map[string]http.HandlerFunc{"HEAD " + repoFiles + "a.txt": repoStatus(403, "403 Forbidden")})
		_, err := f.ReadFile(t.Context(), repoRef, "main", "a.txt")
		if !errors.Is(err, forge.ErrUnauthorized) || errors.Is(err, forge.ErrRefused) {
			t.Errorf("err = %v, want ErrUnauthorized and not ErrRefused", err)
		}
	})
}

func TestSentinelErrorsEndWithGitLabsMessage(t *testing.T) {
	const msg = "the reason GitLab gave"
	body := `{"message":"` + msg + `"}`
	tests := []struct {
		name  string
		do    func(f *gitlab.Forge, s *server) error
		want  error
		retry bool
	}{
		{"401 on merge", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPut, "/merge", reply{Status: 401, Body: body})
			return f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
		}, forge.ErrRefused, false},
		{"403 on merge", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPut, "/merge", reply{Status: 403, Body: body})
			return f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
		}, forge.ErrRefused, false},
		{"401 on read", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 401, Body: body})
			_, err := f.GetChangeRequest(t.Context(), sub, openMR)
			return err
		}, forge.ErrUnauthorized, false},
		{"403 on read", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 403, Body: body})
			_, err := f.GetChangeRequest(t.Context(), sub, openMR)
			return err
		}, forge.ErrUnauthorized, false},
		{"404 on read", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 404, Body: body})
			_, err := f.GetChangeRequest(t.Context(), sub, openMR)
			return err
		}, forge.ErrNotFound, false},
		{"429 twice on read", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 429, Body: body}, reply{Status: 429, Body: body})
			_, err := f.GetChangeRequest(t.Context(), sub, openMR)
			return err
		}, forge.ErrRateLimited, true},
		{"409 on merge", func(f *gitlab.Forge, s *server) error {
			s.answer(http.MethodPut, "/merge", reply{Status: 409, Body: body})
			return f.Merge(t.Context(), sub, openMR, forge.MergeOpts{HeadSHA: openHead})
		}, forge.ErrHeadChanged, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.retry {
				waits(t)
			}
			f, s := newForge(t)
			err := tt.do(f, s)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !strings.HasSuffix(err.Error(), ": "+msg) {
				t.Errorf("err %q does not end with %q", err, ": "+msg)
			}
		})
	}
}

func TestSentinelErrorsWithoutAMessageHaveNoTrailingColon(t *testing.T) {
	f, s := newForge(t)
	s.answer(http.MethodGet, "/merge_requests/7", reply{Status: 404, Body: `{}`})
	_, err := f.GetChangeRequest(t.Context(), sub, openMR)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.HasSuffix(err.Error(), forge.ErrNotFound.Error()) {
		t.Errorf("err %q, want it to end with the sentinel text %q and no trailing \": \"", err, forge.ErrNotFound)
	}
}

func scopesReply(body string) reply { return reply{Status: 200, Body: body} }

func TestNewBlocksATokenWithoutTheAPIScope(t *testing.T) {
	s := newServer(t)
	s.answer(http.MethodGet, "/personal_access_tokens/self", scopesReply(`{"scopes":["read_api","read_user"]}`))
	_, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client())
	if !errors.Is(err, forge.ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
	if !strings.Contains(err.Error(), "lazyforge needs the api scope") {
		t.Errorf("err %q lacks the scope hint", err)
	}
	if errors.Is(err, forge.ErrUnauthorized) {
		t.Errorf("err %v also matches ErrUnauthorized, which onboarding reads as a rejected token", err)
	}
	if reqs := s.requestsTo(http.MethodGet, "/metadata"); len(reqs) != 0 {
		t.Errorf("GET /metadata requested %d times, want never after a read-only token", len(reqs))
	}
	self := s.requestsTo(http.MethodGet, "/personal_access_tokens/self")
	if len(self) != 1 || self[0].Token != testToken {
		t.Errorf("token-scope requests %+v, want one GET carrying the token", self)
	}
}

func TestNewLetsATokenConnectWhenItsScopesAreNotProvablyReadOnly(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
	}{
		{"api", scopesReply(`{"scopes":["api"]}`)},
		{"read_api and api", scopesReply(`{"scopes":["read_api","api"]}`)},
		{"empty scopes, as a granular token", scopesReply(`{"scopes":[]}`)},
		{"no scopes key", scopesReply(`{"granular_scopes":[{"name":"read_repository"}]}`)},
		{"404, as a non-PAT token", reply{Status: 404, Body: `{"message":"404 Not Found"}`}},
		{"401", reply{Status: 401, Body: `{"message":"401 Unauthorized"}`}},
		{"403", reply{Status: 403, Body: `{"message":"403 Forbidden"}`}},
		{"500", reply{Status: 500, Body: `{"message":"boom"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(t)
			s.answer(http.MethodGet, "/personal_access_tokens/self", tt.reply)
			f, err := gitlab.New(t.Context(), s.webURL(), testToken, s.Client())
			if err != nil {
				t.Fatalf("New: %v, want the connection to continue", err)
			}
			if f.Info().Version == "" {
				t.Errorf("Info %+v, want the metadata read after the scope check", f.Info())
			}
		})
	}
}

func TestInfoRunsTermIsPipelines(t *testing.T) {
	f, _ := newForge(t)
	if got := f.Info().RunsTerm; got != "Pipelines" {
		t.Errorf("RunsTerm = %q, want Pipelines", got)
	}
}

func TestTreeAndFileReadsWithAnExplicitRefMakeNoProjectGet(t *testing.T) {
	body := []byte("hi")
	routes := fileRoutes("a.txt", len(body), body)
	routes["GET "+repoTree] = repoServe(repoTreeJSON([2]string{"README.md", "blob"}))
	f, api := newRepoForge(t, routes)
	if _, err := f.ListTree(t.Context(), repoRef, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFile(t.Context(), repoRef, "feature", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if n := api.count(http.MethodGet, repoProject); n != 0 {
		t.Errorf("%d GET %s, want none when the ref is explicit", n, repoProject)
	}
}

func TestTreeAndFileReadsWithNoRefStillReadTheProjectOnce(t *testing.T) {
	body := []byte("hi")
	routes := fileRoutes("a.txt", len(body), body)
	routes["GET "+repoTree] = repoServe(repoTreeJSON([2]string{"README.md", "blob"}))
	f, api := newRepoForge(t, routes)
	if _, err := f.ListTree(t.Context(), repoRef, "", ""); err != nil {
		t.Fatal(err)
	}
	if n := api.count(http.MethodGet, repoProject); n != 1 {
		t.Errorf("ListTree made %d GET %s, want exactly 1", n, repoProject)
	}
	if _, err := f.ReadFile(t.Context(), repoRef, "", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if n := api.count(http.MethodGet, repoProject); n != 2 {
		t.Errorf("after ReadFile, %d GET %s in all, want 1 per call", n, repoProject)
	}
}

func TestGetReadmeMakesNoProjectGet(t *testing.T) {
	f, api := newRepoForge(t, map[string]http.HandlerFunc{
		"GET " + repoTree:                repoServe(repoTreeJSON([2]string{"README.md", "blob"})),
		"GET " + repoFiles + "README.md": repoFileJSON("README.md", readmeBody),
	})
	got, err := f.GetReadme(t.Context(), repoRef)
	if err != nil || got.Body != readmeBody {
		t.Fatalf("GetReadme() = %+v, %v", got, err)
	}
	if n := api.count(http.MethodGet, repoProject); n != 0 {
		t.Errorf("%d GET %s, want none: the server resolves the default branch", n, repoProject)
	}
	for _, c := range api.requests() {
		if c.Path == repoTree && c.Query.Has("ref") {
			t.Errorf("tree query %v carries a ref, want none", c.Query)
		}
		if strings.HasPrefix(c.Path, repoFiles) && c.Query.Get("ref") != "HEAD" {
			t.Errorf("file ref = %q, want HEAD", c.Query.Get("ref"))
		}
	}
}

func TestGetReadmeTreeNotFoundIsErrNotFound(t *testing.T) {
	f, _ := newRepoForge(t, map[string]http.HandlerFunc{"GET " + repoTree: repoStatus(404, "404 Tree Not Found")})
	if _, err := f.GetReadme(t.Context(), repoRef); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// labelsAPI serves labels.json, counts GETs of the project's label list, and echoes label names on item PUTs.
type labelsAPI struct{ lists atomic.Int32 }

func (l *labelsAPI) handler(t *testing.T, itemLabels []string, putLabels []string) func(http.ResponseWriter, *http.Request, string, string) {
	body := rtFixture(t, "labels.json")
	return func(w http.ResponseWriter, r *http.Request, path, _ string) {
		switch {
		case path == rtProject+"/labels" && r.Method == http.MethodGet:
			l.lists.Add(1)
			rtPage(t, w, r, body)
		case strings.HasPrefix(path, rtProject+"/issues/3"):
			names := itemLabels
			if r.Method == http.MethodPut {
				names = putLabels
			}
			out, _ := json.Marshal(map[string]any{"iid": 3, "labels": names})
			_, _ = w.Write(out)
		default:
			rtNotFound(w)
		}
	}
}

func TestSetLabelsListsTheProjectsLabelsOnce(t *testing.T) {
	var l labelsAPI
	f, _ := newStub(t, l.handler(t, nil, []string{"bug", "good first issue"}))
	got, err := f.SetLabels(t.Context(), rtIssue, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if n := l.lists.Load(); n != 3 {
		// labels.json spans three pages at two per page, so one listing is three requests.
		t.Errorf("%d label-list requests, want one listing (3 pages)", n)
	}
	want := []domain.Label{{ID: 1, Name: "bug", Color: "d9534f"}, {ID: 2, Name: "good first issue", Color: "69d100"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("labels %+v, want %+v with their IDs and colours", got, want)
	}
}

func TestSetLabelsClearingMakesNoLabelListing(t *testing.T) {
	var l labelsAPI
	f, _ := newStub(t, l.handler(t, []string{"bug"}, []string{}))
	got, err := f.SetLabels(t.Context(), rtIssue, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("SetLabels(nil) = %+v, %v; want none", got, err)
	}
	if n := l.lists.Load(); n != 0 {
		t.Errorf("%d label-list requests, want none when the response has no labels", n)
	}
}

func TestSetLabelsClearingThatStillReturnsLabelsListsOnceForTheirIDs(t *testing.T) {
	var l labelsAPI
	f, _ := newStub(t, l.handler(t, nil, []string{"bug"}))
	got, err := f.SetLabels(t.Context(), rtIssue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 1 || got[0].Name != "bug" {
		t.Errorf("labels %+v, want bug with ID 1, not a zero ID", got)
	}
	if n := l.lists.Load(); n != 3 {
		t.Errorf("%d label-list requests, want one listing (3 pages)", n)
	}
}

func TestItemLabelsWithNoLabelsMakesNoLabelListing(t *testing.T) {
	var l labelsAPI
	f, _ := newStub(t, l.handler(t, []string{}, nil))
	got, err := f.ItemLabels(t.Context(), rtIssue)
	if err != nil || len(got) != 0 {
		t.Fatalf("ItemLabels = %+v, %v; want none", got, err)
	}
	if n := l.lists.Load(); n != 0 {
		t.Errorf("%d label-list requests, want none for an item without labels", n)
	}
}

// rateLimited answers the first request to match with a 429 and every later one by serving.
type rateLimited struct {
	mu      sync.Mutex
	hits    int
	accepts []string
	tokens  []string
}

func (r *rateLimited) serve(w http.ResponseWriter, req *http.Request, body []byte, contentType string, limit int) {
	r.mu.Lock()
	r.hits++
	hit := r.hits
	r.accepts = append(r.accepts, req.Header.Get("Accept"))
	r.tokens = append(r.tokens, req.Header.Get("PRIVATE-TOKEN"))
	r.mu.Unlock()
	if hit <= limit {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}

func TestJobLogRetriesOnceAfterA429(t *testing.T) {
	waited := waits(t)
	var rl rateLimited
	f, _ := newStub(t, func(w http.ResponseWriter, r *http.Request, path, _ string) {
		if path != rtProject+"/jobs/502/trace" {
			rtNotFound(w)
			return
		}
		rl.serve(w, r, []byte("job output\n"), "text/plain", 1)
	})
	rc, err := f.JobLog(t.Context(), rtRepo, 502)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got := make([]byte, 64)
	n, _ := rc.Read(got)
	if string(got[:n]) != "job output\n" {
		t.Errorf("log %q, want the body of the second answer", got[:n])
	}
	if len(*waited) != 1 || rl.hits != 2 {
		t.Errorf("%d waits and %d requests, want one retry", len(*waited), rl.hits)
	}
	for i := range rl.hits {
		if rl.accepts[i] != "text/plain" || rl.tokens[i] != rtToken {
			t.Errorf("request %d sent Accept %q and token %q, want text/plain and the token", i, rl.accepts[i], rl.tokens[i])
		}
	}
}

func TestJobLogSecond429IsRateLimited(t *testing.T) {
	waits(t)
	var rl rateLimited
	f, _ := newStub(t, func(w http.ResponseWriter, r *http.Request, _, _ string) { rl.serve(w, r, nil, "text/plain", 10) })
	rc, err := f.JobLog(t.Context(), rtRepo, 502)
	if rc != nil {
		_ = rc.Close()
	}
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if rl.hits != 2 {
		t.Errorf("%d requests, want 2: one retry", rl.hits)
	}
}

func TestOpenAssetRetriesOnceAfterA429(t *testing.T) {
	waited := waits(t)
	var rl rateLimited
	f, s := newStub(t, func(w http.ResponseWriter, r *http.Request, path, _ string) {
		if path != rtProject+uploadPath {
			rtNotFound(w)
			return
		}
		rl.serve(w, r, forgetest.DemoPNG, "image/png", 1)
	})
	if _, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath); err != nil {
		t.Fatal(err)
	}
	if len(*waited) != 1 || rl.hits != 2 {
		t.Errorf("%d waits and %d requests, want one retry", len(*waited), rl.hits)
	}
	for i := range rl.hits {
		if rl.tokens[i] != rtToken {
			t.Errorf("request %d token %q, want the token on the host", i, rl.tokens[i])
		}
		if rl.accepts[i] == "application/json" {
			t.Errorf("request %d sent Accept: application/json for an image", i)
		}
	}
}

func TestOpenAssetSecond429IsRateLimited(t *testing.T) {
	waits(t)
	var rl rateLimited
	f, s := newStub(t, func(w http.ResponseWriter, r *http.Request, _, _ string) { rl.serve(w, r, nil, "image/png", 10) })
	_, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath)
	if !errors.Is(err, forge.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if rl.hits != 2 {
		t.Errorf("%d requests, want 2: one retry", rl.hits)
	}
}

func TestOpenAssetWithACanceledContextReturnsContextCanceled(t *testing.T) {
	f, s := newForgeAt(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rc, err := assetReaderOf(t, f).OpenAsset(ctx, mustParse(t, s.URL+"/group/sub/repo"+uploadPath))
	if rc != nil {
		_ = rc.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDispatchWorkflowMapsTheRepoInputToRenovatesFilter(t *testing.T) {
	f, s := newStub(t, createdPipeline(t))
	err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main",
		map[string]string{"repo": "group/app", "LOG_LEVEL": "debug"})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPost, "/pipeline")
	if len(reqs) != 1 {
		t.Fatalf("got %d POSTs to /pipeline, want 1", len(reqs))
	}
	const want = `{"ref":"main","variables":[` +
		`{"key":"LOG_LEVEL","value":"debug","variable_type":"env_var"},` +
		`{"key":"RENOVATE_AUTODISCOVER_FILTER","value":"group/app","variable_type":"env_var"}]}`
	if reqs[0].Body != want {
		t.Errorf("body %s, want %s", reqs[0].Body, want)
	}
}

func TestDispatchWorkflowRepoInputAloneBecomesTheFilter(t *testing.T) {
	f, s := newStub(t, createdPipeline(t))
	if err := dispatcher(t, f).DispatchWorkflow(t.Context(), rtRepo, "", "main", map[string]string{"repo": "group/app"}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requestsTo(http.MethodPost, "/pipeline")
	if len(reqs) != 1 || strings.Contains(reqs[0].Body, `"key":"repo"`) || !strings.Contains(reqs[0].Body, `"key":"RENOVATE_AUTODISCOVER_FILTER","value":"group/app"`) {
		t.Fatalf("requests %+v, want the repo input sent only as RENOVATE_AUTODISCOVER_FILTER", reqs)
	}
}
