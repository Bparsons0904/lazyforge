package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// renovateRunMsg.only is the repo it ran for, zero when it ran for all repos.
type renovateRunMsg struct {
	only domain.RepoRef
	err  error
}

// renovateScope is the repo N runs Renovate for; zero when the cursor has no repo in context, which makes the run host-wide.
func (m Model) renovateScope() domain.RepoRef {
	var r domain.RepoRef
	switch {
	case m.level == levelRepos:
		if repo, ok := m.repos.selected(); ok {
			r = repo.RepoRef
		}
	case m.onStar():
		r = starRepo(m.star.selected())
	default:
		r = m.boxes.repo
	}
	return r
}

// starRepo is the repo a ★ box item belongs to; zero for a group, a host-wide row or an empty box.
func starRepo(item any) domain.RepoRef {
	switch it := item.(type) {
	case renovate.RepoSummary:
		return it.Repo
	case renovate.Member:
		return it.Repo
	case renovate.Dashboard:
		return it.Repo
	}
	return domain.RepoRef{}
}

// renovateRunDialog asks whether to run Renovate for the repo in context or, when only is zero, for all repos.
func renovateRunDialog(only domain.RepoRef) *dialog {
	return &dialog{isRenovate: true, runRepo: only}
}

// runRenovate dispatches the run in the background; its result arrives as a renovateRunMsg.
func (m *Model) runRenovate(only domain.RepoRef) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		return renovateRunMsg{only: only, err: svc.RunRenovate(ctx, only)}
	}
}

func (m *Model) renovateRunDone(msg renovateRunMsg) {
	switch {
	case msg.err != nil:
		m.setError(msg.err)
	case msg.only == (domain.RepoRef{}):
		m.setInfo("Renovate run started for all repos")
	default:
		m.setInfo("Renovate run started for " + msg.only.String())
	}
}

// renovateRunKey moves between the two options when a repo is in context, and confirms the one under the cursor.
func (m *Model) renovateRunKey(msg tea.KeyPressMsg) tea.Cmd {
	d, k := m.dialog, m.keys
	hasRepo := d.runRepo != (domain.RepoRef{})
	switch {
	case key.Matches(msg, k.Down):
		if hasRepo {
			d.cursor = 1
		}
	case key.Matches(msg, k.Up):
		d.cursor = 0
	case key.Matches(msg, k.Confirm):
		only := d.runRepo
		if d.cursor == 1 {
			only = domain.RepoRef{}
		}
		m.dialog = nil
		return m.runRenovate(only)
	}
	return nil
}
