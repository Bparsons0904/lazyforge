package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// noLogs hides the Fake's log reading while keeping its runs and jobs.
type noLogs struct {
	forge.Forge
	rl forge.RunLister
}

func (n noLogs) ListRuns(ctx context.Context, r domain.RepoRef, f forge.RunFilter) ([]domain.Run, error) {
	return n.rl.ListRuns(ctx, r, f)
}

func (n noLogs) ListJobs(ctx context.Context, r domain.RepoRef, id int64) ([]domain.Job, error) {
	return n.rl.ListJobs(ctx, r, id)
}

func runsFake(status domain.CIState, jobs []domain.Job) *forgetest.Fake {
	f := homelabFake(nil)
	f.AddRun(homelab, domain.Run{ID: 7, Number: 3, Workflow: "ci", Status: status, StartedAt: time.Now(), Commit: "abcdef1234"}, jobs,
		map[int64]string{70: "\x1b[32mlint ok\x1b[0m\n", 71: "compiling\nerror: boom\n"})
	return f
}

var twoJobs = []domain.Job{
	{ID: 70, RunID: 7, Name: "lint", Status: domain.CIPass},
	{ID: 71, RunID: 7, Name: "build", Status: domain.CIFail},
}

// actionsDetails returns a model with the run open in the details pane.
func actionsDetails(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, newModel(t, f), "j", "3", "l")
}

func TestRunOverviewListsJobsAndPicksTheFailedOne(t *testing.T) {
	m := actionsDetails(t, runsFake(domain.CIFail, twoJobs))
	if l := lineContaining(m, "lint"); !strings.Contains(l, "✓") || strings.Contains(l, "›") {
		t.Errorf("lint row = %q, want a pass icon and no cursor", l)
	}
	if l := lineContaining(m, "build"); !strings.Contains(l, "✗") || !strings.Contains(l, "›") {
		t.Errorf("build row = %q, want a fail icon under the cursor", l)
	}
}

func TestRunLogsTabShowsThePickedJobsLog(t *testing.T) {
	m := actionsDetails(t, runsFake(domain.CIFail, twoJobs))
	m = press(t, m, "l")
	v := screen(m)
	if !strings.Contains(v, "error: boom") || strings.Contains(v, "lint ok") {
		t.Errorf("Logs tab should show the failed job's log only:\n%s", v)
	}
	m = press(t, m, "[", "k", "l")
	v = screen(m)
	if !strings.Contains(v, "lint ok") || strings.Contains(v, "\x1b[32m") || strings.Contains(v, "boom") {
		t.Errorf("after picking lint, Logs tab should show its cleaned log:\n%s", v)
	}
}

func TestRunWithoutLogReaderHasNoLogsTab(t *testing.T) {
	inner := runsFake(domain.CIFail, twoJobs)
	m := actionsDetails(t, noLogs{Forge: inner, rl: inner})
	v := screen(m)
	if strings.Contains(v, "Logs") || !strings.Contains(v, "build") {
		t.Errorf("a host without logs should list jobs but no Logs tab:\n%s", v)
	}
}

func TestRunningRunPollsUntilItFinishes(t *testing.T) {
	running := []domain.Job{{ID: 70, RunID: 7, Name: "lint", Status: domain.CIRunning}}
	m := press(t, newModel(t, runsFake(domain.CIRunning, running)), "j")
	m, msgs := step(t, m, "3")
	if !slices.ContainsFunc(msgs, func(x tea.Msg) bool { _, ok := x.(pollScheduled); return ok }) {
		t.Errorf("a running run should arm a poll, got %T", msgs)
	}
	jobs := 0
	for _, x := range exec(t, m.pollRun()) {
		if _, ok := x.(jobsLoadedMsg); ok {
			jobs++
		}
	}
	if jobs != 1 {
		t.Errorf("a poll of a running run should reload its jobs once, got %d", jobs)
	}
	done := press(t, newModel(t, runsFake(domain.CIPass, twoJobs[:1])), "j", "3")
	if cmd := done.pollRun(); cmd != nil {
		t.Errorf("a finished run should not be polled")
	}
}

func TestCleanLog(t *testing.T) {
	got := cleanLog("\x1b[31mred\x1b[0m\r\nprogress 10%\rprogress 100%\nplain\ttab")
	want := "red\nprogress 100%\nplain    tab"
	if got != want {
		t.Errorf("cleanLog = %q, want %q", got, want)
	}
}

func TestRunLogsTabShowsWhyALogFailedToLoad(t *testing.T) {
	m := actionsDetails(t, runsFake(domain.CIFail, twoJobs))
	m, _ = step(t, m, "l") // the log's own load result is not fed back, so the failure is its first answer
	m = run(t, m, jobLogLoadedMsg{repo: homelab, jobID: 71, err: errors.New("503 from the forge")})
	if v := screen(m); !strings.Contains(v, "Couldn't load the log: 503 from the forge") {
		t.Errorf("a failed log fetch should say why in the pane:\n%s", v)
	}
}

func TestOnlyOnePollChainRuns(t *testing.T) {
	running := []domain.Job{{ID: 70, RunID: 7, Name: "lint", Status: domain.CIRunning}}
	m := press(t, newModel(t, runsFake(domain.CIRunning, running)), "j", "3")
	// A tick lands, then the user moves on and back; the chain must not double.
	for _, k := range []string{"l", "h"} {
		var msgs []tea.Msg
		m, msgs = step(t, m, k)
		for _, x := range msgs {
			if _, ok := x.(pollScheduled); ok {
				t.Errorf("key %q armed a second poll while one was in flight", k)
			}
		}
	}
}
