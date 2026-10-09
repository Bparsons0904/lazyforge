package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const refreshEvery = 5 * time.Minute

type reposLoadedMsg struct {
	repos []domain.Repo
	err   error
}

type changeRequestsLoadedMsg struct {
	key   core.Key
	items []domain.ChangeRequest
	err   error
}

type issuesLoadedMsg struct {
	key   core.Key
	items []domain.Issue
	err   error
}

type runsLoadedMsg struct {
	key   core.Key
	items []domain.Run
	err   error
}

type readmeLoadedMsg struct {
	key    core.Key
	readme domain.Readme
	err    error
}

type branchesLoadedMsg struct {
	key      core.Key
	branches []domain.Branch
	err      error
}

type commitsLoadedMsg struct {
	key     core.Key
	commits []domain.Commit
	err     error
}

type refreshTickMsg struct{}

func tickEvery() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func loadRepos(ctx context.Context, svc *core.Service) tea.Cmd {
	return func() tea.Msg {
		repos, err := svc.Repos(ctx)
		return reposLoadedMsg{repos: repos, err: err}
	}
}

func loadChangeRequests(ctx context.Context, svc *core.Service, r domain.RepoRef) tea.Cmd {
	return func() tea.Msg {
		items, err := svc.ChangeRequests(ctx, r)
		return changeRequestsLoadedMsg{key: core.Key{Kind: core.KindChangeRequests, Repo: r}, items: items, err: err}
	}
}

func loadIssues(ctx context.Context, svc *core.Service, r domain.RepoRef) tea.Cmd {
	return func() tea.Msg {
		items, err := svc.Issues(ctx, r)
		return issuesLoadedMsg{key: core.Key{Kind: core.KindIssues, Repo: r}, items: items, err: err}
	}
}

func loadRuns(ctx context.Context, svc *core.Service, r domain.RepoRef) tea.Cmd {
	return func() tea.Msg {
		items, err := svc.Runs(ctx, r)
		return runsLoadedMsg{key: core.Key{Kind: core.KindRuns, Repo: r}, items: items, err: err}
	}
}

func loadReadme(ctx context.Context, svc *core.Service, r domain.RepoRef) tea.Cmd {
	return func() tea.Msg {
		rd, err := svc.Readme(ctx, r)
		return readmeLoadedMsg{key: core.Key{Kind: core.KindReadme, Repo: r}, readme: rd, err: err}
	}
}

func loadBranches(ctx context.Context, svc *core.Service, r domain.RepoRef) tea.Cmd {
	return func() tea.Msg {
		bs, err := svc.Branches(ctx, r)
		return branchesLoadedMsg{key: core.Key{Kind: core.KindBranches, Repo: r}, branches: bs, err: err}
	}
}

func loadCommits(ctx context.Context, svc *core.Service, r domain.RepoRef, branch string) tea.Cmd {
	return func() tea.Msg {
		cs, err := svc.Commits(ctx, r, branch)
		return commitsLoadedMsg{key: core.Key{Kind: core.KindCommits, Repo: r, Ref: branch}, commits: cs, err: err}
	}
}

// landOn shows the cursor branch's cached commits while the refetch runs; the cursor must be in range.
func (m *Model) landOn() tea.Cmd {
	r, name := m.boxes.repo, m.boxes.branches.list[m.details.branchCur].Name
	if cs, _, ok := m.svc.PeekCommits(r, name); ok {
		m.boxes.branches.setCommits(name, cs)
	}
	return loadCommits(m.selCtx, m.svc, r, name)
}

// selectRepo cancels the previous selection's fetches and loads the boxes of the repo under the cursor.
// With cached=true it seeds from the cache and fetches only what missed; otherwise it refetches everything.
func (m *Model) selectRepo(cached bool) tea.Cmd {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	r, ok := m.repos.selected()
	if !ok {
		m.boxes = boxes{}
		if m.repos.loaded {
			return m.startScan(cached)
		}
		return nil
	}
	m.stopScan()
	ctx, cancel := context.WithCancel(m.ctx)
	m.selCtx, m.cancel = ctx, cancel
	m.boxes = boxes{repo: r.RepoRef, repoRow: r, showRuns: m.svc.Can(forge.ActRuns, r).OK, showRepo: m.svc.Can(forge.ActReadme, r).OK, showBranches: m.svc.Can(forge.ActBranches, r).OK}
	m.boxes.loaded[boxRepo] = true
	// The cursor must be reset before loadBoxes, which reads it to pick the branch whose commits it fetches.
	m.details.branchCur = 0
	return m.loadBoxes(cached)
}

// loadBoxes fetches the selected repo's kinds, skipping cache hits when cached is true.
func (m *Model) loadBoxes(cached bool) tea.Cmd {
	b, ref, svc, ctx := &m.boxes, m.boxes.repo, m.svc, m.selCtx
	var cmds []tea.Cmd
	if items, _, ok := svc.PeekChangeRequests(ref); ok {
		b.crs, b.loaded[boxCRs] = items, true
	}
	if !cached || !b.loaded[boxCRs] {
		cmds = append(cmds, loadChangeRequests(ctx, svc, ref))
	}
	if items, _, ok := svc.PeekIssues(ref); ok {
		b.issues, b.loaded[boxIssues] = items, true
	}
	if !cached || !b.loaded[boxIssues] {
		cmds = append(cmds, loadIssues(ctx, svc, ref))
	}
	if b.showRuns {
		if items, _, ok := svc.PeekRuns(ref); ok {
			b.runs, b.loaded[boxRuns] = items, true
		}
		if !cached || !b.loaded[boxRuns] {
			cmds = append(cmds, loadRuns(ctx, svc, ref))
		}
	}
	if b.showRepo {
		if rd, _, ok := svc.PeekReadme(ref); ok {
			b.readme = readmeState{ok: true, r: rd}
		}
		if !cached || !b.readme.ok {
			cmds = append(cmds, loadReadme(ctx, svc, ref))
		}
	}
	if b.showBranches {
		if bs, _, ok := svc.PeekBranches(ref); ok {
			b.branches.ok, b.branches.list = true, bs
		}
		if !cached || !b.branches.ok {
			cmds = append(cmds, loadBranches(ctx, svc, ref))
		}
		if n := len(b.branches.list); n > 0 {
			m.details.branchCur = max(min(m.details.branchCur, n-1), 0)
			cmds = append(cmds, m.landOn())
		}
	}
	return tea.Batch(cmds...)
}

// refresh refetches the repo list and every visible kind of the selected repo, cached or not.
func (m *Model) refresh() tea.Cmd {
	cmds := []tea.Cmd{loadRepos(m.ctx, m.svc)}
	if m.onStar() {
		if m.star.cov != nil {
			cmds = append(cmds, m.startScan(false))
		}
	} else if m.selCtx != nil && m.boxes.repo != (domain.RepoRef{}) {
		cmds = append(cmds, m.loadBoxes(false))
	}
	return tea.Batch(cmds...)
}

// loadFailed reports whether a per-repo load result must be dropped, setting the status for real errors.
// Results for another repo are stale, and cancellations come from navigating away.
func (m *Model) loadFailed(k core.Key, err error) bool {
	if k.Repo != m.boxes.repo {
		return true
	}
	if err == nil {
		return false
	}
	if !errors.Is(err, context.Canceled) {
		m.setError(err)
	}
	return true
}
