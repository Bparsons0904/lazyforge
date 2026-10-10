package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// starRecheckedMsg carries the rechecked merge targets; group is set for a [2] group merge.
type starRecheckedMsg struct {
	results []core.Checked
	group   bool
}

type starMergeDoneMsg struct{ results []core.MergeResult }

// starActionKey handles the action keys in the ★ boxes and details; disabled keys never match.
func (m *Model) starActionKey(msg tea.KeyPressMsg) (cmd tea.Cmd, ok bool) {
	k, s := m.keys, &m.star
	item, repo := s.item()
	switch {
	case key.Matches(msg, k.Mark):
		if mb, ok := s.selected().(renovate.Member); ok {
			s.toggleMark(starTarget{mb.Repo, mb.CR.Number})
		}
	case key.Matches(msg, k.Close):
		s.marked = nil
	case key.Matches(msg, k.Merge):
		return m.starRecheck(), true
	default:
		return m.itemActionKey(msg, itemRef(repo, item), m.starOpenTarget(item))
	}
	return nil, true
}

func (m *Model) starRecheck() tea.Cmd {
	ts := m.star.targets()
	if len(ts) == 0 {
		return nil
	}
	svc, ctx, group := m.svc, m.ctx, m.star.focus == starGroups
	return func() tea.Msg { return starRecheckedMsg{results: svc.Recheck(ctx, ts), group: group} }
}

// starRechecked opens the merge dialog over the still-open targets the user may merge.
func (m *Model) starRechecked(msg starRecheckedMsg) {
	// A late recheck must not replace a dialog the user already opened or confirmed.
	if m.dialog != nil || m.labels != nil || !m.onStar() {
		return
	}
	var open []core.Target
	var skipped, merged []string
	var failed error
	repos := map[domain.RepoRef]bool{}
	for _, c := range msg.results {
		t := c.Target
		label := fmt.Sprintf("%s #%d", t.Repo, t.CR.Number)
		repos[t.Repo] = true
		switch {
		case c.Err != nil:
			if !errors.Is(c.Err, context.Canceled) {
				failed = c.Err
				skipped = append(skipped, label+": recheck failed")
			}
		case t.CR.State != domain.StateOpen:
			delete(m.star.marked, starTarget{t.Repo, t.CR.Number})
			if t.CR.State == domain.StateMerged {
				merged = append(merged, label)
			}
			skipped = append(skipped, label+": no longer open")
		default:
			if a := m.svc.Can(forge.ActMerge, m.star.repoOf(t.Repo)); !a.OK {
				skipped = append(skipped, label+": "+a.Reason)
				continue
			}
			open = append(open, t)
		}
	}
	var refs []domain.RepoRef
	for r := range repos {
		refs = append(refs, r)
	}
	m.reseed(refs...)
	switch {
	case len(open) == 0 && len(skipped) > 0:
		m.setError(errors.New("Nothing to merge: " + strings.Join(skipped, "; ")))
	case failed != nil:
		m.setError(failed)
	case len(merged) > 0:
		m.setInfo("Already merged: " + strings.Join(merged, ", "))
	}
	if len(open) == 0 {
		return
	}
	o := mergeOpts{greenOnly: m.svc.RequiresGreenCI, skipped: skipped, star: true, renovateUser: m.svc.RenovateUser()}
	if len(distinctRepos(open)) == 1 {
		o.strategy = m.star.repoOf(open[0].Repo).MergeStyle
	}
	if c := m.star.cov; msg.group && c != nil && !c.Complete() {
		o.coverage = fmt.Sprintf("%d repositories not scanned; this may not be every %s for this update", c.Total()-c.Scanned()+len(c.Failed()), m.info.ChangeRequestTerm)
	}
	m.dialog = mergeDialog(open, o)
}

func (m *Model) startStarMerge(ts []core.Target) tea.Cmd {
	ctx := m.beginMerge()
	svc := m.svc
	return func() tea.Msg { return starMergeDoneMsg{results: svc.Merge(ctx, ts)} }
}

// starMergeDone marks every target that didn't merge, so the user can retry or inspect it, and unmarks the merged.
func (m *Model) starMergeDone(msg starMergeDoneMsg) {
	m.endMerge(msg.results)
	var refs []domain.RepoRef
	for _, r := range msg.results {
		t := starTarget{r.Target.Repo, r.Target.CR.Number}
		refs = append(refs, t.Repo)
		if r.Outcome == core.OutcomeMerged {
			delete(m.star.marked, t)
		} else {
			m.star.mark(t)
		}
	}
	m.reseed(refs...)
}

func distinctRepos(ts []core.Target) map[domain.RepoRef]bool {
	out := map[domain.RepoRef]bool{}
	for _, t := range ts {
		out[t.Repo] = true
	}
	return out
}
