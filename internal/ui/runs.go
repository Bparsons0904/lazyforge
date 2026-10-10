package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const (
	pollEvery = 3 * time.Second
	// logsTab is right only because details.tabs appends Logs straight after Overview.
	logsTab = 1
)

type jobsLoadedMsg struct {
	repo  domain.RepoRef
	runID int64
	jobs  []domain.Job
	err   error
}

type jobLogLoadedMsg struct {
	repo  domain.RepoRef
	jobID int64
	text  string
	err   error
}

type runPollMsg struct{}

func (jobsLoadedMsg) fromSession()   {}
func (jobLogLoadedMsg) fromSession() {}
func (runPollMsg) fromSession()      {}

func pollAfter() tea.Cmd {
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return runPollMsg{} })
}

func loadJobs(ctx context.Context, svc *core.Service, r domain.RepoRef, runID int64) tea.Cmd {
	return func() tea.Msg {
		js, err := svc.Jobs(ctx, r, runID)
		return jobsLoadedMsg{repo: r, runID: runID, jobs: js, err: err}
	}
}

func loadJobLog(ctx context.Context, svc *core.Service, r domain.RepoRef, jobID int64) tea.Cmd {
	return func() tea.Msg {
		text, err := svc.JobLog(ctx, r, jobID)
		return jobLogLoadedMsg{repo: r, jobID: jobID, text: cleanLog(text), err: err}
	}
}

type runState struct {
	repo    domain.RepoRef
	run     int64 // 0 while no run is on screen
	jobs    []domain.Job
	jobsOK  bool
	jobsErr error
	cur     int
	// logJob is set before the load starts, so a result for any other job is dropped.
	logJob int64
	log    string
	logOK  bool
	logErr error
}

func (r runState) curJob() (domain.Job, bool) {
	if r.cur < 0 || r.cur >= len(r.jobs) {
		return domain.Job{}, false
	}
	return r.jobs[r.cur], true
}

func (r runState) moving(run domain.Run) bool {
	return inProgress(run.Status) || slices.ContainsFunc(r.jobs, func(j domain.Job) bool { return inProgress(j.Status) })
}

func inProgress(s domain.CIState) bool { return s == domain.CIRunning || s == domain.CIPending }

// selectedRun is false at the repo list and on ★ Renovate, where no run is on screen.
func (m Model) selectedRun() (domain.Run, bool) {
	if m.onStar() || m.level == levelRepos {
		return domain.Run{}, false
	}
	run, ok := m.boxes.selected().(domain.Run)
	return run, ok
}

// followRun loads what the details pane needs from the run on screen:
// its jobs, the log of the job under the cursor while the Logs tab is open, and a poll while anything is moving.
func (m *Model) followRun() tea.Cmd {
	d := &m.details.run
	run, ok := m.selectedRun()
	if !ok {
		*d = runState{}
		return nil
	}
	var cmds []tea.Cmd
	if d.run != run.ID || d.repo != m.boxes.repo {
		*d = runState{repo: m.boxes.repo, run: run.ID}
		cmds = append(cmds, loadJobs(m.selCtx, m.svc, d.repo, run.ID))
	}
	if j, ok := d.curJob(); ok && m.logsOpen() && d.logJob != j.ID {
		d.logJob, d.log, d.logOK, d.logErr = j.ID, "", false, nil
		cmds = append(cmds, loadJobLog(m.selCtx, m.svc, d.repo, j.ID))
	}
	if !m.details.polling && d.moving(run) {
		m.details.polling = true
		cmds = append(cmds, m.poll())
	}
	return tea.Batch(cmds...)
}

func (m Model) logsOpen() bool {
	return m.details.logs && m.details.tab == logsTab
}

// pollRun refreshes the run list, the jobs and the open log; followRun re-arms the poll while the run is still going.
func (m *Model) pollRun() tea.Cmd {
	d := &m.details.run
	m.details.polling = false
	run, ok := m.selectedRun()
	if !ok || !d.moving(run) {
		return nil
	}
	cmds := []tea.Cmd{loadRuns(m.selCtx, m.svc, d.repo), loadJobs(m.selCtx, m.svc, d.repo, d.run)}
	if m.logsOpen() && d.logJob != 0 {
		cmds = append(cmds, loadJobLog(m.selCtx, m.svc, d.repo, d.logJob))
	}
	return tea.Batch(cmds...)
}

// The first load puts the cursor on the job most worth seeing; later loads keep it on the same job.
func (m *Model) jobsLoaded(msg jobsLoadedMsg) {
	d := &m.details.run
	if msg.repo != d.repo || msg.runID != d.run {
		return
	}
	if msg.err != nil {
		if len(d.jobs) == 0 {
			d.jobsErr, d.jobsOK = msg.err, true
		}
		return
	}
	prev, had := d.curJob()
	d.jobsErr, d.jobsOK, d.jobs = nil, true, msg.jobs
	d.cur = core.FirstProblemJob(msg.jobs)
	if had {
		if i := slices.IndexFunc(msg.jobs, func(j domain.Job) bool { return j.ID == prev.ID }); i >= 0 {
			d.cur = i
		}
	}
}

// jobLogLoaded drops a log for a job that is no longer open.
func (m *Model) jobLogLoaded(msg jobLogLoadedMsg) {
	d := &m.details.run
	if msg.repo != d.repo || msg.jobID != d.logJob {
		return
	}
	if msg.err != nil {
		if !d.logOK {
			d.logErr, d.logOK = msg.err, true
		}
		return
	}
	d.logErr, d.logOK, d.log = nil, true, msg.text
}

// cleanLog drops colour codes and rewrites carriage-return progress lines to their last state, so the text is safe to draw.
// It runs in the load command, not in Update, because a log is large.
func cleanLog(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = sanitizeLine(l)
	}
	return strings.Join(lines, "\n")
}

func (m Model) jobsActive() bool {
	_, isRun := m.boxes.selected().(domain.Run)
	return m.level == levelDetails && isRun && m.details.tab == 0 && len(m.details.run.jobs) > 0
}

func (m *Model) jobsKey(msg tea.KeyPressMsg) bool {
	k, d := m.keys, &m.details
	switch {
	case key.Matches(msg, k.Down):
		d.run.cur = min(d.run.cur+1, len(d.run.jobs)-1)
	case key.Matches(msg, k.Up):
		d.run.cur = max(d.run.cur-1, 0)
	case d.logs && key.Matches(msg, k.Right):
		d.tab = logsTab
	default:
		return false
	}
	return true
}

func (d *details) runText(run domain.Run, b boxes, cw int, now time.Time, highlight bool) string {
	if d.tab == logsTab {
		return d.logText()
	}
	s := overview(run, b.repo, now, nil) + "\n\n" + style.Heading.Render("Jobs") + "\n"
	r := d.run
	switch {
	case !r.jobsOK:
		return s + style.Faint.Render("Loading…")
	case r.jobsErr != nil:
		return s + style.Faint.Render("Couldn't load the jobs: "+sanitizeLine(r.jobsErr.Error()))
	case len(r.jobs) == 0:
		return s + style.Faint.Render("— none —")
	}
	rows := make([]string, len(r.jobs))
	for i, j := range r.jobs {
		mark, base := "  ", lipgloss.NewStyle()
		if highlight && i == r.cur {
			mark, base = "› ", lipgloss.NewStyle().Bold(true)
		}
		name := truncate(sanitizeLine(j.Name), max(cw-6, 1))
		row := mark + ciIcon(j.Status, base) + " " + base.Render(name)
		if j.Attempt > 1 {
			row += style.Faint.Render(fmt.Sprintf(" · attempt %d", j.Attempt))
		}
		rows[i] = row
	}
	s += strings.Join(rows, "\n")
	if highlight && d.logs {
		s += "\n\n" + style.Faint.Render("j/k pick a job · l open its log")
	}
	return s
}

func (d *details) logText() string {
	r := d.run
	j, ok := r.curJob()
	switch {
	case !r.jobsOK:
		return style.Faint.Render("Loading…")
	case !ok:
		return style.Faint.Render("This run has no jobs")
	}
	head := ciIcon(j.Status, lipgloss.NewStyle()) + " " + style.Heading.Render(sanitizeLine(j.Name))
	switch {
	case r.logErr != nil:
		return head + "\n\n" + style.Faint.Render("Couldn't load the log: "+sanitizeLine(r.logErr.Error()))
	case !r.logOK:
		return head + "\n\n" + style.Faint.Render("Loading…")
	case strings.TrimSpace(r.log) == "":
		return head + "\n\n" + style.Faint.Render("No output yet")
	}
	return head + "\n\n" + r.log
}
