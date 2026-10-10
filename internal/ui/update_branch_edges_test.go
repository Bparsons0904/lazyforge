package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// wideAtCRs is atCRs at 200 columns, so the status bar keeps the u hint that a 120-column bar clips.
func wideAtCRs(t *testing.T, f forge.Forge) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, core.Options{}))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	return press(t, sizedWith(t, m, 200, 40), "j", "l")
}

// starUpdateFake is repo B with one failing Renovate PR, so it shows in both the [3] and the [5] box.
func starUpdateFake(access domain.Access) *forgetest.Fake {
	return rvFake("PR", rvRepo{ref: repoB, access: access, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIFail)}})
}

// starDashboardFake is starUpdateFake plus a Dependency Dashboard issue in repo B, so the [4] box has a row whose item is an issue.
func starDashboardFake(access domain.Access) *forgetest.Fake {
	body := "## Awaiting Schedule\n\n- [ ] <!-- unlimit-branch=renovate/a -->Update a\n"
	return rvFake("PR", rvRepo{
		ref: repoB, access: access, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)},
		issues: []domain.Issue{{Number: 12, Title: "Dependency Dashboard", Body: body}},
	})
}

func TestUpdateStarRowDisabledWithoutStyles(t *testing.T) {
	f := starUpdateFake(domain.AccessWrite)
	f.SetUpdateStyles()
	for _, box := range []string{"3", "5"} {
		m := press(t, rvSized(t, f, core.Options{}, 200, 40), "l", box)
		if m.keys.UpdateBranch.Enabled() {
			t.Errorf("[%s] u enabled on a ★ PR row although the forge advertises no styles", box)
		}
		if strings.Contains(strip(m.statusBar()), "u update") {
			t.Errorf("[%s] u update hinted although the forge advertises no styles: %s", box, strip(m.statusBar()))
		}
		next, cmd := update(m, keyMsg("u"))
		if next.dialog != nil || cmd != nil {
			t.Errorf("[%s] u produced dialog %v / cmd %v with no styles", box, next.dialog, cmd)
		}
	}
}

func TestUpdateStarRowsNeedWriteAccess(t *testing.T) {
	f := starUpdateFake(domain.AccessRead)
	for _, box := range []string{"3", "5"} {
		m := press(t, rvSized(t, f, core.Options{}, 200, 40), "l", box)
		if m.keys.UpdateBranch.Enabled() || strings.Contains(strip(m.statusBar()), "u update") {
			t.Errorf("[%s] u offered on a ★ PR row in a read-only repo: %s", box, strip(m.statusBar()))
		}
		if next, cmd := update(m, keyMsg("u")); next.dialog != nil || cmd != nil {
			t.Errorf("[%s] u opened a dialog in a read-only repo", box)
		}
	}
}

func TestUpdateHintOnStarPRAndCIBoxes(t *testing.T) {
	f := starUpdateFake(domain.AccessWrite)
	for _, box := range []string{"3", "5"} {
		m := press(t, rvSized(t, f, core.Options{}, 200, 40), "l", box)
		if !m.keys.UpdateBranch.Enabled() {
			t.Errorf("[%s] u disabled on an open ★ PR row with write access", box)
		}
		if !strings.Contains(strip(m.statusBar()), "u update") {
			t.Errorf("[%s] u update missing from the hints: %s", box, strip(m.statusBar()))
		}
		m = openUpdate(t, m)
		if s := screen(m); !strings.Contains(s, "Update #2") || !strings.Contains(s, "› merge") {
			t.Errorf("[%s] dialog for ★ row:\n%s", box, s)
		}
	}
}

func TestUpdateHiddenOnStarGroupsByRepoAndDashboards(t *testing.T) {
	f := starDashboardFake(domain.AccessWrite)
	for _, box := range []string{"1", "2", "4"} {
		m := press(t, rvSized(t, f, core.Options{}, 200, 40), "l", box)
		if n := map[string]int{"1": len(m.star.view.ByRepo), "2": len(m.star.view.Groups), "4": len(m.star.view.Dashboards)}[box]; n == 0 {
			t.Fatalf("[%s] has no rows, so the gating is not exercised", box)
		}
		if m.keys.UpdateBranch.Enabled() {
			t.Errorf("[%s] u enabled on a ★ row that is not a PR", box)
		}
		if strings.Contains(strip(m.statusBar()), "u update") {
			t.Errorf("[%s] u update hinted on a ★ row that is not a PR: %s", box, strip(m.statusBar()))
		}
		if next, cmd := update(m, keyMsg("u")); next.dialog != nil || cmd != nil {
			t.Errorf("[%s] u did something on a ★ row that is not a PR", box)
		}
	}
}

func TestUpdateHintAtPRDetailsLevel(t *testing.T) {
	m := press(t, wideAtCRs(t, updateFake(domain.AccessWrite)), "l")
	if m.level != levelDetails {
		t.Fatalf("l from the PR box went to level %v, want details", m.level)
	}
	if !m.keys.UpdateBranch.Enabled() || !strings.Contains(strip(m.statusBar()), "u update") {
		t.Errorf("u not offered on an open PR at details level: %s", strip(m.statusBar()))
	}
	m = openUpdate(t, m)
	if s := screen(m); !strings.Contains(s, "Update #1") {
		t.Errorf("dialog did not open from details:\n%s", s)
	}
}

func TestUpdateHiddenOnNonPRBoxesAndLevels(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		box  boxKind
	}{
		{"issues", []string{"2"}, boxIssues},
		{"runs", []string{"3"}, boxRuns},
		{"releases", []string{"5"}, boxReleases},
	} {
		m := press(t, wideAtCRs(t, updateFake(domain.AccessWrite)), tc.keys...)
		if m.boxes.focus != tc.box {
			t.Fatalf("%s: focus is box %d, want %d", tc.name, m.boxes.focus, tc.box)
		}
		if m.keys.UpdateBranch.Enabled() || strings.Contains(strip(m.statusBar()), "u update") {
			t.Errorf("%s: u offered: %s", tc.name, strip(m.statusBar()))
		}
		if next, cmd := update(m, keyMsg("u")); next.dialog != nil || cmd != nil {
			t.Errorf("%s: u did something", tc.name)
		}
	}

	m := press(t, wideAtCRs(t, updateFake(domain.AccessWrite)), "h")
	if m.level != levelRepos {
		t.Fatalf("h from the PR box went to level %v, want repos", m.level)
	}
	if m.keys.UpdateBranch.Enabled() || strings.Contains(strip(m.statusBar()), "u update") {
		t.Errorf("u offered on the repo list: %s", strip(m.statusBar()))
	}
}

func TestUpdateDialogHintsForTwoStyles(t *testing.T) {
	m := openUpdate(t, atCRs(t, updateFake(domain.AccessWrite), core.Options{}))
	for _, want := range []string{"j/k choose", "y/enter confirm", "esc cancel"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("two-style dialog lacks hint %q:\n%s", want, screen(m))
		}
	}
}

func TestUpdateDialogLinesInOrder(t *testing.T) {
	m := openUpdate(t, atCRs(t, updateFake(domain.AccessWrite), core.Options{}))
	ls := lines(m)
	steps := []struct {
		want string
		ci   bool
	}{
		{"Update #1", false},
		{"#1 pr1", true},
		{"Update with the latest main:", false},
		{"› merge", false},
		{"  rebase", false},
	}
	from := 0
	for _, s := range steps {
		i := from
		for ; i < len(ls); i++ {
			if strings.Contains(ls[i], s.want) {
				break
			}
		}
		if i == len(ls) {
			t.Fatalf("dialog has no line %q after line %d:\n%s", s.want, from, screen(m))
		}
		if s.ci && !strings.Contains(ls[i], "✓") {
			t.Errorf("line %q lacks the CI icon: %q", s.want, ls[i])
		}
		from = i + 1
	}
}

func TestUpdateEnterConfirmsWithCursorStyle(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	m = press(t, m, "j", "enter")
	if m.dialog != nil {
		t.Error("enter left the update dialog open")
	}
	ms := f.Mutations()
	if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Style != forge.UpdateRebase {
		t.Fatalf("mutations = %+v, want one update-branch with rebase", ms)
	}
	if m.status != "Updated #1 (rebase)" || m.statusErr {
		t.Errorf("status = %q (err %v), want Updated #1 (rebase)", m.status, m.statusErr)
	}
}

func TestUpdateConfirmFromDetailsUpdatesPR(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := press(t, atCRs(t, f, core.Options{}), "l")
	m = press(t, openUpdate(t, m), "y")
	ms := f.Mutations()
	if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Item.Number != 1 || ms[0].Style != forge.UpdateMerge {
		t.Fatalf("mutations = %+v, want one merge update of #1", ms)
	}
	if m.status != "Updated #1 (merge)" || m.statusErr {
		t.Errorf("status = %q (err %v), want Updated #1 (merge)", m.status, m.statusErr)
	}
}

func TestUpdateStarRowReseedsStarView(t *testing.T) {
	f := starUpdateFake(domain.AccessWrite)
	m := press(t, rvSized(t, f, core.Options{}, 120, 40), "l", "3")
	before, err := f.GetChangeRequest(t.Context(), repoB, 2)
	if err != nil {
		t.Fatal(err)
	}
	m = press(t, openUpdate(t, m), "y")
	fresh, err := f.GetChangeRequest(t.Context(), repoB, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.HeadSHA == before.HeadSHA || fresh.CI != domain.CIPending {
		t.Fatalf("fake did not advance: head %q -> %q, CI %v", before.HeadSHA, fresh.HeadSHA, fresh.CI)
	}
	var found bool
	for _, mb := range m.star.view.PRs {
		if mb.Repo != repoB || mb.CR.Number != 2 {
			continue
		}
		found = true
		if mb.CR.HeadSHA != fresh.HeadSHA || mb.CR.CI != domain.CIPending {
			t.Errorf("★ row for repo B #2 has head %q CI %v, want head %q CI pending", mb.CR.HeadSHA, mb.CR.CI, fresh.HeadSHA)
		}
	}
	if !found {
		t.Error("★ view lost repo B #2 after the update")
	}
}

func TestUpdateFailureFromStarRowShowsForgeReason(t *testing.T) {
	f := starUpdateFake(domain.AccessWrite)
	m := press(t, rvSized(t, f, core.Options{}, 120, 40), "l", "3")
	m = openUpdate(t, m)
	f.FailNext(fmt.Errorf("%w: merge failed because of conflict", forge.ErrRefused))
	m = press(t, m, "y")
	if !m.statusErr || !strings.Contains(m.status, "merge failed because of conflict") {
		t.Errorf("status = %q (err %v), want the forge's reason as an error", m.status, m.statusErr)
	}
	if strings.Contains(m.status, "Updated") {
		t.Errorf("failed update reported as updated: %q", m.status)
	}
}

func TestUpdateHelpOverlayListsUpdateOnlyWhereEnabled(t *testing.T) {
	helpHasUpdate := func(m Model) bool {
		for _, l := range lines(press(t, m, "?")) {
			if strings.Contains(l, "update") {
				return true
			}
		}
		return false
	}
	if !helpHasUpdate(wideAtCRs(t, updateFake(domain.AccessWrite))) {
		t.Error("help overlay on an open PR lacks u update")
	}
	if helpHasUpdate(wideAtCRs(t, updateFake(domain.AccessRead))) {
		t.Error("help overlay offers u update with read access")
	}
	m := press(t, wideAtCRs(t, updateFake(domain.AccessWrite)), "2")
	if helpHasUpdate(m) {
		t.Error("help overlay offers u update on an issue")
	}
}
