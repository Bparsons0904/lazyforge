package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	repoA = domain.RepoRef{Owner: "o", Name: "a"}
	repoB = domain.RepoRef{Owner: "o", Name: "b"}
	repoC = domain.RepoRef{Owner: "o", Name: "c"}
	repoD = domain.RepoRef{Owner: "o", Name: "d"}
)

func mergedRefs(f *forgetest.Fake) []string {
	var out []string
	for _, mu := range f.Mutations() {
		if mu.Op == "merge" {
			out = append(out, fmt.Sprintf("%s#%d", mu.Item.Repo, mu.Item.Number))
		}
	}
	return out
}

func hasMark(m Model, r domain.RepoRef, n int) bool { return m.star.marked[starTarget{r, n}] }

func inView(m Model, r domain.RepoRef, n int) bool {
	for _, mb := range m.star.view.PRs {
		if mb.Repo == r && mb.CR.Number == n {
			return true
		}
	}
	return false
}

// pinForge answers rechecks with a new head SHA and reports the SHA each merge was pinned to.
type pinForge struct {
	*forgetest.Fake
	mu     sync.Mutex
	pinned map[int]string
}

func (p *pinForge) GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error) {
	cr, err := p.Fake.GetChangeRequest(ctx, r, n)
	cr.HeadSHA = "rechecked-" + cr.HeadSHA
	return cr, err
}

func (p *pinForge) Merge(ctx context.Context, r domain.RepoRef, n int, o forge.MergeOpts) error {
	p.mu.Lock()
	p.pinned[n] = o.HeadSHA
	p.mu.Unlock()
	return p.Fake.Merge(ctx, r, n, o)
}

// Seam: D11 confirm. Merge is called with the SHA from this cycle's recheck, not the one the scan saw.
func TestStarMergePinsRecheckedHeadSHA(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
	)
	pf := &pinForge{Fake: f, pinned: map[int]string{}}
	m := rvSized(t, pf, core.Options{}, 120, 40)
	press(t, m, "l", "2", "m", "y")
	if pf.pinned[1] != "rechecked-sha-pg-1" || pf.pinned[2] != "rechecked-sha-pg-2" {
		t.Errorf("merges pinned to %v, want the rechecked SHAs", pf.pinned)
	}
}

// Seam: dedup is per (repo, number). Two repos with a PR #1 each are both merged.
func TestStarMergeKeepsSameNumberInDifferentRepos(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
	)
	m := rvSized(t, f, core.Options{}, 120, 40)
	m = press(t, m, "l", "2", "m")
	if m.dialog == nil || len(m.dialog.targets) != 2 {
		t.Fatalf("dialog %+v, want two targets", m.dialog)
	}
	m = press(t, m, "y")
	if got := strings.Join(mergedRefs(f), " "); !strings.Contains(got, "o/a#1") || !strings.Contains(got, "o/b#1") || len(f.Mutations()) != 2 {
		t.Errorf("merged %q, want o/a#1 and o/b#1", got)
	}
}

// Seam: write access. A target in a read-only repo is skipped with the forge's reason and never reaches Merge.
func TestStarMergeSkipsReadOnlyRepos(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessRead, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
		rvRepo{ref: repoC, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(3, domain.CIPass)}},
	)
	m := rvSized(t, f, core.Options{}, 120, 40)
	m = press(t, m, "l", "2", "m")
	if m.dialog == nil || len(m.dialog.targets) != 2 {
		t.Fatalf("dialog %+v, want the two writable targets", m.dialog)
	}
	v := screen(m)
	if !regexp.MustCompile(`Skipped o/b #2: .*write access`).MatchString(v) {
		t.Errorf("no skipped line with the reason for o/b #2:\n%s", v)
	}
	m = press(t, m, "y")
	if got := mergedRefs(f); len(got) != 2 || strings.Contains(strings.Join(got, " "), "o/b") {
		t.Errorf("merged %v, want only o/a#1 and o/c#3", got)
	}
	if m.status != "Merged 2 of 2" {
		t.Errorf("status %q", m.status)
	}
	if !inView(m, repoB, 2) {
		t.Error("the skipped read-only PR left the view")
	}
}

// Seam: key enablement. With no mergeable target m and space are off, so nothing opens or changes.
func TestStarKeysOffForReadOnlyTargets(t *testing.T) {
	f := rvFake("PR", rvRepo{ref: repoB, access: domain.AccessRead, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}})
	m := rvSized(t, f, core.Options{}, 120, 40)
	m = press(t, m, "l")
	for _, box := range []string{"2", "3", "5"} {
		m = press(t, m, box)
		if m.keys.Merge.Enabled() || m.keys.Mark.Enabled() {
			t.Errorf("box [%s]: merge enabled %v, mark enabled %v; want both off for a read-only repo", box, m.keys.Merge.Enabled(), m.keys.Mark.Enabled())
		}
		m = press(t, m, "m", "space")
		if m.dialog != nil || len(m.star.marked) != 0 || len(f.Mutations()) != 0 {
			t.Errorf("box [%s]: m or space acted on a read-only target", box)
		}
	}
}

// Seam: key enablement per box with a writable target under the cursor.
func TestStarKeyEnablementPerBox(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l")
	for _, tc := range []struct {
		box         string
		merge, mark bool
	}{{"1", false, false}, {"2", true, false}, {"3", true, true}, {"4", false, false}, {"5", true, true}} {
		m = press(t, m, tc.box)
		if m.keys.Merge.Enabled() != tc.merge || m.keys.Mark.Enabled() != tc.mark {
			t.Errorf("box [%s]: merge %v mark %v, want %v and %v", tc.box, m.keys.Merge.Enabled(), m.keys.Mark.Enabled(), tc.merge, tc.mark)
		}
	}
}

// Seam: nothing left to merge. When every target was merged elsewhere no dialog opens, the status bar says why,
// and the merged PRs leave the view.
func TestStarMergeWithNothingLeftExplainsInStatusBar(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
	)
	m := rvSized(t, f, core.Options{}, 120, 40)
	for _, mb := range m.star.view.PRs {
		if err := f.Merge(context.Background(), mb.Repo, mb.CR.Number, forge.MergeOpts{HeadSHA: mb.CR.HeadSHA}); err != nil {
			t.Fatal(err)
		}
	}
	before := len(f.Mutations())
	m = press(t, m, "l", "2", "m")
	if m.dialog != nil {
		t.Fatal("dialog opened with nothing left to merge")
	}
	if !m.statusErr || !strings.Contains(m.status, "o/a #1") || !strings.Contains(m.status, "o/b #2") {
		t.Errorf("status %q (err %v), want it to explain why nothing can merge", m.status, m.statusErr)
	}
	if len(f.Mutations()) != before {
		t.Error("a merge ran for targets that are no longer open")
	}
	if inView(m, repoA, 1) || inView(m, repoB, 2) {
		t.Error("already-merged PRs still shown")
	}
}

// Seam: failed rechecks. A target whose recheck fails is listed as "<repo> #n: recheck failed" and not merged.
func TestStarFailedRecheckIsListedAndNotMerged(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
	)
	m := rvSized(t, f, core.Options{}, 120, 40)
	m = press(t, m, "l", "2")
	f.FailNext(fmt.Errorf("forge down"))
	m = press(t, m, "m")
	if m.dialog == nil || len(m.dialog.targets) != 1 {
		t.Fatalf("dialog %+v, want the one target whose recheck worked", m.dialog)
	}
	v := screen(m)
	if !regexp.MustCompile(`Skipped o/[ab] #[12]: recheck failed`).MatchString(v) {
		t.Errorf("dialog doesn't list the failed recheck:\n%s", v)
	}
	failed := "o/a #1"
	if m.dialog.targets[0].Repo == repoA {
		failed = "o/b #2"
	}
	if !strings.Contains(v, failed+": recheck failed") {
		t.Errorf("the failed target should be %s:\n%s", failed, v)
	}
	m = press(t, m, "y")
	if len(mergedRefs(f)) != 1 {
		t.Errorf("merged %v, want only the target that rechecked", mergedRefs(f))
	}
}

// partialModel returns a model over 4 repos where o/a and o/b hold the postgres group, o/c failed to scan
// and o/d never answered.
func partialModel(t *testing.T, term string) Model {
	t.Helper()
	f := rvFake(term,
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}},
		rvRepo{ref: repoC, access: domain.AccessWrite},
		rvRepo{ref: repoD, access: domain.AccessWrite},
	)
	m := New(context.Background(), core.New(f, core.Options{}))
	m = run(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	repos, err := m.svc.Repos(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	m = feed(m, reposLoadedMsg{repos: repos})
	byRef := map[domain.RepoRef]domain.Repo{}
	for _, r := range repos {
		byRef[r.RepoRef] = r
	}
	m = feed(m, scanOf(t, m, byRef[repoA]))
	m = feed(m, scanOf(t, m, byRef[repoB]))
	return feed(m, renovateScannedMsg{seq: m.star.seq, repo: repoC, err: fmt.Errorf("boom")})
}

// Seam: coverage warning. A [2] group merge over an incomplete scan names how many repos weren't covered,
// counting both unscanned and failed ones, and uses the host's word for a change request.
func TestStarGroupMergeWarnsOnIncompleteCoverage(t *testing.T) {
	for _, term := range []string{"PR", "MR"} {
		m := partialModel(t, term)
		m = press(t, m, "l", "2", "m")
		if m.dialog == nil {
			t.Fatalf("%s: no dialog", term)
		}
		want := fmt.Sprintf("2 repositories not scanned; this may not be every %s for this update", term)
		if v := screen(m); !strings.Contains(v, want) {
			t.Errorf("%s: dialog missing %q:\n%s", term, want, v)
		}
	}
}

// Seam: the coverage warning belongs to group merges only, and only while coverage is incomplete.
func TestStarCoverageWarningOnlyForGroupsAndGaps(t *testing.T) {
	m := partialModel(t, "PR")
	m = press(t, m, "l", "3", "m")
	if m.dialog == nil {
		t.Fatal("no dialog from [3]")
	}
	if strings.Contains(screen(m), "not scanned") {
		t.Errorf("[3] merge shows the group coverage warning:\n%s", screen(m))
	}

	full := sized(t, 120, 40)
	full = press(t, full, "l", "2", "m")
	if full.dialog == nil || strings.Contains(screen(full), "not scanned") {
		t.Errorf("complete scan shows a coverage warning:\n%s", screen(full))
	}
}

// Seam: per-repo green-CI. A failing PR is refused where its repo requires green CI and merges anyway where not.
func TestStarMergeHonoursGreenCIPerRepo(t *testing.T) {
	f := rvFake("PR",
		rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIFail)}},
		rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIFail)}},
	)
	opts := core.Options{RequireGreenCI: func(r domain.RepoRef) bool { return r == repoA }}
	m := rvSized(t, f, opts, 120, 40)
	m = press(t, m, "l", "2", "m")
	if m.dialog == nil {
		t.Fatal("no dialog")
	}
	ls := lines(m)
	window := func(label string) string {
		for i, l := range ls {
			if strings.Contains(l, label) {
				return strings.Join(ls[i:min(i+4, len(ls))], "\n")
			}
		}
		t.Fatalf("no line for %s:\n%s", label, screen(m))
		return ""
	}
	if w := window("o/a #1"); !strings.Contains(w, "will be refused") {
		t.Errorf("o/a (green CI required) not flagged as refused:\n%s", w)
	}
	if w := window("o/b #2"); !strings.Contains(w, "will merge anyway") || strings.Contains(w, "will be refused") {
		t.Errorf("o/b (green CI not required) not flagged as merging anyway:\n%s", w)
	}

	m = press(t, m, "y")
	if got := mergedRefs(f); len(got) != 1 || got[0] != "o/b#2" {
		t.Errorf("merged %v, want only o/b#2", got)
	}
	if v := screen(m); !strings.Contains(v, "refused") || !strings.Contains(v, "merged") {
		t.Errorf("done dialog lacks per-target outcomes:\n%s", v)
	}
	if m.status != "Merged 1 of 2" {
		t.Errorf("status %q, want 'Merged 1 of 2'", m.status)
	}
	if !hasMark(m, repoA, 1) || hasMark(m, repoB, 2) {
		t.Errorf("marks %v: the refused PR must be ★-marked and the merged one not", m.star.marked)
	}
	if !inView(m, repoA, 1) || inView(m, repoB, 2) {
		t.Error("merged PR should leave [3] and the refused one should stay")
	}
	for _, g := range m.star.view.Groups {
		for _, mb := range g.Members {
			if mb.Repo == repoB {
				t.Error("merged PR still in [2]")
			}
		}
	}
	for _, mb := range m.star.view.CI {
		if mb.Repo == repoB {
			t.Error("merged PR still in [5]")
		}
	}
}

// Seam: marks. Shared between [3] and [5], keyed by (repo, number), and toggled from either box.
func TestStarMarksAreSharedAndToggleFromEitherBox(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l", "5", "space") // first [5] row is homelab #41
	if !hasMark(m, homelab, 41) || len(m.star.marked) != 1 {
		t.Fatalf("marks %v after marking in [5]", m.star.marked)
	}
	m = press(t, m, "3", "j") // second [3] row is homelab #41 too
	if !strings.Contains(screen(m), "◆ home/homelab #41") {
		t.Errorf("mark from [5] not visible in [3]:\n%s", screen(m))
	}
	m = press(t, m, "space")
	if len(m.star.marked) != 0 {
		t.Errorf("space in [3] didn't unmark the PR marked in [5]: %v", m.star.marked)
	}
}

// Seam: space only marks PR rows in [3] and [5].
func TestStarSpaceIgnoredOutsidePRBoxes(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l")
	for _, box := range []string{"1", "2", "4"} {
		m = press(t, m, box, "space")
		if len(m.star.marked) != 0 {
			t.Errorf("space marked something in [%s]: %v", box, m.star.marked)
		}
	}
}

// Seam: m on [3] merges the marked set across repos, not the row under the cursor.
func TestStarMergesMarkedSetAcrossRepos(t *testing.T) {
	f := newDemo()
	m := New(context.Background(), core.New(f, core.Options{}))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	m = sizedWith(t, m, 120, 40)
	m = press(t, m, "l", "3", "space", "j", "space", "j") // [3] rows: infra #18, homelab #41, homelab #42
	m = press(t, m, "m")
	if m.dialog == nil || len(m.dialog.targets) != 2 {
		t.Fatalf("dialog %+v, want the two marked targets", m.dialog)
	}
	m = press(t, m, "y")
	got := strings.Join(mergedRefs(f), " ")
	if len(f.Mutations()) != 2 || !strings.Contains(got, "home/infra#18") || !strings.Contains(got, "home/homelab#41") {
		t.Errorf("merged %q, want home/infra#18 and home/homelab#41", got)
	}
	if len(m.star.marked) != 0 {
		t.Errorf("merged PRs left marked: %v", m.star.marked)
	}
	if inView(m, infra, 18) || inView(m, homelab, 41) || !inView(m, homelab, 42) {
		t.Error("view not rebuilt: merged PRs should be gone and the unmarked one kept")
	}
}

// Seam: a PR marked in both boxes is one target, merged once.
func TestStarMarkedInBothBoxesMergesOnce(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l", "5", "space", "3", "j", "m")
	if m.dialog == nil || len(m.dialog.targets) != 1 {
		t.Fatalf("dialog %+v, want one target", m.dialog)
	}
}

// Seam: m on [2] merges the group under the cursor and ignores marks.
func TestStarGroupMergeIgnoresMarks(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l", "3", "space", "2", "m") // mark infra #18, then merge the postgres group
	if m.dialog == nil || len(m.dialog.targets) != 2 {
		t.Fatalf("dialog %+v, want the two group members", m.dialog)
	}
	for _, tg := range m.dialog.targets {
		if tg.CR.Number == 18 {
			t.Errorf("marked PR %v leaked into the group merge", tg.Repo)
		}
	}
}

// Seam: esc in the ★ boxes clears every mark.
func TestStarEscClearsMarks(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "l", "3", "space", "j", "space", "esc")
	if len(m.star.marked) != 0 {
		t.Errorf("esc left marks %v", m.star.marked)
	}
}

// Seam: a failed merge (not a refusal) also leaves the target marked, so it can be retried.
func TestStarFailedMergeStaysMarked(t *testing.T) {
	f := rvFake("PR", rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}})
	m := rvSized(t, f, core.Options{}, 120, 40)
	m = press(t, m, "l", "2", "m")
	if m.dialog == nil {
		t.Fatal("no dialog")
	}
	f.FailNext(fmt.Errorf("500 from forge"))
	m = press(t, m, "y")
	if len(mergedRefs(f)) != 0 || !hasMark(m, repoA, 1) || m.status != "Merged 0 of 1" {
		t.Errorf("merged %v, marks %v, status %q; want nothing merged, o/a #1 marked, 'Merged 0 of 1'", mergedRefs(f), m.star.marked, m.status)
	}
}

// Seam: regular-repo flow. A single-repo dialog has no repo prefix, lists the Renovate updates, and says
// "packages unknown" for an unreadable body only when the PR is a Renovate PR by branch or by renovate_user.
func TestRegularRepoMergeDialog(t *testing.T) {
	bot := func(branch string) domain.ChangeRequest {
		cr := rvPostgres(1, domain.CIPass)
		cr.Author, cr.SourceBranch, cr.Body = "botty", branch, "no table"
		return cr
	}
	for _, tc := range []struct {
		name    string
		cr      domain.ChangeRequest
		user    string
		updates bool
		unknown bool
	}{
		{"parsed Renovate PR lists its updates", rvPostgres(1, domain.CIPass), "", true, false},
		{"unreadable body, detected by branch", bot("renovate/x"), "", false, true},
		{"unreadable body, detected by renovate_user", bot("feature/x"), "botty", false, true},
		{"unreadable body, not a Renovate PR", bot("feature/x"), "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := rvFake("PR", rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{tc.cr}})
			m := New(context.Background(), core.New(f, core.Options{RenovateUser: tc.user}))
			m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
			m = press(t, sizedWith(t, m, 120, 40), "j", "l", "m")
			if m.dialog == nil {
				t.Fatal("no dialog")
			}
			v := screen(m)
			if strings.Contains(v, "o/a #1") || !strings.Contains(v, "#1 ") {
				t.Errorf("single-repo dialog must list '#n title' without the repo:\n%s", v)
			}
			if got := strings.Contains(v, "postgres 16.4 → 17.0"); got != tc.updates {
				t.Errorf("lists updates = %v, want %v:\n%s", got, tc.updates, v)
			}
			if got := strings.Contains(v, "packages unknown"); got != tc.unknown {
				t.Errorf("'packages unknown' = %v, want %v:\n%s", got, tc.unknown, v)
			}
		})
	}
}
