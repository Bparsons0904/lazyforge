package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type dialogPhase int

const (
	phaseConfirm dialogPhase = iota
	phaseRunning
	phaseDone
)

// dialog confirms a merge or a close; a close has one target in item and no running or done phase.
type dialog struct {
	isClose   bool
	phase     dialogPhase
	targets   []core.Target // merge only
	strategy  string
	greenOnly func(domain.RepoRef) bool
	coverage  string   // warning line; "" for none
	skipped   []string // "owner/name #n: reason"
	star      bool
	prefix    bool
	renovUser string // host's renovate_user, for spotting author-detected Renovate PRs
	results   []core.MergeResult
	item      forge.ItemRef // close only
	label     string        // close only: "#n title"
	ci        string        // close only: the CR's CI icon
}

type mergeOpts struct {
	strategy     string // "" shows the repos' own default
	greenOnly    func(domain.RepoRef) bool
	coverage     string
	skipped      []string
	star         bool
	renovateUser string
}

// mergeDialog dedups ts by (repo, number), keeping the first occurrence, for the dialog's own listing.
func mergeDialog(ts []core.Target, o mergeOpts) *dialog {
	var uniq []core.Target
	repos := map[domain.RepoRef]bool{}
	for _, t := range ts {
		if !slices.ContainsFunc(uniq, func(u core.Target) bool { return u.Repo == t.Repo && u.CR.Number == t.CR.Number }) {
			uniq = append(uniq, t)
			repos[t.Repo] = true
		}
	}
	if o.strategy == "" {
		o.strategy = "repo default"
		if len(repos) > 1 {
			o.strategy = "each repo's default"
		}
	}
	return &dialog{targets: uniq, strategy: o.strategy, greenOnly: o.greenOnly, coverage: o.coverage, skipped: o.skipped, star: o.star, renovUser: o.renovateUser, prefix: o.star || len(repos) > 1}
}

// targetLabel names a target for the dialog, with its repo when the listing needs one.
func (d dialog) targetLabel(t core.Target) string {
	if d.prefix {
		return fmt.Sprintf("%s #%d %s", t.Repo, t.CR.Number, t.CR.Title)
	}
	return fmt.Sprintf("#%d %s", t.CR.Number, t.CR.Title)
}

func closeDialog(ref forge.ItemRef, item any) *dialog {
	d := &dialog{isClose: true, item: ref}
	switch it := item.(type) {
	case domain.ChangeRequest:
		d.label, d.ci = fmt.Sprintf("#%d %s", it.Number, it.Title), " "+ciIcon(it.CI, lipgloss.NewStyle())
	case domain.Issue:
		d.label = fmt.Sprintf("#%d %s", it.Number, it.Title)
	}
	return d
}

func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	d, k := m.dialog, m.keys
	switch d.phase {
	case phaseConfirm:
		switch {
		case key.Matches(msg, k.Close):
			m.dialog = nil
		case key.Matches(msg, k.Confirm) && d.isClose:
			m.dialog = nil
			return m.closeItem(d.item)
		case key.Matches(msg, k.Confirm):
			d.phase = phaseRunning
			if d.star {
				return m.startStarMerge(d.targets)
			}
			return m.startMerge(d.targets)
		}
	case phaseRunning:
		if key.Matches(msg, k.Close) && m.mergeCancel != nil {
			m.mergeCancel()
		}
	case phaseDone:
		if key.Matches(msg, k.Close, k.Confirm) {
			m.dialog = nil
		}
	}
	return nil
}

func (d dialog) title(term string) string {
	switch {
	case d.isClose:
		return fmt.Sprintf("Close #%d", d.item.Number)
	case len(d.targets) == 1 && d.prefix:
		return fmt.Sprintf("Merge %s #%d", d.targets[0].Repo, d.targets[0].CR.Number)
	case len(d.targets) == 1:
		return fmt.Sprintf("Merge #%d", d.targets[0].CR.Number)
	}
	return fmt.Sprintf("Merge %d %ss", len(d.targets), term)
}

func (d dialog) lines() []string {
	if d.isClose {
		return []string{style.Text.Render(d.label) + d.ci}
	}
	switch d.phase {
	case phaseRunning:
		return []string{style.Text.Render(fmt.Sprintf("Merging %d…", len(d.targets)))}
	case phaseDone:
		var out []string
		for _, r := range d.results {
			out = append(out, style.Text.Render(d.targetLabel(r.Target)), "  "+outcome(r))
		}
		return out
	}
	out := []string{style.Faint.Render("Strategy: " + d.strategy)}
	if d.coverage != "" {
		out = append(out, style.CIRunning.Render(d.coverage))
	}
	for _, s := range d.skipped {
		out = append(out, style.Faint.Render("Skipped "+s))
	}
	for _, t := range d.targets {
		cr := t.CR
		out = append(out, style.Text.Render(d.targetLabel(t))+" "+ciIcon(cr.CI, lipgloss.NewStyle()))
		for _, u := range cr.Renovate {
			out = append(out, style.Faint.Render(fmt.Sprintf("  %s %s → %s", u.Package, u.From, u.To)))
		}
		if cr.Renovate == nil && (d.star || renovate.IsRenovate(cr, d.renovUser)) {
			out = append(out, style.Faint.Render("  packages unknown"))
		}
		if !cr.CI.Green() {
			if d.greenOnly != nil && d.greenOnly(t.Repo) {
				out = append(out, style.StatusErr.Render("  "+ciText(cr.CI)+", will be refused (only merge when CI is green)"))
			} else {
				out = append(out, style.CIRunning.Render("  "+ciText(cr.CI)+", will merge anyway"))
			}
		}
	}
	return out
}

func outcome(r core.MergeResult) string {
	switch r.Outcome {
	case core.OutcomeMerged:
		return style.CIPass.Render("merged")
	case core.OutcomeRefused:
		return style.StatusErr.Render("refused: " + r.Err.Error())
	case core.OutcomeFailed:
		return style.StatusErr.Render("failed: " + r.Err.Error())
	case core.OutcomeUnknown:
		return style.CIRunning.Render("unknown: timed out, may have merged")
	}
	return style.Faint.Render("not started")
}

func (d dialog) hints(k keyMap) []key.Binding {
	switch d.phase {
	case phaseRunning:
		return []key.Binding{hint(k.Close, "esc", "stop starting new merges")}
	case phaseDone:
		return []key.Binding{hint(k.Close, "esc/enter", "close")}
	}
	return []key.Binding{k.Confirm, hint(k.Close, "esc", "cancel")}
}

func (d dialog) view(w, h int, term string, keys keyMap, hm help.Model) string {
	hm.SetWidth(max(w-4, 1))
	lines := append(d.lines(), "", hm.ShortHelpView(d.hints(keys)))
	title := style.ActiveTitle.Render(d.title(term))
	bw := lipgloss.Width(title) + 6
	for _, l := range lines {
		bw = max(bw, lipgloss.Width(l)+4)
	}
	box := frame(title, lines, min(bw, w), min(len(lines)+2, h), true)
	return fitLines(strings.Split(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box), "\n"), w, h)
}
