package ui

import (
	"fmt"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// noUpdater hides the Fake's update capability, so the forge looks like one that can't update branches.
type noUpdater struct{ forge.Forge }

// updateFake is listFake plus PR #1 targeting main, so the dialog has a real branch to name.
func updateFake(access domain.Access) *forgetest.Fake {
	f := listFake(access, 0)
	f.AddChangeRequest(repoOR, domain.ChangeRequest{
		Number: 1, Title: "pr1", HeadSHA: "sha1", CI: domain.CIPass, TargetBranch: "main",
		WebURL: "https://f.test/o/r/pulls/1",
	})
	return f
}

// openUpdate presses u on the PR under the cursor and checks that the dialog opened with no forge call.
func openUpdate(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := update(m, keyMsg("u"))
	if cmd != nil {
		t.Fatal("u produced a command; the dialog should open without a forge call")
	}
	if m.dialog == nil || !m.dialog.isUpdate {
		t.Fatal("u did not open the update dialog")
	}
	return m
}

func TestUpdateKeyOpensDialogOnOpenPR(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := atCRs(t, f, core.Options{})
	// u update is the last hint, so 120 columns clips it; the binding being enabled is what puts it in the bar.
	if !m.keys.UpdateBranch.Enabled() {
		t.Errorf("u update not enabled on an open PR: %s", strip(m.statusBar()))
	}
	m = openUpdate(t, m)
	scr := screen(m)
	for _, want := range []string{"Update #1", "#1 pr1", "Update with the latest main:", "› merge", "  rebase"} {
		if !strings.Contains(scr, want) {
			t.Errorf("dialog lacks %q", want)
		}
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations before confirming, want none", n)
	}
}

func TestUpdateDialogSingleStyle(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	f.SetUpdateStyles(forge.UpdateMerge)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	scr := screen(m)
	if strings.Contains(scr, "rebase") || !strings.Contains(scr, "› merge") {
		t.Errorf("GitHub-style dialog should list only merge:\n%s", scr)
	}
	if !strings.Contains(scr, "y/enter confirm") || strings.Contains(scr, "j/k choose") {
		t.Errorf("single-style hints wrong:\n%s", scr)
	}
}

func TestUpdateDialogBlankTargetFallsBack(t *testing.T) {
	m := openUpdate(t, atCRs(t, listFake(domain.AccessWrite, 1), core.Options{}))
	if !strings.Contains(screen(m), "Update with the latest target branch:") {
		t.Errorf("blank TargetBranch should fall back to target branch:\n%s", screen(m))
	}
}

func TestUpdateDialogMovesWithinBoundsAndEscCancels(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	m = press(t, m, "k")
	if !strings.Contains(screen(m), "› merge") {
		t.Error("k above the first row moved the cursor")
	}
	m = press(t, m, "j", "j", "j")
	if !strings.Contains(screen(m), "› rebase") {
		t.Error("j past the last row did not stop on rebase")
	}
	m = press(t, m, "esc")
	if m.dialog != nil {
		t.Error("esc left the update dialog open")
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("esc made %d mutations, want none", n)
	}
}

func TestUpdateConfirmRebasesAndRefreshesRow(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	m = press(t, m, "j", "y")
	if m.dialog != nil {
		t.Error("y left the update dialog open")
	}
	ms := f.Mutations()
	if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Style != forge.UpdateRebase {
		t.Fatalf("mutations = %+v, want one update-branch with rebase", ms)
	}
	if m.status != "Updated #1 (rebase)" || m.statusErr {
		t.Errorf("status = %q (err %v), want Updated #1 (rebase)", m.status, m.statusErr)
	}
	fresh, err := f.GetChangeRequest(t.Context(), repoOR, 1)
	if err != nil {
		t.Fatal(err)
	}
	crs, _, _ := m.svc.PeekChangeRequests(repoOR)
	if fresh.HeadSHA == "sha1" || len(crs) == 0 || crs[0].HeadSHA != fresh.HeadSHA {
		t.Errorf("PR box row head %v, want the forge's new head %q", crs, fresh.HeadSHA)
	}
}

func TestUpdateFailureShowsForgeReason(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	f.FailNext(fmt.Errorf("%w: merge failed because of conflict", forge.ErrRefused))
	m = press(t, m, "y")
	if !m.statusErr || !strings.Contains(m.status, "merge failed because of conflict") {
		t.Errorf("status = %q (err %v), want the forge's reason as an error", m.status, m.statusErr)
	}
	if strings.Contains(m.status, "Updated") {
		t.Errorf("failed update reported as updated: %q", m.status)
	}
}

func TestUpdateGatedOnReadAccessAndCapability(t *testing.T) {
	m := atCRs(t, updateFake(domain.AccessRead), core.Options{})
	if strings.Contains(strip(m.statusBar()), "u update") {
		t.Error("u update hinted on a repo with read access")
	}
	if m, _ = update(m, keyMsg("u")); m.dialog != nil {
		t.Error("u opened the dialog with read access")
	}

	m = atCRs(t, noUpdater{updateFake(domain.AccessWrite)}, core.Options{})
	if strings.Contains(strip(m.statusBar()), "u update") {
		t.Error("u update hinted on a forge without BranchUpdater")
	}
	if m, _ = update(m, keyMsg("u")); m.dialog != nil {
		t.Error("u opened the dialog on a forge without BranchUpdater")
	}
}

func TestUpdateDisabledWhenNoStylesAdvertised(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	f.SetUpdateStyles()
	m := atCRs(t, f, core.Options{})
	if m.keys.UpdateBranch.Enabled() {
		t.Error("u update enabled although the forge advertises no styles")
	}
	m, cmd := update(m, keyMsg("u"))
	if m.dialog != nil || cmd != nil {
		t.Errorf("u produced dialog %v / cmd %v with no styles", m.dialog, cmd)
	}
}

func TestUpdateDisabledOnIssues(t *testing.T) {
	m := press(t, atCRs(t, updateFake(domain.AccessWrite), core.Options{}), "h", "2")
	if strings.Contains(strip(m.statusBar()), "u update") {
		t.Errorf("u update hinted on an issue: %s", strip(m.statusBar()))
	}
	if m, _ = update(m, keyMsg("u")); m.dialog != nil {
		t.Error("u opened a dialog on an issue")
	}
}

func TestUpdateTwiceReopensDialogBeforeFirstResult(t *testing.T) {
	f := updateFake(domain.AccessWrite)
	m := openUpdate(t, atCRs(t, f, core.Options{}))
	m, first := update(m, keyMsg("y"))
	m = openUpdate(t, m)
	_, second := update(m, keyMsg("y"))
	if first == nil || second == nil {
		t.Fatal("confirming should return an update command each time")
	}
	exec(t, first)
	exec(t, second)
	if n := len(f.Mutations()); n != 2 {
		t.Errorf("%d update-branch mutations, want 2 with no in-flight block", n)
	}
}

// Seam: u on a ★ PR row updates it and reseeds the ★ view from the refreshed cache; read-only repos get no u.
func TestStarUpdateFromRenovateRow(t *testing.T) {
	f := rvFake("PR", rvRepo{ref: repoB, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}})
	m := press(t, rvSized(t, f, core.Options{}, 120, 40), "l", "3")
	if !m.keys.UpdateBranch.Enabled() {
		t.Fatal("u disabled on a ★ PR row with write access")
	}
	m = press(t, openUpdate(t, m), "y")
	ms := f.Mutations()
	if len(ms) != 1 || ms[0].Op != "update-branch" || ms[0].Item.Repo != repoB || ms[0].Item.Number != 2 || ms[0].Style != forge.UpdateMerge {
		t.Fatalf("mutations = %+v, want one merge update of repo B #2", ms)
	}
	if m.status != "Updated #2 (merge)" || m.statusErr {
		t.Errorf("status = %q (err %v), want Updated #2 (merge)", m.status, m.statusErr)
	}

	ro := rvFake("PR", rvRepo{ref: repoB, access: domain.AccessRead, prs: []domain.ChangeRequest{rvPostgres(2, domain.CIPass)}})
	m = press(t, rvSized(t, ro, core.Options{}, 120, 40), "l", "3")
	if m.keys.UpdateBranch.Enabled() {
		t.Error("u enabled on a ★ PR row in a read-only repo")
	}
}
