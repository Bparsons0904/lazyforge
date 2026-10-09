package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	repoOR = domain.RepoRef{Owner: "o", Name: "r"}
	repoPS = domain.RepoRef{Owner: "p", Name: "s"}
)

// spyForge reports each Merge's options before delegating to the Fake; onMerge may block to hold a merge in flight.
type spyForge struct {
	*forgetest.Fake
	onMerge func(n int, o forge.MergeOpts)
}

func (s *spyForge) Merge(ctx context.Context, r domain.RepoRef, n int, opts forge.MergeOpts) error {
	if s.onMerge != nil {
		s.onMerge(n, opts)
	}
	return s.Fake.Merge(ctx, r, n, opts)
}

// listFake is repo o/r with n open PRs (#1..n, passing CI), issue #50 and run 9, plus an empty repo p/s.
func listFake(access domain.Access, n int) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoOR, Access: access})
	f.AddRepo(domain.Repo{RepoRef: repoPS, Access: access})
	for i := 1; i <= n; i++ {
		f.AddChangeRequest(repoOR, domain.ChangeRequest{
			Number: i, Title: fmt.Sprintf("pr%d", i), HeadSHA: fmt.Sprintf("sha%d", i), CI: domain.CIPass,
			WebURL: fmt.Sprintf("https://f.test/o/r/pulls/%d", i),
		})
	}
	f.AddIssue(repoOR, domain.Issue{Number: 50, Title: "bug", WebURL: "https://f.test/o/r/issues/50"})
	f.AddRun(repoOR, domain.Run{ID: 9, Workflow: "ci", Branch: "main", WebURL: "https://f.test/o/r/actions/runs/9"}, nil, nil)
	return f
}

// atCRs returns a 120x40 model over f with the cursor on the first PR of o/r.
func atCRs(t *testing.T, f forge.Forge, opts core.Options) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, opts))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	return press(t, sizedWith(t, m, 120, 40), "j", "l")
}

func screen(m Model) string { return strings.Join(lines(m), "\n") }

// update sends msg without running the command it returns.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestHelpOverlayFollowsAccess(t *testing.T) {
	for _, tc := range []struct {
		access domain.Access
		want   map[string]bool
	}{
		{domain.AccessRead, map[string]bool{"merge": false, "approve": false, "close": false, "comment": true}},
		{domain.AccessWrite, map[string]bool{"merge": true, "approve": true, "close": true, "comment": true}},
	} {
		m := press(t, atCRs(t, listFake(tc.access, 1), core.Options{}), "?")
		got := screen(m)
		for word, want := range tc.want {
			if has := strings.Contains(got, word); has != want {
				t.Errorf("access %v: help shows %q = %v, want %v\n%s", tc.access, word, has, want, got)
			}
		}
	}
}

func TestReadAccessKeys(t *testing.T) {
	m := atCRs(t, listFake(domain.AccessRead, 1), core.Options{})
	for _, k := range []string{"m", "a", "x", "space"} {
		if next, cmd := update(m, keyMsg(k)); cmd != nil || next.dialog != nil || len(next.boxes.marked) != 0 {
			t.Errorf("%s did something at read access", k)
		}
	}
	// The editor command is built but never run here.
	if _, cmd := update(m, keyMsg("c")); cmd == nil {
		t.Error("c should be available at read access")
	}
}

func TestApproveGatedKeyDoesNothing(t *testing.T) {
	f := listFake(domain.AccessWrite, 1)
	f.SetGate(forge.ActApprove, errors.New("off"))
	m := atCRs(t, f, core.Options{})
	if _, cmd := update(m, keyMsg("a")); cmd != nil {
		t.Error("a produced a command while approve is gated")
	}
	if _, cmd := update(m, keyMsg("m")); cmd == nil {
		t.Error("m should still work")
	}
}

func TestRunAndOpenHintsFollowSelection(t *testing.T) {
	f := listFake(domain.AccessWrite, 1)
	f.AddChangeRequest(repoOR, domain.ChangeRequest{Number: 2, Title: "no url", HeadSHA: "x"})
	m := atCRs(t, f, core.Options{})
	hints := func() string { return strip(m.statusBar()) }
	if h := hints(); !strings.Contains(h, "open") || strings.Contains(h, "run page") {
		t.Errorf("PR with url: %s", h)
	}
	m = press(t, m, "j")
	if h := hints(); strings.Contains(h, "open") {
		t.Errorf("PR without url still hints open: %s", h)
	}
	if _, cmd := update(m, keyMsg("o")); cmd != nil {
		t.Error("o produced a command for an item with no URL")
	}
	m = press(t, m, "3")
	if h := hints(); !strings.Contains(h, "run page") {
		t.Errorf("run selected: %s", h)
	}
}

func TestMarksClearedWhenRepoChanges(t *testing.T) {
	m := press(t, atCRs(t, listFake(domain.AccessWrite, 3), core.Options{}), "space", "h")
	if len(m.boxes.marked) != 1 {
		t.Fatalf("mark lost on leaving the boxes: %v", m.boxes.marked)
	}
	m = press(t, m, "j")
	if len(m.boxes.marked) != 0 {
		t.Fatalf("marks survived selecting another repo: %v", m.boxes.marked)
	}
	if m = press(t, m, "k", "l"); len(m.boxes.marked) != 0 || strings.Contains(screen(m), "◆") {
		t.Fatalf("marks came back: %v", m.boxes.marked)
	}
}

func TestMergeDialogFromMarks(t *testing.T) {
	f := listFake(domain.AccessWrite, 3)
	f.AddChangeRequest(repoOR, domain.ChangeRequest{
		Number: 4, Title: "bump", HeadSHA: "sha4",
		Renovate: []domain.RenovateUpdate{{Package: "alpha", From: "1.0", To: "2.0"}, {Package: "beta", From: "3.0", To: "4.0"}},
	})
	m := press(t, atCRs(t, f, core.Options{}), "space", "j", "j", "j", "space", "m")
	got := screen(m)
	for _, want := range []string{"Merge 2 PRs", "Strategy: repo default", "#1 pr1", "#4 bump", "alpha 1.0 → 2.0", "beta 3.0 → 4.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "#2 pr2") || strings.Contains(got, "#3 pr3") {
		t.Errorf("unmarked PRs listed:\n%s", got)
	}
}

func TestCIGateLabelsAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		require   func(domain.RepoRef) bool
		want      string
		wantMerge bool
	}{
		{"off", nil, "will merge anyway", true},
		{"on", func(domain.RepoRef) bool { return true }, "will be refused", false},
	} {
		f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR"})
		f.AddRepo(domain.Repo{RepoRef: repoOR, Access: domain.AccessWrite})
		f.AddChangeRequest(repoOR, domain.ChangeRequest{Number: 1, Title: "red", HeadSHA: "sha1", CI: domain.CIFail})
		m := press(t, atCRs(t, f, core.Options{RequireGreenCI: tc.require}), "m")
		if got := screen(m); !strings.Contains(got, "CI failing") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: dialog\n%s", tc.name, got)
		}
		m = press(t, m, "y")
		if merged := len(f.Mutations()) == 1; merged != tc.wantMerge {
			t.Errorf("%s: merged = %v, want %v", tc.name, merged, tc.wantMerge)
		}
		if !tc.wantMerge && !strings.Contains(screen(m), "refused") {
			t.Errorf("%s: result doesn't say refused\n%s", tc.name, screen(m))
		}
	}
}

func TestConfirmSendsRecheckedHeadSHA(t *testing.T) {
	var got []string
	spy := &spyForge{Fake: listFake(domain.AccessWrite, 1), onMerge: func(_ int, o forge.MergeOpts) { got = append(got, o.HeadSHA) }}
	m := atCRs(t, spy, core.Options{})
	fresh := domain.ChangeRequest{Number: 1, Title: "pr1", HeadSHA: "fresh-sha"}
	m, _ = update(m, recheckedMsg{repo: repoOR, results: []core.Checked{{Target: core.Target{Repo: repoOR, CR: fresh}}}})
	_, cmd := update(m, keyMsg("y"))
	exec(t, cmd)
	if len(got) != 1 || got[0] != "fresh-sha" {
		t.Fatalf("merge sent head SHAs %v, want [fresh-sha]", got)
	}
}

func targetsFor(nums ...int) []core.MergeResult {
	var out []core.MergeResult
	for _, n := range nums {
		out = append(out, core.MergeResult{Target: core.Target{Repo: repoOR, CR: domain.ChangeRequest{Number: n, Title: fmt.Sprintf("pr%d", n)}}})
	}
	return out
}

func TestMergeDoneShowsEachOutcome(t *testing.T) {
	m := press(t, atCRs(t, listFake(domain.AccessWrite, 5), core.Options{}), "space", "j", "space", "j", "space", "j", "space", "j", "space", "m")
	m, _ = update(m, keyMsg("y"))
	rs := targetsFor(1, 2, 3, 4, 5)
	rs[0].Outcome = core.OutcomeMerged
	rs[1].Outcome, rs[1].Err = core.OutcomeRefused, errors.New("2 approvals required")
	rs[2].Outcome, rs[2].Err = core.OutcomeFailed, errors.New("500 from server")
	rs[3].Outcome, rs[3].Err = core.OutcomeUnknown, context.DeadlineExceeded
	rs[4].Outcome = core.OutcomeNotStarted
	m, _ = update(m, mergeDoneMsg{repo: repoOR, results: rs})
	got := screen(m)
	for _, want := range []string{"merged", "refused: 2 approvals required", "failed: 500 from server", "unknown", "not started"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if len(m.boxes.marked) != 4 || m.boxes.marked[1] {
		t.Errorf("marks after partial merge: %v", m.boxes.marked)
	}
	if !strings.Contains(strip(m.statusBar()), "Merged 1 of 5") || !m.statusErr {
		t.Errorf("status %q err=%v", m.status, m.statusErr)
	}
}

func TestMergeDoneAllMergedIsNotAnError(t *testing.T) {
	m := press(t, atCRs(t, listFake(domain.AccessWrite, 2), core.Options{}), "space", "m")
	m, _ = update(m, keyMsg("y"))
	m, _ = update(m, mergeDoneMsg{repo: repoOR, results: targetsFor(1)})
	if m.status != "Merged 1 of 1" || m.statusErr || len(m.boxes.marked) != 0 {
		t.Errorf("status %q err=%v marks=%v", m.status, m.statusErr, m.boxes.marked)
	}
}

func TestRetryReconcilesAlreadyMerged(t *testing.T) {
	f := listFake(domain.AccessWrite, 2)
	m := press(t, atCRs(t, f, core.Options{}), "space", "j", "space")
	if err := f.Merge(context.Background(), repoOR, 1, forge.MergeOpts{HeadSHA: "sha1"}); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, "m")
	got := screen(m)
	if m.boxes.marked[1] || !m.boxes.marked[2] {
		t.Errorf("marks %v, want only #2", m.boxes.marked)
	}
	if strings.Contains(got, "#1 pr1") || !strings.Contains(got, "#2 pr2") {
		t.Errorf("dialog should list only #2:\n%s", got)
	}
	if s := strings.ToLower(m.status); !strings.Contains(s, "#1") || !strings.Contains(s, "merged") {
		t.Errorf("status %q doesn't report #1 as merged", m.status)
	}
	if n := len(f.Mutations()); n != 1 {
		t.Errorf("%d mutations, want only the external merge", n)
	}
}

func TestEscWhileRunningStopsNewMerges(t *testing.T) {
	started, release := make(chan int, 2), make(chan struct{})
	spy := &spyForge{Fake: listFake(domain.AccessWrite, 2), onMerge: func(n int, _ forge.MergeOpts) {
		started <- n
		<-release
	}}
	m := press(t, atCRs(t, spy, core.Options{MaxConcurrent: 1}), "space", "j", "space", "m")
	m, cmd := update(m, keyMsg("y"))
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-started:
	case <-time.After(execTimeout):
		t.Fatal("first merge never started")
	}
	m, _ = update(m, keyMsg("esc"))
	if m.dialog == nil || m.dialog.phase != phaseRunning {
		t.Fatal("dialog must stay open, running, until the results arrive")
	}
	close(release)
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(execTimeout):
		t.Fatal("merge never finished")
	}
	m, _ = update(m, msg)
	if m.dialog == nil || m.dialog.phase != phaseDone || !strings.Contains(screen(m), "not started") {
		t.Fatalf("done dialog:\n%s", screen(m))
	}
	if m.boxes.marked[1] || !m.boxes.marked[2] {
		t.Errorf("marks %v, want only #2 left", m.boxes.marked)
	}
	if n := len(spy.Mutations()); n != 1 {
		t.Errorf("%d merges reached the forge, want 1", n)
	}
}

func TestEscInConfirmCancelsWithoutMutation(t *testing.T) {
	f := listFake(domain.AccessWrite, 1)
	m := press(t, atCRs(t, f, core.Options{}), "m")
	if m.dialog == nil {
		t.Fatal("no dialog")
	}
	if m = press(t, m, "esc"); m.dialog != nil || len(f.Mutations()) != 0 {
		t.Errorf("dialog %v, mutations %v", m.dialog, f.Mutations())
	}
}

func TestCloseIssueAndChangeRequest(t *testing.T) {
	f := listFake(domain.AccessWrite, 2)
	m := press(t, atCRs(t, f, core.Options{}), "2", "x")
	if got := screen(m); !strings.Contains(got, "Close #50") {
		t.Fatalf("no close dialog:\n%s", got)
	}
	m = press(t, m, "y")
	if len(m.boxes.issues) != 0 {
		t.Errorf("issue still in box [2]: %v", m.boxes.issues)
	}
	m = press(t, m, "1", "x", "y")
	for _, c := range m.boxes.crs {
		if c.Number == 1 {
			t.Error("closed PR still in box [1]")
		}
	}
	muts := f.Mutations()
	if len(muts) != 2 || muts[0].Op != "close" || muts[0].Item.Kind != forge.ItemIssue || muts[1].Op != "close" || muts[1].Item.Kind != forge.ItemChangeRequest {
		t.Fatalf("mutations %+v", muts)
	}
}

func TestApprove(t *testing.T) {
	f := listFake(domain.AccessWrite, 1)
	m := press(t, atCRs(t, f, core.Options{}), "a")
	muts := f.Mutations()
	if len(muts) != 1 || muts[0].Op != "approve" || muts[0].Item.Number != 1 {
		t.Fatalf("mutations %+v", muts)
	}
	if m.status != "Approved #1" {
		t.Errorf("status %q", m.status)
	}
}

func TestEditorDone(t *testing.T) {
	item := forge.ItemRef{Repo: repoOR, Kind: forge.ItemChangeRequest, Number: 1}
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "c.md")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("posts the trimmed body and removes the file", func(t *testing.T) {
		f := listFake(domain.AccessWrite, 1)
		p := write("hello\n")
		run(t, atCRs(t, f, core.Options{}), editorDoneMsg{item: item, path: p})
		muts := f.Mutations()
		if len(muts) != 1 || muts[0].Op != "comment" || muts[0].Body != "hello" || muts[0].Item != item {
			t.Fatalf("mutations %+v", muts)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("temp file left behind: %v", err)
		}
	})
	t.Run("blank content is cancelled", func(t *testing.T) {
		f := listFake(domain.AccessWrite, 1)
		p := write(" \n\t\n")
		m := run(t, atCRs(t, f, core.Options{}), editorDoneMsg{item: item, path: p})
		if len(f.Mutations()) != 0 || m.status != "Comment cancelled" {
			t.Errorf("mutations %v, status %q", f.Mutations(), m.status)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("temp file left behind: %v", err)
		}
	})
	t.Run("editor error reaches the status bar", func(t *testing.T) {
		f := listFake(domain.AccessWrite, 1)
		m := run(t, atCRs(t, f, core.Options{}), editorDoneMsg{item: item, path: write("hi"), err: errors.New("editor exploded")})
		if len(f.Mutations()) != 0 || !m.statusErr || !strings.Contains(m.status, "editor exploded") {
			t.Errorf("mutations %v, status %q err=%v", f.Mutations(), m.status, m.statusErr)
		}
	})
}

func TestOpener(t *testing.T) {
	var opened []string
	var failWith error
	m := atCRs(t, listFake(domain.AccessWrite, 1), core.Options{})
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return failWith
	}
	m = press(t, m, "o", "2", "o", "3", "o", "R")
	want := []string{"https://f.test/o/r/pulls/1", "https://f.test/o/r/issues/50", "https://f.test/o/r/actions/runs/9", "https://f.test/o/r/actions/runs/9"}
	if strings.Join(opened, " ") != strings.Join(want, " ") {
		t.Fatalf("opened %v, want %v", opened, want)
	}
	failWith = errors.New("no browser")
	if m = press(t, m, "o"); !m.statusErr || !strings.Contains(m.status, "no browser") {
		t.Errorf("status %q err=%v", m.status, m.statusErr)
	}
	// R is for runs only.
	opened = nil
	press(t, m, "1", "R")
	if len(opened) != 0 {
		t.Errorf("R opened %v on a PR", opened)
	}
}

func TestStaleResultsOnlySetStatus(t *testing.T) {
	m := press(t, atCRs(t, listFake(domain.AccessWrite, 2), core.Options{}), "space")
	before := len(m.boxes.crs)
	m, _ = update(m, mergeDoneMsg{repo: repoPS, results: targetsFor(1)})
	if !strings.Contains(m.status, "Merged") {
		t.Errorf("merge status %q", m.status)
	}
	m, _ = update(m, actionDoneMsg{repo: repoPS, item: forge.ItemRef{Repo: repoPS, Kind: forge.ItemChangeRequest, Number: 1}, verb: "Closed", closed: true})
	if m.status != "Closed #1" {
		t.Errorf("close status %q", m.status)
	}
	if len(m.boxes.crs) != before || !m.boxes.marked[1] {
		t.Errorf("current boxes touched: %d PRs, marks %v", len(m.boxes.crs), m.boxes.marked)
	}
}

func TestDialogFitsWindow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {20, 5}} {
		check := func(m Model, name string) {
			t.Helper()
			ls := lines(m)
			if len(ls) != size[1] {
				t.Errorf("%v %s: %d lines", size, name, len(ls))
			}
			for _, l := range ls {
				if lipgloss.Width(l) > size[0] {
					t.Errorf("%v %s: line too wide: %q", size, name, l)
				}
			}
		}
		base := New(context.Background(), core.New(listFake(domain.AccessWrite, 3), core.Options{}))
		base.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
		m := press(t, sizedWith(t, base, size[0], size[1]), "j", "l", "space", "j", "space", "m")
		check(m, "merge confirm")
		m, _ = update(m, keyMsg("y"))
		check(m, "merge running")
		m = run(t, m, mergeDoneMsg{repo: repoOR, results: targetsFor(1, 2)})
		check(m, "merge done")
		m = press(t, m, "esc", "x")
		check(m, "close confirm")
	}
}
