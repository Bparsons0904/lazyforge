package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// gatedFake is one repo with one PR and one issue at the given access.
func gatedFake(access domain.Access) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR"})
	r := domain.RepoRef{Owner: "o", Name: "r"}
	f.AddRepo(domain.Repo{RepoRef: r, Access: access})
	f.AddChangeRequest(r, domain.ChangeRequest{Number: 1, Title: "pr", HeadSHA: "abc", WebURL: "https://f.test/o/r/pulls/1"})
	f.AddIssue(r, domain.Issue{Number: 2, Title: "issue"})
	return f
}

func TestActionKeysGated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		access domain.Access
		gate   bool
		want   map[string]bool
	}{
		{"read", domain.AccessRead, false, map[string]bool{"merge": false, "approve": false, "close": false, "comment": true}},
		{"write", domain.AccessWrite, false, map[string]bool{"merge": true, "approve": true, "close": true, "comment": true}},
		{"approve gated", domain.AccessWrite, true, map[string]bool{"merge": true, "approve": false, "close": true, "comment": true}},
	} {
		f := gatedFake(tc.access)
		if tc.gate {
			f.SetGate(forge.ActApprove, errors.New("off"))
		}
		m := press(t, sizedWith(t, seededWith(t, f), 200, 30), "j", "l")
		hints := strip(m.statusBar())
		for desc, want := range tc.want {
			if got := strings.Contains(hints, desc); got != want {
				t.Errorf("%s: hint %q shown=%v, want %v (%s)", tc.name, desc, got, want, hints)
			}
		}
		if !strings.Contains(hints, "o open") || (tc.want["close"] && !strings.Contains(hints, "x close")) || strings.Contains(hints, "run page") {
			t.Errorf("%s: o/R hints wrong: %s", tc.name, hints)
		}
		if tc.access == domain.AccessRead {
			if _, cmd := update(m, keyMsg("c")); cmd == nil {
				t.Error("read: c (comment) produced no command")
			}
			for _, k := range []string{"m", "a", "x"} {
				if next, msgs := step(t, m, k); len(msgs) != 0 || next.dialog != nil {
					t.Errorf("read: %s produced %v / dialog %v", k, msgs, next.dialog)
				}
			}
		}
	}
}

func TestMarks(t *testing.T) {
	m := press(t, sized(t, 120, 40), "j", "l", "space", "j", "space")
	if len(m.boxes.marked) != 2 || strings.Count(strings.Join(lines(m), "\n"), "◆") != 2 {
		t.Fatalf("marked %v", m.boxes.marked)
	}
	m = press(t, m, "space")
	if len(m.boxes.marked) != 1 {
		t.Fatalf("space again: %v", m.boxes.marked)
	}
	if m = press(t, m, "esc"); len(m.boxes.marked) != 0 {
		t.Fatalf("esc: %v", m.boxes.marked)
	}
}

func TestMergeDialogDedups(t *testing.T) {
	cr := domain.ChangeRequest{Number: 41, Title: "x", CI: domain.CIFail, Renovate: []domain.RenovateUpdate{{Package: "a", From: "1", To: "2"}, {Package: "b", From: "3", To: "4"}}}
	other := domain.ChangeRequest{Number: 42, Title: "y"}
	for _, tc := range []struct {
		name       string
		mergeStyle string
		greenOnly  bool
		want       []string
	}{
		{"repo default, CI not required", "", false, []string{"Strategy: repo default", "a 1 → 2", "b 3 → 4", "CI failing, will merge anyway"}},
		{"repo merge style, CI required", "squash", true, []string{"Strategy: squash", "will be refused"}},
	} {
		f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR"})
		f.AddRepo(domain.Repo{RepoRef: repoOR, Access: domain.AccessWrite, MergeStyle: tc.mergeStyle})
		f.AddChangeRequest(repoOR, domain.ChangeRequest{Number: 1, Title: "pr1", HeadSHA: "sha1"})
		m := atCRs(t, f, core.Options{RequireGreenCI: func(domain.RepoRef) bool { return tc.greenOnly }})
		var results []core.Checked
		for _, c := range []domain.ChangeRequest{cr, other, cr} {
			results = append(results, core.Checked{Target: core.Target{Repo: repoOR, CR: c}})
		}
		m, _ = update(m, recheckedMsg{repo: repoOR, results: results})
		got := screen(m)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in\n%s", tc.name, want, got)
			}
		}
		if n := strings.Count(got, "#41"); n != 1 {
			t.Errorf("%s: #41 listed %d times, want once\n%s", tc.name, n, got)
		}
	}
}

func TestSingleMergeEndToEnd(t *testing.T) {
	f := forgetest.NewDemo(time.Now())
	m := press(t, sizedWith(t, seededWith(t, f), 120, 40), "j", "l")
	cr := m.boxes.selected().(domain.ChangeRequest)
	m = press(t, m, "m")
	if m.dialog == nil || !strings.Contains(strings.Join(lines(m), "\n"), "Merge #") {
		t.Fatalf("no dialog:\n%s", strings.Join(lines(m), "\n"))
	}
	m = press(t, m, "y")
	if m.dialog == nil || m.dialog.phase != phaseDone || !strings.Contains(strings.Join(lines(m), "\n"), "merged") {
		t.Fatalf("not done:\n%s", strings.Join(lines(m), "\n"))
	}
	muts := f.Mutations()
	if len(muts) != 1 || muts[0].Op != "merge" || muts[0].Opts.HeadSHA != cr.HeadSHA {
		t.Fatalf("mutations %+v", muts)
	}
	for _, c := range m.boxes.crs {
		if c.Number == cr.Number {
			t.Fatal("merged CR still in box [1]")
		}
	}
	if m = press(t, m, "esc"); m.dialog != nil {
		t.Fatal("esc did not close the done dialog")
	}
}

func TestStatusSurvivesLongHints(t *testing.T) {
	m := press(t, sized(t, 80, 24), "j", "l", "2")
	m.setInfo("Comment cancelled")
	if got := strip(m.statusBar()); !strings.HasSuffix(got, "Comment cancelled") {
		t.Fatalf("status pushed off: %q", got)
	}
}
