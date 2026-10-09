package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

type recheckedMsg struct {
	repo    domain.RepoRef
	results []core.Checked
}

type mergeDoneMsg struct {
	repo    domain.RepoRef
	results []core.MergeResult
}

// actionDoneMsg reports approve, close, comment and open; verb is the past tense the status bar shows.
type actionDoneMsg struct {
	repo domain.RepoRef
	item forge.ItemRef
	verb string
	// closed marks a close so actionDone can unmark the change request.
	closed bool
	err    error
}

type editorDoneMsg struct {
	item forge.ItemRef
	path string
	err  error
}

var errEmptyComment = errors.New("comment is empty")

// openBrowser starts the platform's URL opener without waiting for it.
func openBrowser(ctx context.Context, u string) error {
	name, args := browserCommand(runtime.GOOS, u)
	return osexec.CommandContext(ctx, name, args...).Start()
}

func browserCommand(goos, u string) (name string, args []string) {
	switch goos {
	case "darwin":
		return "open", []string{u}
	case "windows":
		// rundll32 opens the URL without cmd.exe, which would read a & in the URL as a command separator.
		return "rundll32", []string{"url.dll,FileProtocolHandler", u}
	}
	return "xdg-open", []string{u}
}

func defaultEditor(goos string) string {
	if goos == "windows" {
		return "notepad"
	}
	return "vi"
}

// syncKeys enables each action key only where it applies, which also hides it from the hints and help.
func (m *Model) syncKeys() {
	k := &m.keys
	if m.onStar() && m.level != levelRepos {
		m.syncStarKeys()
		return
	}
	repo, _ := m.repos.selected()
	item := m.boxes.selected()
	if m.level == levelRepos {
		item = nil
	}
	can := func(a forge.Action) bool { return m.svc.Can(a, repo).OK }
	_, isCR := item.(domain.ChangeRequest)
	_, isIssue := item.(domain.Issue)
	_, isRun := item.(domain.Run)
	k.Merge.SetEnabled(isCR && can(forge.ActMerge))
	k.Mark.SetEnabled(isCR && can(forge.ActMerge))
	k.Approve.SetEnabled(isCR && can(forge.ActApprove))
	k.CloseItem.SetEnabled((isCR || isIssue) && can(forge.ActClose))
	k.Comment.SetEnabled((isCR || isIssue) && can(forge.ActComment))
	k.Labels.SetEnabled((isCR || isIssue) && can(forge.ActLabels))
	k.Open.SetEnabled(webURL(m.openTarget(item)) != "")
	k.Rerun.SetEnabled(isRun && webURL(item) != "")
}

func webURL(item any) string {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return it.WebURL
	case domain.Issue:
		return it.WebURL
	case domain.Run:
		return it.WebURL
	case domain.Release:
		return it.WebURL
	case domain.Branch:
		return it.WebURL
	case domain.TreeEntry:
		return it.WebURL
	}
	return ""
}

// openTarget is what o opens: the cursor branch on the Branches tab, the cursor entry on the Files tab, otherwise item.
func (m Model) openTarget(item any) any {
	if m.branchesActive() {
		return m.boxes.branches.list[m.details.branchCur]
	}
	if e, ok := m.details.cursorEntry(m.boxes.files); ok && m.filesActive() {
		return e
	}
	return item
}

func itemRef(r domain.RepoRef, item any) forge.ItemRef {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return forge.ItemRef{Repo: r, Kind: forge.ItemChangeRequest, Number: it.Number}
	case domain.Issue:
		return forge.ItemRef{Repo: r, Kind: forge.ItemIssue, Number: it.Number}
	}
	return forge.ItemRef{}
}

// actionKey handles the action keys at the boxes and details levels.
func (m *Model) actionKey(msg tea.KeyPressMsg) (cmd tea.Cmd, ok bool) {
	if m.onStar() {
		return m.starActionKey(msg)
	}
	k, b := m.keys, &m.boxes
	item := m.openTarget(b.selected())
	switch {
	case key.Matches(msg, k.Mark):
		if cr, ok := item.(domain.ChangeRequest); ok {
			b.toggleMark(cr.Number)
		}
	case key.Matches(msg, k.Close):
		b.marked = nil
	case key.Matches(msg, k.Merge):
		return m.recheck(), true
	default:
		return m.itemActionKey(msg, itemRef(b.repo, item), item)
	}
	return nil, true
}

// itemActionKey handles the keys that act on one item the same way in the repo boxes and the ★ boxes.
func (m *Model) itemActionKey(msg tea.KeyPressMsg, ref forge.ItemRef, item any) (cmd tea.Cmd, ok bool) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Approve):
		svc, ctx := m.svc, m.ctx
		return func() tea.Msg {
			return actionDoneMsg{repo: ref.Repo, item: ref, verb: "Approved", err: svc.Approve(ctx, ref.Repo, ref.Number)}
		}, true
	case key.Matches(msg, k.CloseItem):
		m.dialog = closeDialog(ref, item)
	case key.Matches(msg, k.Labels):
		return m.openLabels(ref), true
	case key.Matches(msg, k.Comment):
		return editComment(m.ctx, ref), true
	case key.Matches(msg, k.Open), key.Matches(msg, k.Rerun):
		open, u := m.openURL, webURL(item)
		return func() tea.Msg {
			if err := open(u); err != nil {
				return actionDoneMsg{repo: ref.Repo, item: ref, err: fmt.Errorf("open %s: %w", u, err)}
			}
			return nil
		}, true
	default:
		return nil, false
	}
	return nil, true
}

// recheck re-fetches the marked change requests, or the one under the cursor, before the merge dialog opens.
func (m *Model) recheck() tea.Cmd {
	b := &m.boxes
	var ts []core.Target
	for _, cr := range b.crs {
		if b.marked[cr.Number] {
			ts = append(ts, core.Target{Repo: b.repo, CR: cr})
		}
	}
	if len(ts) == 0 {
		cr, ok := b.selected().(domain.ChangeRequest)
		if !ok {
			return nil
		}
		ts = []core.Target{{Repo: b.repo, CR: cr}}
	}
	svc, ctx, r := m.svc, m.selCtx, b.repo
	return func() tea.Msg { return recheckedMsg{repo: r, results: svc.Recheck(ctx, ts)} }
}

// rechecked leaves a target whose recheck failed marked and out of the dialog.
func (m *Model) rechecked(msg recheckedMsg) tea.Cmd {
	// A late recheck must not replace a dialog the user already opened or confirmed.
	if msg.repo != m.boxes.repo || m.dialog != nil || m.labels != nil {
		return nil
	}
	var open []core.Target
	var merged []string
	var failed error
	for _, c := range msg.results {
		n := c.Target.CR.Number
		switch {
		case c.Err != nil:
			if !errors.Is(c.Err, context.Canceled) {
				failed = c.Err
			}
		case c.Target.CR.State == domain.StateOpen:
			open = append(open, c.Target)
		default:
			delete(m.boxes.marked, n)
			if c.Target.CR.State == domain.StateMerged {
				merged = append(merged, fmt.Sprintf("#%d", n))
			}
		}
	}
	switch {
	case failed != nil:
		m.setError(failed)
	case len(merged) > 0:
		m.setInfo("Already merged: " + strings.Join(merged, ", "))
	}
	if len(open) > 0 {
		repo, _ := m.repos.selected()
		m.dialog = mergeDialog(open, mergeOpts{strategy: repo.MergeStyle, greenOnly: m.svc.RequiresGreenCI, renovateUser: m.svc.RenovateUser()})
	}
	return m.loadBoxes(true)
}

// beginMerge returns the context for a merge run; Esc in the running dialog cancels it through mergeCancel.
func (m *Model) beginMerge() context.Context {
	ctx, cancel := context.WithCancel(m.ctx)
	m.mergeCancel = cancel
	return ctx
}

// endMerge closes the run, fills the dialog's results and reports the summary.
func (m *Model) endMerge(results []core.MergeResult) {
	if m.mergeCancel != nil {
		m.mergeCancel()
		m.mergeCancel = nil
	}
	if m.dialog != nil && m.dialog.phase == phaseRunning {
		m.dialog.phase, m.dialog.results = phaseDone, results
	}
	n := 0
	for _, r := range results {
		if r.Outcome == core.OutcomeMerged {
			n++
		}
	}
	if s := fmt.Sprintf("Merged %d of %d", n, len(results)); n < len(results) {
		m.setError(errors.New(s))
	} else {
		m.setInfo(s)
	}
}

func (m *Model) startMerge(ts []core.Target) tea.Cmd {
	ctx := m.beginMerge()
	svc, r := m.svc, m.boxes.repo
	return func() tea.Msg { return mergeDoneMsg{repo: r, results: svc.Merge(ctx, ts)} }
}

func (m *Model) mergeDone(msg mergeDoneMsg) tea.Cmd {
	m.endMerge(msg.results)
	if msg.repo == m.boxes.repo {
		for _, r := range msg.results {
			if r.Outcome == core.OutcomeMerged {
				delete(m.boxes.marked, r.Target.CR.Number)
			}
		}
		return m.loadBoxes(true)
	}
	return nil
}

func (m *Model) actionDone(msg actionDoneMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, errEmptyComment):
		m.setInfo("Comment cancelled")
		return nil
	case msg.err != nil:
		m.setError(msg.err)
		return nil
	}
	m.setInfo(fmt.Sprintf("%s #%d", msg.verb, msg.item.Number))
	if m.onStar() {
		if msg.item.Kind == forge.ItemChangeRequest && msg.closed {
			delete(m.star.marked, starTarget{msg.repo, msg.item.Number})
		}
		m.reseed(msg.repo)
		return nil
	}
	if msg.repo != m.boxes.repo {
		return nil
	}
	if msg.item.Kind == forge.ItemChangeRequest && msg.closed {
		delete(m.boxes.marked, msg.item.Number)
	}
	return m.loadBoxes(true)
}

func (m *Model) closeItem(item forge.ItemRef) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		return actionDoneMsg{repo: item.Repo, item: item, verb: "Closed", closed: true, err: svc.Close(ctx, item)}
	}
}

// editComment opens $EDITOR (vi, or notepad on Windows, when unset) on a fresh temp file; the result arrives as editorDoneMsg.
func editComment(ctx context.Context, item forge.ItemRef) tea.Cmd {
	return func() tea.Msg {
		f, err := os.CreateTemp("", "lazyforge-comment-*.md")
		if err != nil {
			return editorDoneMsg{item: item, err: fmt.Errorf("create comment file: %w", err)}
		}
		path := f.Name()
		if err := f.Close(); err != nil {
			return editorDoneMsg{item: item, path: path, err: err}
		}
		args := strings.Fields(os.Getenv("EDITOR"))
		if len(args) == 0 {
			args = []string{defaultEditor(runtime.GOOS)}
		}
		c := osexec.CommandContext(ctx, args[0], append(args[1:], path)...)
		// ExecProcess's cmd only returns a message; running it here keeps the file creation off Update.
		return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{item: item, path: path, err: err} })()
	}
}

func (m *Model) postComment(msg editorDoneMsg) tea.Cmd {
	svc, ctx, item := m.svc, m.ctx, msg.item
	return func() tea.Msg {
		done := actionDoneMsg{repo: item.Repo, item: item, verb: "Commented on"}
		if msg.path != "" {
			defer func() { _ = os.Remove(msg.path) }()
		}
		if msg.err != nil {
			done.err = fmt.Errorf("editor: %w", msg.err)
			return done
		}
		raw, err := os.ReadFile(msg.path)
		body := strings.TrimSpace(string(raw))
		switch {
		case err != nil:
			done.err = fmt.Errorf("read comment: %w", err)
		case body == "":
			done.err = errEmptyComment
		default:
			done.err = svc.Comment(ctx, item, body)
		}
		return done
	}
}
