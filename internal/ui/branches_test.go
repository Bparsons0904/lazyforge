package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// noBranchesForge hides the BranchReader of the forge it wraps but keeps its ReadmeReader, so the Repo box still shows.
type noBranchesForge struct {
	forge.Forge
	rd forge.ReadmeReader
}

func (n noBranchesForge) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	return n.rd.GetReadme(ctx, r)
}

// branchOn is the sha-style commit fixture the Branches tab tests share.
func branchOn(sha, subject, author string, day time.Time) domain.Commit {
	return domain.Commit{SHA: sha, Message: subject, Author: author, Date: day}
}

var branchDay0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// branchFake is homelab with three branches. feature/x is the source of open PR #5, and the
// commits are seeded for main and feature/x only, so a/b#c/d shows "— none —".
func branchFake(t *testing.T) *forgetest.Fake {
	t.Helper()
	f := homelabFake(nil)
	f.AddBranch(homelab, domain.Branch{
		Name: "main", Default: true, WebURL: "https://f.test/home/homelab/src/branch/main",
		Commit: branchOn("1111111111111111111111111111111111111111", "Fix the build", "Ada", branchDay0),
	})
	f.AddBranch(homelab, domain.Branch{
		Name: "feature/x", WebURL: "https://f.test/home/homelab/src/branch/feature/x",
		Commit: branchOn("2222222222222222222222222222222222222222", "Add the x box", "Grace", branchDay0.AddDate(0, 0, 5)),
	})
	f.AddBranch(homelab, domain.Branch{
		Name: "a&b#c/d", WebURL: "https://f.test/home/homelab/src/branch/a&b%23c/d",
		Commit: branchOn("3333333333333333333333333333333333333333", "Odd name", "Linus", branchDay0.AddDate(0, 0, 3)),
	})
	f.SetCommits(homelab, "main", []domain.Commit{branchOn("1111111111111111111111111111111111111111", "Fix the build", "Ada", branchDay0)})
	f.SetCommits(homelab, "feature/x", []domain.Commit{
		branchOn("2222222222222222222222222222222222222222", "Add the x box", "Grace", branchDay0.AddDate(0, 0, 5)),
		branchOn("4444444444444444444444444444444444444444", "Start the x box", "Grace", branchDay0.AddDate(0, 0, 4)),
	})
	f.AddChangeRequest(homelab, domain.ChangeRequest{Number: 5, Title: "x box", State: domain.StateOpen, SourceBranch: "feature/x"})
	return f
}

// openBranchesTab returns a 120×40 model over f with homelab's Branches tab open in details.
func openBranchesTab(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, repoBox(t, f), "l", "]", "]")
}

func lineWith(v, sub string) string {
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}

func TestBranchesTabListsRowsWithDefaultAndPRMarker(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	v := screen(m)
	for _, want := range []string{"main", "feature/x", "a&b#c/d", "Fix the build", "Add the x box"} {
		if !strings.Contains(v, want) {
			t.Errorf("Branches tab missing %q:\n%s", want, v)
		}
	}
	if line := lineWith(v, "main"); !strings.Contains(line, "default") {
		t.Errorf("main row lacks the default marker: %q", line)
	}
	if line := lineWith(v, "feature/x"); !strings.Contains(line, "#5") {
		t.Errorf("feature/x row lacks the open PR #5 marker: %q", line)
	}
	if line := lineWith(v, "a&b#c/d"); strings.Contains(line, "#5") || strings.Contains(line, "default") {
		t.Errorf("a&b#c/d row carries a marker it should not: %q", line)
	}
}

func TestBranchesOrderDefaultFirstThenNewest(t *testing.T) {
	v := screen(openBranchesTab(t, branchFake(t)))
	main := strings.Index(v, "main")
	x := strings.Index(v, "feature/x")
	odd := strings.Index(v, "a&b#c/d")
	if main < 0 || main >= x || x >= odd {
		t.Errorf("row order main=%d feature/x=%d a&b#c/d=%d, want default, then newest, then older", main, x, odd)
	}
}

func TestBranchesCursorStopsAtBothEnds(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	m = press(t, m, "k")
	if m.details.branchCur != 0 {
		t.Errorf("k at the top: cursor %d, want 0", m.details.branchCur)
	}
	m = press(t, m, "j", "j", "j", "j", "j")
	if m.details.branchCur != 2 {
		t.Errorf("j past the bottom: cursor %d, want 2 (the last of three)", m.details.branchCur)
	}
}

func TestBranchesGAndGGJump(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	if m = press(t, m, "G"); m.details.branchCur != 2 {
		t.Errorf("G: cursor %d, want 2", m.details.branchCur)
	}
	if m = press(t, m, "g", "g"); m.details.branchCur != 0 {
		t.Errorf("gg: cursor %d, want 0", m.details.branchCur)
	}
}

func TestBranchesHalfPageStaysInRange(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	m = press(t, m, "ctrl+d")
	if m.details.branchCur < 0 || m.details.branchCur > 2 {
		t.Errorf("ctrl+d: cursor %d, want within 0..2", m.details.branchCur)
	}
	m = press(t, m, "ctrl+u", "ctrl+u")
	if m.details.branchCur != 0 {
		t.Errorf("ctrl+u past the top: cursor %d, want 0", m.details.branchCur)
	}
}

func TestBranchesCommitsFollowCursorAndShowLoading(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	if v := screen(m); !strings.Contains(v, "Fix the build") {
		t.Fatalf("default branch's commit missing before moving:\n%s", v)
	}

	next, msgs := step(t, m, "j")
	if v := screen(next); !strings.Contains(v, "Loading…") {
		t.Errorf("after j before the commits arrive, want Loading…:\n%s", v)
	}
	if counts(msgs)["commits"] != 1 {
		t.Errorf("j issued %v, want one commits load for feature/x", counts(msgs))
	}
	for _, msg := range msgs {
		next = run(t, next, msg)
	}
	v := screen(next)
	// "Start the x box" appears only in the Commits section; the branch row shows just the newest subject.
	for _, want := range []string{"Start the x box", "Grace"} {
		if !strings.Contains(v, want) {
			t.Errorf("feature/x commits missing %q after loading:\n%s", want, v)
		}
	}
}

func TestBranchesCommitsStaleMessagesDropped(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	m = run(t, m, commitsLoadedMsg{key: core.Key{Kind: core.KindCommits, Repo: infra, Ref: "main"}, commits: []domain.Commit{{Message: "ghost repo"}}})
	m = run(t, m, commitsLoadedMsg{key: core.Key{Kind: core.KindCommits, Repo: homelab, Ref: "gone"}, commits: []domain.Commit{{Message: "ghost branch"}}})
	v := screen(m)
	for _, ghost := range []string{"ghost repo", "ghost branch"} {
		if strings.Contains(v, ghost) {
			t.Errorf("stale commits %q were shown:\n%s", ghost, v)
		}
	}
}

func TestBranchesEmptyRepoIsSafe(t *testing.T) {
	var opened []string
	m := openBranchesTab(t, homelabFake(nil))
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	if v := screen(m); !strings.Contains(v, "No branches") {
		t.Errorf("empty repo missing its empty state:\n%s", v)
	}
	m = press(t, m, "j", "k", "G", "g", "g", "o")
	if len(opened) != 0 {
		t.Errorf("o on an empty list opened %v, want nothing", opened)
	}
}

func TestBranchesWithoutCommitsShowsNone(t *testing.T) {
	f := branchFake(t)
	f.AddBranch(homelab, domain.Branch{
		Name: "lonely", WebURL: "https://f.test/home/homelab/src/branch/lonely",
		Commit: branchOn("5555555555555555555555555555555555555555", "No commits yet", "Linus", branchDay0.AddDate(0, 0, -9)),
	})
	m := press(t, openBranchesTab(t, f), "G")
	if v := screen(m); !strings.Contains(v, "— none —") {
		t.Errorf("branch without commits missing %q:\n%s", "— none —", v)
	}
}

func TestBranchesUnavailableOnHost(t *testing.T) {
	var opened []string
	f := homelabFake(nil)
	m := openBranchesTab(t, noBranchesForge{Forge: f, rd: f})
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	if v := screen(m); !strings.Contains(v, "Branches aren't available on this host") {
		t.Errorf("missing the unavailable message:\n%s", v)
	}
	m = press(t, m, "j", "k", "o")
	if len(opened) != 0 {
		t.Errorf("o opened %v on a host without branches", opened)
	}
}

func TestBranchesOpenCursorBranchInBrowser(t *testing.T) {
	var opened []string
	m := openBranchesTab(t, branchFake(t))
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	m = press(t, m, "j", "o")
	want := []string{"https://f.test/home/homelab/src/branch/feature/x"}
	if strings.Join(opened, " ") != strings.Join(want, " ") {
		t.Errorf("o opened %v, want %v", opened, want)
	}
}

func TestBranchesOpenIsDisabledOutsideBranchesTab(t *testing.T) {
	var opened []string
	open := func(m Model) Model {
		m.openURL = func(u string) error {
			opened = append(opened, u)
			return nil
		}
		return m
	}
	f := branchFake(t)

	press(t, open(repoBox(t, f)), "o")
	press(t, open(readmeTab(t, f)), "o")
	press(t, open(press(t, readmeTab(t, f), "]")), "o")

	if len(opened) != 0 {
		t.Errorf("o outside the Branches tab opened %v, want nothing", opened)
	}
}

func TestBranchesHaveNoChangeRequestActions(t *testing.T) {
	m := openBranchesTab(t, branchFake(t))
	m = press(t, m, "j")
	for _, k := range []string{"m", "a", "x", "c", "L", "R", "space"} {
		m = press(t, m, k)
		if m.dialog != nil {
			t.Fatalf("key %q opened a dialog on a branch: %+v", k, m.dialog)
		}
	}
}

func TestBranchesRefreshFollowsCursorByName(t *testing.T) {
	m := press(t, openBranchesTab(t, branchFake(t)), "j")
	if m.details.branchCur != 1 {
		t.Fatalf("setup: cursor %d, want 1 on feature/x", m.details.branchCur)
	}
	reordered := []domain.Branch{
		{Name: "main", Default: true, Commit: branchOn("1111111111111111111111111111111111111111", "Fix the build", "Ada", branchDay0)},
		{Name: "a&b#c/d", Commit: branchOn("3333333333333333333333333333333333333333", "Odd name", "Linus", branchDay0.AddDate(0, 0, 9))},
		{Name: "feature/x", Commit: branchOn("2222222222222222222222222222222222222222", "Add the x box", "Grace", branchDay0.AddDate(0, 0, 5))},
	}
	m = run(t, m, branchesLoadedMsg{key: core.Key{Kind: core.KindBranches, Repo: homelab}, branches: reordered})
	if m.details.branchCur != 2 {
		t.Errorf("after refresh cursor %d, want 2 (feature/x moved down)", m.details.branchCur)
	}
	if v := screen(m); !strings.Contains(v, "Start the x box") {
		t.Errorf("already-loaded commits dropped on refresh:\n%s", v)
	}
}

func TestBranchesRefreshClampsToLastRow(t *testing.T) {
	m := press(t, openBranchesTab(t, branchFake(t)), "G")
	shorter := []domain.Branch{
		{Name: "main", Default: true, Commit: branchOn("1111111111111111111111111111111111111111", "Fix the build", "Ada", branchDay0)},
		{Name: "feature/x", Commit: branchOn("2222222222222222222222222222222222222222", "Add the x box", "Grace", branchDay0.AddDate(0, 0, 5))},
	}
	m = run(t, m, branchesLoadedMsg{key: core.Key{Kind: core.KindBranches, Repo: homelab}, branches: shorter})
	if m.details.branchCur != 1 {
		t.Errorf("cursor %d after the last branch vanished, want 1 (last row)", m.details.branchCur)
	}

	m = run(t, m, branchesLoadedMsg{key: core.Key{Kind: core.KindBranches, Repo: homelab}, branches: shorter[:1]})
	if m.details.branchCur != 0 {
		t.Errorf("cursor %d after the list shrank to one, want 0", m.details.branchCur)
	}
}

func TestBranchesTextFitsTheBox(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	long := strings.Repeat("a very long commit subject that keeps going ", 8)
	bs := branchesState{
		ok: true,
		list: []domain.Branch{
			{Name: "main", Default: true, Commit: branchOn("1111111111111111111111111111111111111111", long, "Ada", now)},
			{Name: "feature/" + strings.Repeat("x", 80), Commit: branchOn("2222222222222222222222222222222222222222", "short", "Grace", now)},
		},
		commits: map[string][]domain.Commit{
			"main": {branchOn("1111111111111111111111111111111111111111", long, "Ada", now)},
		},
	}
	for _, cw := range []int{8, 20, 40, 120} {
		for _, ch := range []int{0, 1, 2, 3, 50} {
			var d details
			text := d.branchesText(bs, nil, true, true, cw, ch, now)
			if ch == 0 {
				if text != "" {
					t.Errorf("cw %d ch 0: got %q, want empty", cw, text)
				}
				continue
			}
			rows := strings.Split(text, "\n")
			if len(rows) != ch {
				t.Errorf("cw %d ch %d: %d lines, want %d", cw, ch, len(rows), ch)
			}
			for _, row := range rows {
				if w := lipgloss.Width(row); w > cw {
					t.Errorf("cw %d ch %d: line is %d wide: %q", cw, ch, w, row)
				}
			}
		}
	}
}
