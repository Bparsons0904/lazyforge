package ui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type starBox int

const (
	starByRepo starBox = iota
	starGroups
	starPRs
	starDashboards
	starCI
	starBoxes = 5
)

const maxGroupIcons = 6

type starTarget struct {
	Repo domain.RepoRef
	N    int
}

// renovateScannedMsg carries one repo's scan; seq ties it to the scan that asked for it.
type renovateScannedMsg struct {
	seq  int
	repo domain.RepoRef
	scan renovate.RepoScan
	err  error
}

// starModel is the ★ Renovate view's state; it exists only while the cursor is on the ★ row.
type starModel struct {
	seq    int
	cancel context.CancelFunc
	refs   []domain.RepoRef // the repos the running scan covers
	fresh  bool             // the running scan refetches every repo (r or the tick)
	cov    *core.Coverage
	scans  map[domain.RepoRef]renovate.RepoScan
	view   renovate.View
	focus  starBox
	cursor [starBoxes]int
	marked map[starTarget]bool
}

func (m Model) onStar() bool { return m.repos.loaded && m.repos.cursor == 0 }

func scanRepo(ctx context.Context, svc *core.Service, seq int, r domain.Repo) tea.Cmd {
	return func() tea.Msg {
		scan, err := svc.RenovateScan(ctx, r)
		return renovateScannedMsg{seq: seq, repo: r.RepoRef, scan: scan, err: err}
	}
}

// stopScan cancels the running scan and bumps seq so its late results are dropped.
func (m *Model) stopScan() {
	s := &m.star
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.seq++
	s.cov, s.scans, s.refs = nil, nil, nil
}

// startScan scans every repo. With cached=true cache hits count as scanned and only misses are fetched;
// otherwise the cache only seeds the display and every repo is refetched.
func (m *Model) startScan(cached bool) tea.Cmd {
	m.stopScan()
	ctx, cancel := context.WithCancel(m.ctx)
	s := &m.star
	s.cancel = cancel
	s.fresh = !cached
	s.refs = repoRefs(m.repos.repos)
	s.cov = core.NewCoverage(s.refs)
	s.scans = map[domain.RepoRef]renovate.RepoScan{}
	var cmds []tea.Cmd
	for _, r := range m.repos.repos {
		if scan, ok := m.svc.PeekRenovateScan(r); ok {
			s.scans[r.RepoRef] = scan
			if cached {
				s.cov.Done(r.RepoRef, nil)
				continue
			}
		}
		cmds = append(cmds, scanRepo(ctx, m.svc, s.seq, r))
	}
	s.rebuild(m.now())
	return tea.Batch(cmds...)
}

// scanned applies one repo's result. A failure is reported by the coverage line, not the status bar.
func (m *Model) scanned(msg renovateScannedMsg) {
	s := &m.star
	if msg.seq != s.seq || s.cov == nil || errors.Is(msg.err, context.Canceled) {
		return
	}
	if msg.err == nil {
		s.scans[msg.repo] = msg.scan
	}
	s.cov.Done(msg.repo, msg.err)
	s.rebuild(m.now())
}

// reseed refreshes the given repos' scans from the cache after a recheck or an action changed it.
func (m *Model) reseed(refs ...domain.RepoRef) {
	s := &m.star
	if s.scans == nil {
		return
	}
	for _, ref := range refs {
		if scan, ok := m.svc.PeekRenovateScan(s.repoOf(ref)); ok {
			s.scans[ref] = scan
		}
	}
	s.rebuild(m.now())
}

func (s *starModel) rebuild(now time.Time) {
	s.view = renovate.Build(slices.Collect(maps.Values(s.scans)), now)
	s.clamp()
}

func (s *starModel) clamp() {
	for b := range s.cursor {
		s.cursor[b] = max(min(s.cursor[b], s.len(starBox(b))-1), 0)
	}
}

func (s starModel) repoOf(ref domain.RepoRef) domain.Repo {
	if sc, ok := s.scans[ref]; ok {
		return sc.Repo
	}
	return domain.Repo{RepoRef: ref}
}

func (s starModel) len(b starBox) int {
	switch b {
	case starByRepo:
		return len(s.view.ByRepo)
	case starGroups:
		return len(s.view.Groups)
	case starPRs:
		return len(s.view.PRs)
	case starDashboards:
		return len(s.view.Dashboards)
	default:
		return len(s.view.CI)
	}
}

func (s starModel) selected() any {
	i := s.cursor[s.focus]
	if i >= s.len(s.focus) {
		return nil
	}
	switch s.focus {
	case starByRepo:
		return s.view.ByRepo[i]
	case starGroups:
		return s.view.Groups[i]
	case starPRs:
		return s.view.PRs[i]
	case starDashboards:
		return s.view.Dashboards[i]
	default:
		return s.view.CI[i]
	}
}

func (s starModel) item() (any, domain.RepoRef) {
	switch it := s.selected().(type) {
	case renovate.Member:
		return it.CR, it.Repo
	case renovate.Dashboard:
		return it.Issue, it.Repo
	}
	return nil, domain.RepoRef{}
}

// targets lists what m would merge: the group under the cursor, else the marked PRs, else the PR under the cursor.
func (s starModel) targets() []core.Target {
	var ts []core.Target
	switch it := s.selected().(type) {
	case renovate.Group:
		if s.focus != starGroups {
			return nil
		}
		for _, mb := range it.Members {
			ts = append(ts, core.Target{Repo: mb.Repo, CR: mb.CR})
		}
	case renovate.Member:
		for _, mb := range s.view.PRs {
			if s.marked[starTarget{mb.Repo, mb.CR.Number}] {
				ts = append(ts, core.Target{Repo: mb.Repo, CR: mb.CR})
			}
		}
		if len(ts) == 0 {
			ts = []core.Target{{Repo: it.Repo, CR: it.CR}}
		}
	}
	return ts
}

func (s *starModel) toggleMark(t starTarget) {
	if s.marked[t] {
		delete(s.marked, t)
		return
	}
	s.mark(t)
}

func (s *starModel) mark(t starTarget) {
	if s.marked == nil {
		s.marked = map[starTarget]bool{}
	}
	s.marked[t] = true
}

func starTitle(b starBox, term string) string {
	switch b {
	case starByRepo:
		return "By repo"
	case starGroups:
		return "Updates by dependency"
	case starPRs:
		return "Renovate " + term + "s"
	case starDashboards:
		return "Dashboards"
	default:
		return "Failing / running CI"
	}
}

// groupText is a group's row label with its from versions folded in: "postgres 16.4 → 17.0".
func groupText(g renovate.Group) string {
	if g.Batched {
		return g.Label
	}
	pkg, to, ok := strings.Cut(g.Label, " → ")
	if !ok {
		return g.Label
	}
	return pkg + " " + strings.Join(g.Froms, ", ") + " → " + to
}

// memberText is the update a PR carries, or its title when it carries several or none.
func memberText(mb renovate.Member) string {
	if us := mb.CR.Renovate; len(us) == 1 {
		return fmt.Sprintf("%s %s → %s", us[0].Package, us[0].From, us[0].To)
	}
	return mb.CR.Title
}

func memberAge(now time.Time, cr domain.ChangeRequest) string {
	if cr.CreatedAt.IsZero() {
		return age(now, cr.UpdatedAt)
	}
	return age(now, cr.CreatedAt)
}

func pendingEntries(d renovate.Dashboard) int {
	n := 0
	for _, e := range d.Entries {
		if !e.Checked && e.Branch != "" {
			n++
		}
	}
	return n
}

func (s starModel) crumb() string {
	switch it := s.selected().(type) {
	case renovate.RepoSummary:
		return it.Repo.String()
	case renovate.Group:
		return groupText(it)
	case renovate.Member:
		return fmt.Sprintf("%s #%d", it.Repo, it.CR.Number)
	case renovate.Dashboard:
		return fmt.Sprintf("%s #%d", it.Repo, it.Issue.Number)
	}
	return ""
}

// render draws the five boxes as a w×h column. focus < 0 is the preview: nothing is selected,
// but [1] keeps the larger share because its totals and per-repo rows are what the preview is for.
func (s starModel) render(w, h, focus int, active bool, term string, now time.Time) string {
	hs := splitHeights(h, starBoxes, max(focus, 0))
	panes := make([]string, 0, starBoxes)
	for i := range starBoxes {
		b := starBox(i)
		focused := i == focus
		title := style.StarAccents.Title(i, focused && active).Render(fmt.Sprintf("[%d] %s", i+1, starTitle(b, term)))
		if s.cov != nil {
			title += " " + style.Count.Render(fmt.Sprint(s.len(b)))
		}
		panes = append(panes, frameWith(style.StarAccents.Border(i, focused && active), title, s.rows(b, w-4, hs[i]-2, focused, now), w, hs[i]))
	}
	return strings.Join(panes, "\n")
}

func (s starModel) header() []string {
	var out []string
	if !s.cov.Complete() {
		l := style.Faint.Render(fmt.Sprintf("scanned %d of %d repositories", s.cov.Scanned(), s.cov.Total()))
		if n := len(s.cov.Failed()); n > 0 {
			l += style.StatusErr.Render(fmt.Sprintf(" · %d failed", n))
		}
		out = append(out, l)
	}
	t := s.view.Totals
	return append(out, style.Text.Render(fmt.Sprintf("All · %d · %dM %dm %dp · %.1f", t.PRs, t.Major, t.Minor, t.Patch, t.Impact)))
}

func (s starModel) rows(b starBox, w, h int, focused bool, now time.Time) []string {
	if s.cov == nil {
		return []string{style.Faint.Render("Loading…")}
	}
	var out []string
	if b == starByRepo {
		out = s.header()
	}
	n := s.len(b)
	if n == 0 {
		if s.cov.Scanned() < s.cov.Total() {
			return append(out, style.Faint.Render("Loading…"))
		}
		return append(out, style.Faint.Render("— none —"))
	}
	avail := max(h-len(out), 0)
	cur := s.cursor[b]
	for i := max(cur-avail+1, 0); i < n && len(out) < h; i++ {
		out = append(out, s.row(b, i, w, focused && i == cur, now))
	}
	return out
}

func (s starModel) row(b starBox, i, w int, selected bool, now time.Time) string {
	base := lipgloss.NewStyle()
	if selected {
		base = style.Selected
	}
	on := func(st lipgloss.Style, t string) string { return st.Inherit(base).Render(t) }
	switch b {
	case starByRepo:
		r := s.view.ByRepo[i]
		// A zero count stays dim so only the update types that exist stand out.
		count := func(n int, unit, typ string) string {
			st := style.Faint
			if n > 0 {
				st = style.UpdateType(typ)
			}
			return on(st, fmt.Sprintf("%d%s", n, unit))
		}
		meta := count(r.Major, "M", "major") + on(style.Faint, " ") + count(r.Minor, "m", "minor") + on(style.Faint, " ") +
			count(r.Patch, "p", "patch") + on(style.Faint, fmt.Sprintf("  %.1f", r.Impact))
		return tagRow("", lipgloss.NewStyle(), r.Repo.String(), style.StarAccents.Text(int(b)), meta, w, base)
	case starGroups:
		g := s.view.Groups[i]
		var meta string
		if n := len(g.Members); n > 1 {
			meta = fmt.Sprintf("×%d ", n)
		}
		meta = on(style.Faint, meta) + on(style.UpdateType(g.UpdateType), g.UpdateType+" ")
		for _, mb := range g.Members[:min(len(g.Members), maxGroupIcons)] {
			meta += ciIcon(mb.CR.CI, base)
		}
		return row(groupText(g), meta, w, base)
	case starDashboards:
		d := s.view.Dashboards[i]
		var meta string
		if n := pendingEntries(d); n > 0 {
			meta = on(style.Faint, fmt.Sprintf("%d pending", n))
		}
		return tagRow(d.Repo.String()+" ", style.StarAccents.Text(int(b)), "⚙ "+d.Issue.Title, style.Text, meta, w, base)
	}
	mb := s.view.PRs[i]
	if b == starCI {
		mb = s.view.CI[i]
	}
	tag, ts := fmt.Sprintf("%s #%d ", mb.Repo, mb.CR.Number), style.StarAccents.Text(int(b))
	label := memberText(mb)
	meta := ciIcon(mb.CR.CI, base) + on(style.Faint, " "+memberAge(now, mb.CR))
	if s.marked[starTarget{mb.Repo, mb.CR.Number}] {
		return on(style.Mark, "◆ ") + tagRow(tag, ts, label, style.Text, meta, w-2, base)
	}
	return tagRow(tag, ts, label, style.Text, meta, w, base)
}

func updateLines(us []domain.RenovateUpdate) []string {
	var out []string
	for _, u := range us {
		out = append(out, style.Faint.Render(fmt.Sprintf("  %s %s → %s (%s)", u.Package, u.From, u.To, u.UpdateType)))
	}
	return out
}

func (s starModel) detail(now time.Time, md func(string) string) string {
	var lines []string
	switch it := s.selected().(type) {
	case renovate.RepoSummary:
		lines = []string{
			style.Heading.Render(it.Repo.String()),
			style.Faint.Render(fmt.Sprintf("%d · %dM %dm %dp · impact %.1f", it.PRs, it.Major, it.Minor, it.Patch, it.Impact)),
			"",
		}
		for _, mb := range s.view.PRs {
			if mb.Repo == it.Repo {
				lines = append(lines, style.Text.Render(fmt.Sprintf("#%d %s", mb.CR.Number, mb.CR.Title))+" "+ciIcon(mb.CR.CI, lipgloss.NewStyle()))
				lines = append(lines, updateLines(mb.CR.Renovate)...)
			}
		}
	case renovate.Group:
		lines = []string{style.Heading.Render(groupText(it))}
		meta := "update: " + it.UpdateType
		if it.Key.Ecosystem != "" {
			meta = "ecosystem: " + it.Key.Ecosystem + " · " + meta
		}
		if len(it.Froms) > 0 {
			meta += " · from: " + strings.Join(it.Froms, ", ")
		}
		lines = append(lines, style.Faint.Render(meta), "")
		for _, mb := range it.Members {
			lines = append(lines, style.Text.Render(fmt.Sprintf("%s #%d %s", mb.Repo, mb.CR.Number, memberText(mb)))+" "+ciIcon(mb.CR.CI, lipgloss.NewStyle()))
		}
	case renovate.Member:
		lines = []string{overview(it.CR, it.Repo, now, md), ""}
		if it.CR.Renovate == nil {
			lines = append(lines, style.StatusErr.Render("Couldn't read the update table"))
		} else {
			lines = append(lines, style.Heading.Render("Updates"))
			lines = append(lines, updateLines(it.CR.Renovate)...)
		}
	case renovate.Dashboard:
		lines = []string{style.Heading.Render(it.Issue.Title), style.Faint.Render(fmt.Sprintf("%s #%d", it.Repo, it.Issue.Number))}
		section := ""
		for _, e := range it.Entries {
			if e.Section != section {
				section = e.Section
				lines = append(lines, "", style.Heading.Render(section))
			}
			box := "☐"
			if e.Checked {
				box = "☑"
			}
			lines = append(lines, style.Text.Render(box+" "+e.Title))
		}
	default:
		return style.Faint.Render("Nothing here.")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) focusStar(i int) bool {
	if i < 0 || i >= starBoxes {
		m.setInfo(fmt.Sprintf("No box [%d] here", i+1))
		return false
	}
	m.star.focus = starBox(i)
	return true
}

func (m *Model) starSetCursor(i int) {
	s := &m.star
	s.cursor[s.focus] = max(min(i, s.len(s.focus)-1), 0)
}

func (m *Model) starBoxesKey(msg tea.KeyPressMsg, gg bool) {
	k, s := m.keys, &m.star
	switch {
	case key.Matches(msg, k.Down):
		m.starSetCursor(s.cursor[s.focus] + 1)
	case key.Matches(msg, k.Up):
		m.starSetCursor(s.cursor[s.focus] - 1)
	case gg:
		m.starSetCursor(0)
	case key.Matches(msg, k.Bottom):
		m.starSetCursor(s.len(s.focus) - 1)
	case key.Matches(msg, k.Jump):
		m.focusStar(jumpIndex(msg))
	case key.Matches(msg, k.NextBox):
		m.focusStar((int(s.focus) + 1) % starBoxes)
	case key.Matches(msg, k.PrevBox):
		m.focusStar((int(s.focus) + starBoxes - 1) % starBoxes)
	case key.Matches(msg, k.Right):
		if s.selected() == nil {
			m.setInfo("This box is empty")
		} else {
			m.level = levelDetails
		}
	case key.Matches(msg, k.Left):
		m.level = levelRepos
	}
}

func (m *Model) starDetailsKey(msg tea.KeyPressMsg, gg bool) {
	if m.scrollKey(msg, gg) {
		return
	}
	k, s := m.keys, &m.star
	switch {
	case key.Matches(msg, k.Jump):
		if m.focusStar(jumpIndex(msg)) {
			m.level = levelBoxes
		}
	case key.Matches(msg, k.NextBox):
		m.focusStar((int(s.focus) + 1) % starBoxes)
		m.level = levelBoxes
	case key.Matches(msg, k.PrevBox):
		m.focusStar((int(s.focus) + starBoxes - 1) % starBoxes)
		m.level = levelBoxes
	case key.Matches(msg, k.Left):
		m.level = levelBoxes
	}
}

func (m *Model) syncStarDetails() {
	bodyH, _, rightW := m.layout()
	m.details.syncText("★ "+fmt.Sprint(m.star.focus)+" "+m.star.crumb(), m.star.detail(m.now(), func(b string) string { return m.details.markdown(b, contentWidth(rightW)) }), rightW, bodyH)
}

// syncStarKeys enables each action key only where the ★ row under the cursor supports it.
func (m *Model) syncStarKeys() {
	k, s := &m.keys, m.star
	item, ref := s.item()
	can := func(a forge.Action) bool { return m.svc.Can(a, s.repoOf(ref)).OK }
	_, isCR := item.(domain.ChangeRequest)
	_, isIssue := item.(domain.Issue)
	k.Merge.SetEnabled(m.starCanMerge())
	k.Mark.SetEnabled(isCR && (s.focus == starPRs || s.focus == starCI) && can(forge.ActMerge))
	k.Approve.SetEnabled(isCR && can(forge.ActApprove))
	k.CloseItem.SetEnabled((isCR || isIssue) && can(forge.ActClose))
	k.Comment.SetEnabled((isCR || isIssue) && can(forge.ActComment))
	k.Open.SetEnabled(webURL(item) != "")
	k.Rerun.SetEnabled(false)
}

func (m Model) starCanMerge() bool {
	if m.star.focus == starByRepo || m.star.focus == starDashboards {
		return false
	}
	return slices.ContainsFunc(m.star.targets(), func(t core.Target) bool {
		return m.svc.Can(forge.ActMerge, m.star.repoOf(t.Repo)).OK
	})
}
