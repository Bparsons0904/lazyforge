package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func scanMsgs(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(renovateScannedMsg); ok {
			n++
		}
	}
	return n
}

// Seam: scan sequencing and coverage. A result for an old scan changes nothing, and a failure shows in [1] only.
func TestStarScanDropsStaleAndReportsFailuresInCoverage(t *testing.T) {
	m := seeded(t)
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	repos, err := m.svc.Repos(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(reposLoadedMsg{repos: repos})
	m = next.(Model)
	seq := m.star.seq
	scan, err := m.svc.RenovateScan(m.ctx, repos[0])
	if err != nil {
		t.Fatal(err)
	}

	next, _ = m.Update(renovateScannedMsg{seq: seq - 1, repo: repos[0].RepoRef, scan: scan})
	m = next.(Model)
	if m.star.cov.Scanned() != 0 || len(m.star.view.ByRepo) != 0 {
		t.Fatalf("stale result applied: scanned %d", m.star.cov.Scanned())
	}

	next, _ = m.Update(renovateScannedMsg{seq: seq, repo: repos[0].RepoRef, scan: scan})
	m = next.(Model)
	next, _ = m.Update(renovateScannedMsg{seq: seq, repo: repos[1].RepoRef, err: errors.New("boom")})
	m = next.(Model)
	v := strings.Join(lines(m), "\n")
	for _, want := range []string{"scanned 2 of 4 repositories", "· 1 failed"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	if m.status != "" || m.statusErr {
		t.Errorf("scan failure leaked to the status bar: %q", m.status)
	}
}

// Seam: leaving ★ cancels the scan, coming back seeds from cache and fetches nothing; r refetches every repo.
func TestStarScanLifecycle(t *testing.T) {
	m := sized(t, 80, 24)
	if !m.star.cov.Complete() {
		t.Fatalf("demo scan incomplete: %d of %d", m.star.cov.Scanned(), m.star.cov.Total())
	}
	m = press(t, m, "j")
	if m.star.cov != nil {
		t.Fatal("moving off ★ left the scan running")
	}
	m, msgs := step(t, m, "k")
	if scanMsgs(msgs) != 0 || !m.star.cov.Complete() {
		t.Fatalf("returning to ★ fetched %d repos, want a cache seed", scanMsgs(msgs))
	}
	m, msgs = step(t, m, "r")
	if scanMsgs(msgs) != m.star.cov.Total() {
		t.Fatalf("r fetched %d repos, want all %d", scanMsgs(msgs), m.star.cov.Total())
	}
}

// Seam: box routing and the group merge. m on a [2] group rechecks all members and merges them across repos.
func TestStarGroupMergeAcrossRepos(t *testing.T) {
	m := sized(t, 80, 24)
	for _, l := range lines(m) {
		if lipgloss.Width(l) > 80 {
			t.Fatalf("line wider than 80: %q", l)
		}
	}
	if len(lines(m)) != 24 {
		t.Fatalf("frame is %d lines, want 24", len(lines(m)))
	}

	m = press(t, m, "1", "m")
	if m.dialog != nil {
		t.Fatal("m opened a dialog on [1]")
	}

	m = press(t, m, "2")
	m.star.cov.Done(homelab, errors.New("x")) // pretend one repo failed to scan
	m = press(t, m, "m")
	if m.dialog == nil || len(m.dialog.targets) != 2 || !m.dialog.star {
		t.Fatalf("dialog %+v", m.dialog)
	}
	v := strings.Join(lines(m), "\n")
	for _, want := range []string{"home/homelab #42", "home/infra #17", "postgres 16.4 → 17.0", "each repo's default", "1 repositories not scanned; this may not be every PR for this update"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	for _, l := range lines(m) {
		if lipgloss.Width(l) > 80 {
			t.Fatalf("dialog line wider than 80: %q", l)
		}
	}

	m = press(t, m, "esc")
	if m.dialog != nil {
		t.Fatal("esc left the dialog open")
	}
	m = press(t, m, "m", "y")
	if m.status != "Merged 2 of 2" || len(m.star.marked) != 0 {
		t.Fatalf("status %q marked %v", m.status, m.star.marked)
	}
	for _, grp := range m.star.view.Groups {
		if strings.HasPrefix(grp.Label, "postgres") {
			t.Fatalf("merged group still in [2]: %+v", grp)
		}
	}
}

// Seam: marks are shared by [3] and [5], and m merges the marked set, else the row under the cursor.
func TestStarMarksAcrossBoxes(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "3", "j", "space", "5")
	if len(m.star.marked) != 1 || !strings.Contains(strings.Join(lines(m), "\n"), "◆ home/homelab #41") {
		t.Fatalf("mark not shared with [5]: %v", m.star.marked)
	}
	m = press(t, m, "esc")
	if len(m.star.marked) != 0 {
		t.Fatalf("esc left marks %v", m.star.marked)
	}
	m = press(t, m, "3", "m")
	if m.dialog == nil || len(m.dialog.targets) != 1 {
		t.Fatalf("cursor merge dialog %+v", m.dialog)
	}
}

// Seam: dialog dedup is per (repo, number), skipped and coverage lines show, and an unreadable Renovate PR says so.
func TestMergeDialogMultiRepo(t *testing.T) {
	a, b := domain.RepoRef{Owner: "o", Name: "a"}, domain.RepoRef{Owner: "o", Name: "b"}
	cr := func(n int) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, Title: "t", SourceBranch: "renovate/x"}
	}
	ts := []core.Target{{Repo: a, CR: cr(1)}, {Repo: b, CR: cr(1)}, {Repo: a, CR: cr(1)}}
	d := mergeDialog(ts, mergeOpts{skipped: []string{"o/c #9: no write access"}, coverage: "2 repositories not scanned", star: true})
	if len(d.targets) != 2 {
		t.Fatalf("dedup by (repo, number) kept %d, want 2", len(d.targets))
	}
	v := strip(strings.Join(d.lines(), "\n"))
	for _, want := range []string{"each repo's default", "o/a #1 t", "o/b #1 t", "Skipped o/c #9: no write access", "2 repositories not scanned", "packages unknown"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	single := strip(strings.Join(mergeDialog(ts[:1], mergeOpts{}).lines(), "\n"))
	if strings.Contains(single, "o/a") || !strings.Contains(single, "#1 t") {
		t.Errorf("single-repo dialog changed:\n%s", single)
	}
}
