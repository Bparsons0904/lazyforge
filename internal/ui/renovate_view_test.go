package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	infra     = domain.RepoRef{Owner: "home", Name: "infra"}
	lazyforge = domain.RepoRef{Owner: "home", Name: "lazyforge"}
)

// rvBody is a Renovate PR body with one update row, in the documented table format.
func rvBody(pkg, bump, from, to string) string {
	return fmt.Sprintf("This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [%s](https://hub.docker.com/_/%[1]s) | %s | `%s` → `%s` |\n\n---\n", pkg, bump, from, to)
}

// rvPostgres is a postgres 16.4 → 17.0 Renovate PR; the title noun lets core infer the docker ecosystem.
func rvPostgres(n int, ci domain.CIState) domain.ChangeRequest {
	return domain.ChangeRequest{
		Number: n, Title: "chore(deps): update postgres docker tag to v17.0", Author: "renovate",
		SourceBranch: "renovate/postgres-17.x", HeadSHA: fmt.Sprintf("sha-pg-%d", n), CI: ci,
		Body:      rvBody("postgres", "major", "16.4", "17.0"),
		CreatedAt: time.Now().Add(-48 * time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
		WebURL: fmt.Sprintf("https://f.test/pulls/%d", n),
	}
}

type rvRepo struct {
	ref    domain.RepoRef
	access domain.Access
	prs    []domain.ChangeRequest
	issues []domain.Issue
}

// rvFake is a host with the given repos, reporting term as its change-request word.
func rvFake(term string, repos ...rvRepo) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: term, User: "you"})
	for i, r := range repos {
		f.AddRepo(domain.Repo{RepoRef: r.ref, Access: r.access, WebURL: "https://f.test/" + r.ref.String(), LastActivity: time.Now().Add(-time.Duration(i) * time.Minute)})
		for _, cr := range r.prs {
			f.AddChangeRequest(r.ref, cr)
		}
		for _, is := range r.issues {
			f.AddIssue(r.ref, is)
		}
	}
	return f
}

// rvSized returns a w×h model over f with every repo scanned and the cursor on ★.
func rvSized(t *testing.T, f forge.Forge, opts core.Options, w, h int) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, opts))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	return sizedWith(t, m, w, h)
}

func assertFits(t *testing.T, m Model, name string) {
	const w, h = 80, 24
	t.Helper()
	ls := lines(m)
	if len(ls) != h {
		t.Errorf("%s: frame is %d lines, want %d", name, len(ls), h)
	}
	for _, l := range ls {
		if lipgloss.Width(l) > w {
			t.Errorf("%s: line wider than %d: %q", name, w, l)
		}
	}
}

// loadedNoScan returns a model whose repo list has arrived and whose ★ scan has started but received nothing.
func loadedNoScan(t *testing.T) (Model, []domain.Repo) {
	t.Helper()
	m := seeded(t)
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	repos, err := m.svc.Repos(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(reposLoadedMsg{repos: repos})
	return next.(Model), repos
}

func scanOf(t *testing.T, m Model, r domain.Repo) renovateScannedMsg {
	t.Helper()
	scan, err := m.svc.RenovateScan(m.ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return renovateScannedMsg{seq: m.star.seq, repo: r.RepoRef, scan: scan}
}

func feed(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// Seam: ★ preview in demo mode. Five titled boxes, [1] totals and per-repo rows by impact, [2] one postgres group.
func TestStarPreviewInDemo(t *testing.T) {
	m := sized(t, 80, 24)
	assertFits(t, m, "preview")
	v := screen(m)
	for _, want := range []string{`\[1\] By repo\b`, `\[2\] Updates by dependency\b`, `\[3\] Renovate PRs\b`, `\[4\] Dashboards\b`, `\[5\] Failing / running CI\b`} {
		if !regexp.MustCompile(want).MatchString(v) {
			t.Errorf("preview missing box title %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "All · 5 · 2M 3m 0p") {
		t.Errorf("preview missing totals line:\n%s", v)
	}

	var order []domain.RepoRef
	for _, r := range m.star.view.ByRepo {
		order = append(order, r.Repo)
	}
	if want := []domain.RepoRef{infra, homelab, lazyforge}; !equalRefs(order, want) {
		t.Errorf("[1] order %v, want %v (impact descending, repos without Renovate PRs omitted)", order, want)
	}

	if n := strings.Count(v, "postgres 16.4 → 17.0"); n != 1 {
		t.Errorf("postgres group shown %d times, want once:\n%s", n, v)
	}
	for _, l := range lines(m) {
		if strings.Contains(l, "postgres 16.4 → 17.0") && !strings.Contains(l, "×2") {
			t.Errorf("postgres group row lacks ×2: %q", l)
		}
	}
}

func equalRefs(a, b []domain.RepoRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Seam: layout. Every ★ screen fits 80×24 exactly: preview, each box focused, each details pane, dialogs, help.
func TestStarEveryViewFits80x24(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "l")
	for b := 1; b <= 5; b++ {
		m = press(t, m, fmt.Sprint(b))
		assertFits(t, m, fmt.Sprintf("box [%d]", b))
		m = press(t, m, "l")
		assertFits(t, m, fmt.Sprintf("details of [%d]", b))
		m = press(t, m, "h")
	}
	m = press(t, m, "?")
	assertFits(t, m, "help")
	m = press(t, m, "esc", "2", "m")
	if m.dialog == nil {
		t.Fatal("no merge dialog on [2]")
	}
	assertFits(t, m, "confirm dialog")
	m = press(t, m, "y")
	assertFits(t, m, "done dialog")
}

// Seam: partial results. Boxes show what has arrived, [1] counts scanned repos, and a late result completes the data.
func TestStarPartialResultsAndCoverageLine(t *testing.T) {
	m, repos := loadedNoScan(t)
	if len(repos) != 4 {
		t.Fatalf("demo has %d repos, want 4", len(repos))
	}

	m = feed(m, scanOf(t, m, repos[0])) // homelab
	v := screen(m)
	if !strings.Contains(v, "scanned 1 of 4 repositories") {
		t.Errorf("want 'scanned 1 of 4 repositories':\n%s", v)
	}
	if len(m.star.view.ByRepo) != 1 || m.star.view.ByRepo[0].Repo != homelab {
		t.Errorf("[1] after one result: %+v", m.star.view.ByRepo)
	}
	if !strings.Contains(v, "postgres 16.4 → 17.0") || strings.Contains(v, "×2") {
		t.Errorf("[2] should show a single-member postgres group after one result:\n%s", v)
	}

	m = feed(m, scanOf(t, m, repos[1])) // infra
	v = screen(m)
	if !strings.Contains(v, "scanned 2 of 4 repositories") || !strings.Contains(v, "×2") {
		t.Errorf("after two results want 'scanned 2 of 4 repositories' and ×2:\n%s", v)
	}

	m = feed(m, renovateScannedMsg{seq: m.star.seq, repo: repos[2].RepoRef, err: errors.New("boom")})
	m = feed(m, scanOf(t, m, repos[3]))
	v = screen(m)
	if !strings.Contains(v, "scanned 4 of 4 repositories") || !strings.Contains(v, "· 1 failed") {
		t.Errorf("a failed repo must stay visible even when every repo answered:\n%s", v)
	}
	if m.status != "" || m.statusErr {
		t.Errorf("failure leaked to the status bar: %q", m.status)
	}
}

// Seam: a clean complete scan drops the coverage line.
func TestStarCoverageLineGoneWhenComplete(t *testing.T) {
	m := sized(t, 80, 24)
	if strings.Contains(screen(m), "scanned ") {
		t.Errorf("coverage line shown for a complete scan:\n%s", screen(m))
	}
}

// Seam: stale results. A result from before a rescan or from before leaving ★ changes nothing.
func TestStarScanDropsResultsFromEarlierScans(t *testing.T) {
	m, repos := loadedNoScan(t)
	old := renovateScannedMsg{seq: m.star.seq, repo: repos[0].RepoRef, err: errors.New("from the old scan")}

	m = feed(m, tea.KeyPressMsg{Code: 'j', Text: "j"}) // off ★
	m = feed(m, tea.KeyPressMsg{Code: 'k', Text: "k"}) // back on ★: a new scan, a new seq
	if m.star.seq == old.seq {
		t.Fatalf("leaving and re-entering ★ kept seq %d", old.seq)
	}
	m = feed(m, old)
	if m.star.cov.Scanned() != 0 || len(m.star.cov.Failed()) != 0 {
		t.Fatalf("result from the previous scan applied: scanned %d, failed %d", m.star.cov.Scanned(), len(m.star.cov.Failed()))
	}

	m = feed(m, scanOf(t, m, repos[0]))
	if m.star.cov.Scanned() != 1 {
		t.Fatalf("current-scan result dropped: scanned %d", m.star.cov.Scanned())
	}
}

// Seam: leaving ★ cancels the scan's context, so commands still in flight finish with context.Canceled and are ignored.
func TestStarLeavingCancelsInFlightScan(t *testing.T) {
	m := sized(t, 80, 24)
	m, cmd := update(m, keyMsg("r"))
	m = press(t, m, "j")
	msgs := exec(t, cmd)
	if scanMsgs(msgs) == 0 {
		t.Fatal("r issued no scan commands")
	}
	for _, msg := range msgs {
		sm, ok := msg.(renovateScannedMsg)
		if !ok {
			continue
		}
		if !errors.Is(sm.err, context.Canceled) {
			t.Errorf("scan of %v after leaving ★ ended with %v, want context.Canceled", sm.repo, sm.err)
		}
		m = feed(m, sm)
	}
	if m.status != "" || m.statusErr {
		t.Errorf("cancelled scan reached the status bar: %q", m.status)
	}
}

// countScans feeds msgs and everything they cause into m, counting scan commands' results.
func countScans(t *testing.T, m Model, msgs []tea.Msg) (Model, int) {
	t.Helper()
	n := 0
	for len(msgs) > 0 {
		if _, ok := msgs[0].(renovateScannedMsg); ok {
			n++
		}
		next, cmd := m.Update(msgs[0])
		m, msgs = next.(Model), append(msgs[1:], exec(t, cmd)...)
	}
	return m, n
}

// Seam: cached=true. Entering ★ counts cached repos as scanned and fetches only the misses.
func TestStarEntryFetchesOnlyCacheMisses(t *testing.T) {
	m := seeded(t)
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	repos, err := m.svc.Repos(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.svc.RenovateScan(m.ctx, repos[0]); err != nil {
		t.Fatal(err)
	}
	m, n := countScans(t, m, exec(t, m.Init()))
	if n != len(repos)-1 {
		t.Errorf("entering ★ fetched %d repos, want %d (all but the cached one)", n, len(repos)-1)
	}
	if !m.star.cov.Complete() {
		t.Errorf("coverage %d of %d after entry", m.star.cov.Scanned(), m.star.cov.Total())
	}
}

// Seam: cached=false. The five-minute tick while ★ is selected refetches every repo; elsewhere it doesn't touch ★.
func TestStarTickRefetchesAllOnlyOnStar(t *testing.T) {
	m := sized(t, 80, 24)
	next, cmd := m.Update(refreshTickMsg{})
	m, n := countScans(t, next.(Model), exec(t, cmd))
	if n != m.star.cov.Total() {
		t.Errorf("tick on ★ fetched %d repos, want all %d", n, m.star.cov.Total())
	}

	m = press(t, m, "j")
	next, cmd = m.Update(refreshTickMsg{})
	m = next.(Model)
	for _, msg := range exec(t, cmd) {
		if _, ok := msg.(renovateScannedMsg); ok {
			t.Fatal("tick off ★ issued a ★ scan")
		}
	}
	if m.star.cov != nil {
		t.Error("tick off ★ restarted the ★ scan")
	}
}

// Seam: box routing and focus keys inside ★.
func TestStarBoxFocusKeys(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "l")
	if m.star.focus != starByRepo {
		t.Fatalf("l entered box %d, want [1]", m.star.focus+1)
	}
	for _, tc := range []struct {
		keys string
		want starBox
	}{{"3", starPRs}, {"tab", starDashboards}, {"shift+tab", starPRs}, {"5", starCI}, {"tab", starByRepo}, {"shift+tab", starCI}} {
		m = press(t, m, tc.keys)
		if m.star.focus != tc.want {
			t.Errorf("after %q focus is [%d], want [%d]", tc.keys, m.star.focus+1, tc.want+1)
		}
	}
	m = press(t, m, "h")
	if m.level != levelRepos {
		t.Errorf("h from the boxes went to level %v", m.level)
	}
}

// Seam: box contents. [3] lists every Renovate PR oldest first, [5] only the ones whose CI isn't green,
// [4] the dashboards.
func TestStarBoxContents(t *testing.T) {
	m := sized(t, 80, 24)
	v := m.star.view
	var all, failing []string
	for _, mb := range v.PRs {
		all = append(all, fmt.Sprintf("%s#%d", mb.Repo.Name, mb.CR.Number))
	}
	for _, mb := range v.CI {
		failing = append(failing, fmt.Sprintf("%s#%d", mb.Repo.Name, mb.CR.Number))
	}
	if want := "infra#18 homelab#41 homelab#42 infra#17 lazyforge#3"; strings.Join(all, " ") != want {
		t.Errorf("[3] = %v, want %q (oldest first)", all, want)
	}
	if want := "homelab#41 infra#17"; strings.Join(failing, " ") != want {
		t.Errorf("[5] = %v, want %q (failing and running only)", failing, want)
	}
	if len(v.Dashboards) != 2 {
		t.Errorf("[4] has %d dashboards, want 2 (homelab, infra)", len(v.Dashboards))
	}

	m = press(t, m, "5")
	if s := screen(m); !strings.Contains(s, "home/infra #17") || strings.Contains(s, "home/lazyforge #3 ") {
		t.Errorf("[5] rows:\n%s", s)
	}
}

// Seam: [4] row text. One row per dashboard, with the count of unchecked entries that name a branch.
func TestStarDashboardRowShowsPendingCount(t *testing.T) {
	body := "## Awaiting Schedule\n\n- [ ] <!-- unlimit-branch=renovate/a -->Update a\n- [ ] <!-- unlimit-branch=renovate/b -->Update b\n" +
		"- [x] <!-- rebase-branch=renovate/c -->Update c\n- [ ] no branch here\n"
	f := rvFake("PR", rvRepo{ref: homelab, access: domain.AccessWrite, issues: []domain.Issue{{Number: 12, Title: "Dependency Dashboard", Body: body}}})
	m := rvSized(t, f, core.Options{}, 200, 40)
	m = press(t, m, "l", "4")
	if v := screen(m); !strings.Contains(v, "home/homelab ⚙ Dependency Dashboard") || !strings.Contains(v, "2 pending") {
		t.Errorf("[4] row:\n%s", v)
	}
}

// detailsPane is the right column of the frame, from the details box's left edge.
func detailsPane(m Model) string {
	ls := lines(m)
	col := -1
	for i, r := range []rune(ls[1]) {
		if r == '╭' {
			col = i
		}
	}
	var out []string
	for _, l := range ls[1:] {
		out = append(out, string([]rune(l)[min(col, len([]rune(l))):]))
	}
	return strings.Join(out, "\n")
}

// Seam: details per row kind.
func TestStarDetailsPerRowKind(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l")

	t.Run("repo row lists that repo's Renovate PRs", func(t *testing.T) {
		m := press(t, m, "1", "j", "l") // second row is homelab
		v := detailsPane(m)
		for _, want := range []string{"home/homelab", "#42", "#41", "update postgres docker tag", "update traefik docker tag"} {
			if !strings.Contains(v, want) {
				t.Errorf("details missing %q:\n%s", want, v)
			}
		}
		if strings.Contains(v, "infra #") {
			t.Errorf("details list another repo's PRs:\n%s", v)
		}
	})

	t.Run("group row lists ecosystem, update type, froms and members", func(t *testing.T) {
		m := press(t, m, "2", "l")
		v := screen(m)
		for _, want := range []string{"ecosystem: docker", "update: major", "from: 16.4", "home/homelab #42", "home/infra #17"} {
			if !strings.Contains(v, want) {
				t.Errorf("details missing %q:\n%s", want, v)
			}
		}
	})

	t.Run("PR row shows its updates", func(t *testing.T) {
		m := press(t, m, "3", "l")
		v := screen(m)
		if !strings.Contains(v, "Updates") || !strings.Contains(v, "opentofu 1.8.5 → 1.9.0") {
			t.Errorf("details missing updates for the oldest PR (infra #18):\n%s", v)
		}
		if strings.Contains(v, "Couldn't read the update table") {
			t.Errorf("parsed PR flagged unreadable:\n%s", v)
		}
	})

	t.Run("dashboard lists entries by section with check state", func(t *testing.T) {
		m := press(t, m, "4", "l")
		v := screen(m)
		for _, want := range []string{"Open", "Awaiting schedule", "☑ chore(deps): update postgres to v17.0 (#42)", "☐ chore(deps): update immich to v1.120.0"} {
			if !strings.Contains(v, want) {
				t.Errorf("details missing %q:\n%s", want, v)
			}
		}
	})
}

// Seam: a Renovate PR whose body can't be parsed is in [3] but not [2], and its details say so.
func TestStarUnreadablePRDetails(t *testing.T) {
	bad := rvPostgres(7, domain.CIPass)
	bad.Body = "no table here"
	f := rvFake("PR", rvRepo{ref: homelab, access: domain.AccessWrite, prs: []domain.ChangeRequest{bad}})
	m := rvSized(t, f, core.Options{}, 120, 40)
	if len(m.star.view.Groups) != 0 || len(m.star.view.PRs) != 1 {
		t.Fatalf("groups %d, PRs %d; want 0 and 1", len(m.star.view.Groups), len(m.star.view.PRs))
	}
	m = press(t, m, "l", "3", "l")
	if v := screen(m); !strings.Contains(v, "Couldn't read the update table") {
		t.Errorf("details for an unreadable PR:\n%s", v)
	}
}

// Seam: breadcrumb inside ★.
func TestStarBreadcrumb(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l", "2")
	want := "forge.home.arpa › ★ Renovate › [2] Updates by dependency › postgres 16.4 → 17.0"
	if got := strip(m.breadcrumb(120)); got != want {
		t.Errorf("breadcrumb %q, want %q", got, want)
	}
}

var wordPR = regexp.MustCompile(`\bPRs?\b`)

// Seam: terminology. A host that says MR never shows "PR" anywhere in ★, including the merge dialog.
func TestStarUsesHostTerm(t *testing.T) {
	f := rvFake("MR",
		rvRepo{ref: homelab, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: infra, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
	)
	m := rvSized(t, f, core.Options{}, 80, 24)
	check := func(name string) {
		t.Helper()
		if v := screen(m); wordPR.MatchString(v) {
			t.Errorf("%s shows 'PR' on an MR host:\n%s", name, v)
		}
	}
	check("preview")
	if !strings.Contains(screen(m), "[3] Renovate MRs") {
		t.Errorf("box [3] not titled with the host term:\n%s", screen(m))
	}
	m = press(t, m, "l", "3", "l")
	// The Renovate body is the bot's own text, not lazyforge's.
	if v := strings.ReplaceAll(screen(m), "This PR contains the following updates:", ""); wordPR.MatchString(v) {
		t.Errorf("details shows 'PR' on an MR host:\n%s", v)
	}
	m = press(t, m, "h", "2", "m")
	if m.dialog == nil {
		t.Fatal("no dialog")
	}
	if !strings.Contains(screen(m), "Merge 2 MRs") {
		t.Errorf("dialog title lacks the host term:\n%s", screen(m))
	}
	check("dialog")
}

// Seam: no ★ path loads boxes for a zero repo.
func TestStarNeverLoadsZeroRepo(t *testing.T) {
	m := sized(t, 80, 24)
	for _, k := range []string{"l", "1", "l", "h", "2", "l", "h", "3", "l", "h", "4", "5", "tab", "shift+tab", "G", "g", "g", "j", "k"} {
		next, cmd := m.Update(keyMsg(k))
		m = next.(Model)
		for _, msg := range exec(t, cmd) {
			switch msg := msg.(type) {
			case changeRequestsLoadedMsg, issuesLoadedMsg, runsLoadedMsg:
				t.Fatalf("key %q in ★ loaded repo boxes: %#v", k, msg)
			}
		}
	}
}

// Seam: zero repositories. The scan finishes at once and every box is empty rather than stuck on "Loading…".
func TestStarWithNoReposShowsEmptyBoxes(t *testing.T) {
	m := rvSized(t, rvFake("PR"), core.Options{}, 80, 24)
	v := screen(m)
	if strings.Contains(v, "Loading") {
		t.Errorf("empty host still loading:\n%s", v)
	}
	if n := strings.Count(v, "— none —"); n < 4 {
		t.Errorf("want empty boxes to say none, found %d:\n%s", n, v)
	}
	assertFits(t, m, "empty host")
}

// Seam: cached=false. If the repo list changes while an r refresh runs, the rescan it triggers still refetches every repo.
func TestStarRefreshStaysUncachedWhenRepoListChanges(t *testing.T) {
	m := sized(t, 80, 24)
	m, _ = update(m, keyMsg("r"))
	repos, err := m.svc.Repos(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	shorter := repos[:len(repos)-1]
	next, cmd := m.Update(reposLoadedMsg{repos: shorter})
	m = next.(Model)
	_, n := countScans(t, m, exec(t, cmd))
	if n != len(shorter) {
		t.Errorf("refetched %d of %d repos after the list changed during r, want all", n, len(shorter))
	}
}
