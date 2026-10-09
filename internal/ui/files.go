package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

// filesTab is the Files tab's index in tabs for a Repo.
const filesTab = 1

// The list column takes a third of the content width, between filesListMin and filesListMax columns.
// Below filesSplitMin there is no room for two columns, so one shows at a time.
const (
	filesSplitMin = 40
	filesListMin  = 16
	filesListMax  = 40
)

// filesState is the selected repo's Files tab data. A missing dir or path is still loading, unless failed marks it.
type filesState struct {
	dirs     map[string][]domain.TreeEntry
	previews map[string]domain.FilePreview
	failed   map[string]bool
}

func (fs *filesState) setDir(dir string, es []domain.TreeEntry) {
	if fs.dirs == nil {
		fs.dirs = map[string][]domain.TreeEntry{}
	}
	fs.dirs[dir] = es
	delete(fs.failed, dir)
}

func (fs *filesState) setPreview(path string, p domain.FilePreview) {
	if fs.previews == nil {
		fs.previews = map[string]domain.FilePreview{}
	}
	fs.previews[path] = p
	delete(fs.failed, path)
}

func (fs *filesState) fail(key string) {
	if fs.failed == nil {
		fs.failed = map[string]bool{}
	}
	fs.failed[key] = true
}

// filesActive reports whether the Files tab takes the cursor keys: the Repo's details on the Files tab, at the details level.
func (m Model) filesActive() bool {
	_, isRepo := m.boxes.selected().(domain.Repo)
	return m.level == levelDetails && isRepo && m.details.tab == filesTab && m.boxes.showFiles
}

// filesKey handles the Files tab's keys and reports whether msg was one of them.
func (m *Model) filesKey(msg tea.KeyPressMsg, gg bool) (tea.Cmd, bool) {
	if m.details.filesFocus {
		return nil, m.filesPreviewKey(msg, gg)
	}
	return m.filesListKey(msg, gg)
}

// filesListKey moves the list cursor, enters a directory or moves up, and reports whether msg was one of its keys.
func (m *Model) filesListKey(msg tea.KeyPressMsg, gg bool) (tea.Cmd, bool) {
	k, d, fs := m.keys, &m.details, m.boxes.files
	bodyH, _, _ := m.layout()
	ch := max(bodyH-2, 0)
	switch {
	case key.Matches(msg, k.Left):
		if d.filesDir == "" {
			return nil, false
		}
		return m.filesUp(), true
	case key.Matches(msg, k.Right):
		return m.filesEnter(), true
	}
	n := len(fs.dirs[d.filesDir])
	was := d.curIndex(fs)
	cur := was
	switch {
	case key.Matches(msg, k.Down):
		cur++
	case key.Matches(msg, k.Up):
		cur--
	case key.Matches(msg, k.HalfDown):
		cur += max(ch/2, 1)
	case key.Matches(msg, k.HalfUp):
		cur -= max(ch/2, 1)
	case gg:
		cur = 0
	case key.Matches(msg, k.Bottom):
		cur = n - 1
	default:
		return nil, false
	}
	d.filesCur = max(min(cur, n-1), 0)
	if d.filesCur == was {
		return nil, true
	}
	d.filesOff = 0
	return m.landOnEntry(), true
}

// filesPreviewKey scrolls the preview and gives the list back on h, and reports whether msg was one of its keys.
func (m *Model) filesPreviewKey(msg tea.KeyPressMsg, gg bool) bool {
	k, d := m.keys, &m.details
	bodyH, _, _ := m.layout()
	ch := max(bodyH-2, 0)
	maxOff := max(len(d.filesPane(m.boxes.files))-ch, 0)
	switch {
	case key.Matches(msg, k.Left):
		d.filesFocus, d.filesOff = false, 0
		return true
	case key.Matches(msg, k.Down):
		d.filesOff++
	case key.Matches(msg, k.Up):
		d.filesOff--
	case key.Matches(msg, k.HalfDown):
		d.filesOff += max(ch/2, 1)
	case key.Matches(msg, k.HalfUp):
		d.filesOff -= max(ch/2, 1)
	case gg:
		d.filesOff = 0
	case key.Matches(msg, k.Bottom):
		d.filesOff = maxOff
	default:
		return false
	}
	d.filesOff = max(min(d.filesOff, maxOff), 0)
	return true
}

// filesUp moves to the parent directory, with the cursor on the directory it came from.
func (m *Model) filesUp() tea.Cmd {
	d, fs := &m.details, m.boxes.files
	child := d.filesDir
	d.filesDir, d.filesCur, d.filesOff = parentDir(child), 0, 0
	if i := slices.IndexFunc(fs.dirs[d.filesDir], func(e domain.TreeEntry) bool { return e.Path == child }); i >= 0 {
		d.filesCur = i
	}
	return m.landOnEntry()
}

// filesEnter opens the cursor entry: a directory becomes the current one, and a file with loaded text takes the preview's focus.
func (m *Model) filesEnter() tea.Cmd {
	d, fs := &m.details, &m.boxes.files
	e, ok := d.cursorEntry(*fs)
	switch {
	case !ok:
	case e.Type == domain.EntryDir:
		d.filesDir, d.filesCur, d.filesOff = e.Path, 0, 0
		if _, known := fs.dirs[e.Path]; !known {
			delete(fs.failed, e.Path)
			return loadTree(m.selCtx, m.svc, m.boxes.repo, e.Path)
		}
		return m.landOnEntry()
	case e.Type == domain.EntryFile && fs.previews[e.Path].Text != "":
		d.filesFocus = true
	}
	return nil
}

// landOnEntry refetches what the cursor entry shows, seeding it from the cache while the fetch runs.
func (m *Model) landOnEntry() tea.Cmd {
	e, ok := m.details.cursorEntry(m.boxes.files)
	if !ok {
		return nil
	}
	fs, r := &m.boxes.files, m.boxes.repo
	delete(fs.failed, e.Path)
	switch e.Type {
	case domain.EntryDir:
		if es, _, ok := m.svc.PeekTree(r, e.Path); ok {
			fs.setDir(e.Path, es)
		}
		return loadTree(m.selCtx, m.svc, r, e.Path)
	case domain.EntryFile:
		if p, _, ok := m.svc.PeekPreview(r, e.Path); ok {
			fs.setPreview(e.Path, p)
		}
		return loadPreview(m.selCtx, m.svc, r, e)
	}
	return nil
}

// treeLoaded stores a listing. For the current directory it keeps the cursor on the same path, and lands on the entry under it when that changed or nothing was listed before.
func (m *Model) treeLoaded(msg treeLoadedMsg) tea.Cmd {
	if m.filesLoadFailed(msg.key, msg.err) {
		return nil
	}
	fs, d, dir := &m.boxes.files, &m.details, msg.key.Ref
	if dir != d.filesDir {
		fs.setDir(dir, msg.entries)
		return nil
	}
	prev, hadPrev := d.cursorEntry(*fs)
	_, hadListing := fs.dirs[dir]
	fs.setDir(dir, msg.entries)
	if i := slices.IndexFunc(msg.entries, func(e domain.TreeEntry) bool { return e.Path == prev.Path }); hadPrev && i >= 0 {
		d.filesCur = i
	}
	cur, has := d.cursorEntry(*fs)
	moved := has && (!hadPrev || cur.Path != prev.Path)
	if moved {
		d.filesOff = 0
	}
	if moved || !hadListing {
		return m.landOnEntry()
	}
	return nil
}

// previewLoaded stores a file's preview.
func (m *Model) previewLoaded(msg previewLoadedMsg) {
	if !m.filesLoadFailed(msg.key, msg.err) {
		m.boxes.files.setPreview(msg.key.Ref, msg.preview)
	}
}

// filesLoadFailed is loadFailed for the Files tab, which also marks the dir or path failed unless the load was canceled.
func (m *Model) filesLoadFailed(k core.Key, err error) bool {
	if !m.loadFailed(k, err) {
		return false
	}
	if err != nil && k.Repo == m.boxes.repo && !errors.Is(err, context.Canceled) {
		m.boxes.files.fail(k.Ref)
	}
	return true
}

// filesCrumb is the breadcrumb's directory segment on the Files tab, or "" at the root or off the tab.
func (m Model) filesCrumb() string {
	if m.boxes.focus != boxRepo || m.details.tab != filesTab || !m.boxes.showFiles {
		return ""
	}
	return sanitizeLine(m.details.filesDir)
}

// curIndex is the cursor's index in the current listing, clamped to it; 0 when the listing is empty.
func (d *details) curIndex(fs filesState) int {
	return max(min(d.filesCur, len(fs.dirs[d.filesDir])-1), 0)
}

// cursorEntry is the entry under the cursor in the current listing, and false when there is none.
func (d *details) cursorEntry(fs filesState) (domain.TreeEntry, bool) {
	ents := fs.dirs[d.filesDir]
	if len(ents) == 0 {
		return domain.TreeEntry{}, false
	}
	return ents[d.curIndex(fs)], true
}

// filesText composes the Files tab: the current directory on the left and the cursor entry on the right, exactly ch lines.
func (d *details) filesText(b boxes, cw, ch int) string {
	if !b.showFiles {
		return fitLines(note("Files aren't available on this host"), cw, ch)
	}
	if cw < filesSplitMin {
		if d.filesFocus {
			return fitLines(d.filesPaneRows(b.files, ch), cw, ch)
		}
		return fitLines(d.filesList(b.files, cw, ch), cw, ch)
	}
	lw := min(max(cw/3, filesListMin), filesListMax)
	pw := cw - lw - 3
	left := d.filesList(b.files, lw, ch)
	right := d.filesPaneRows(b.files, ch)
	rows := make([]string, ch)
	for i := range rows {
		rows[i] = fitLine(at(left, i), lw) + style.Faint.Render(" │ ") + fitLine(at(right, i), pw)
	}
	return fitLines(rows, cw, ch)
}

// filesList is the current directory's rows, fitted to w, with the cursor row highlighted.
func (d *details) filesList(fs filesState, w, ch int) []string {
	if n := listingNote(fs, d.filesDir); n != "" {
		return note(n)
	}
	ents := fs.dirs[d.filesDir]
	cur := d.curIndex(fs)
	first := max(cur-ch+1, 0)
	var rows []string
	for i := first; i < len(ents) && len(rows) < ch; i++ {
		row := fitLine(entryName(ents[i]), w)
		switch {
		case i != cur:
		case d.filesFocus:
			row = style.Faint.Render(row)
		case d.branchHL:
			row = style.Selected.Render(row)
		}
		rows = append(rows, row)
	}
	return rows
}

// filesPaneRows is the right column: the cursor entry's lines from d.filesOff, at most ch of them.
// The offset is clamped here, so a resize or a shorter refreshed text can't leave it out of range.
func (d *details) filesPaneRows(fs filesState, ch int) []string {
	lines := d.filesPane(fs)
	d.filesOff = max(min(d.filesOff, len(lines)-ch), 0)
	return lines[d.filesOff:min(d.filesOff+ch, len(lines))]
}

// filesPane is what the cursor entry shows: a directory's names, a file's preview, or the state of either.
func (d *details) filesPane(fs filesState) []string {
	e, ok := d.cursorEntry(fs)
	switch {
	case !ok:
		return nil
	case e.Type == domain.EntryDir:
		if n := listingNote(fs, e.Path); n != "" {
			return note(n)
		}
		var names []string
		for _, c := range fs.dirs[e.Path] {
			names = append(names, entryName(c))
		}
		return names
	case e.Type == domain.EntrySymlink:
		return note("symlink")
	case e.Type == domain.EntrySubmodule:
		return note("submodule")
	}
	p, loaded := fs.previews[e.Path]
	switch {
	case !loaded && fs.failed[e.Path]:
		return note("couldn't load")
	case !loaded:
		return note("Loading…")
	case p.Binary:
		return note("binary file")
	case p.TooLarge:
		return note("too large to preview")
	case p.Text == "":
		return note("— empty —")
	}
	var lines []string
	// A final newline ends the last line; it doesn't start another one.
	for _, l := range strings.Split(strings.TrimSuffix(p.Text, "\n"), "\n") {
		lines = append(lines, sanitizeLine(strings.TrimSuffix(l, "\r")))
	}
	return lines
}

// listingNote is what a directory's column shows instead of its entries, or "" when it has entries to show.
func listingNote(fs filesState, dir string) string {
	switch ents, ok := fs.dirs[dir]; {
	case !ok && fs.failed[dir]:
		return "couldn't load"
	case !ok:
		return "Loading…"
	case len(ents) == 0:
		return "— empty —"
	}
	return ""
}

// entryName is an entry's row text, with a / after a directory's name.
func entryName(e domain.TreeEntry) string {
	if e.Type == domain.EntryDir {
		return sanitizeLine(e.Name) + "/"
	}
	if e.Type == domain.EntrySymlink {
		return sanitizeLine(e.Name) + "@"
	}
	return sanitizeLine(e.Name)
}

// sanitizeLine makes s safe to draw on one line: tabs become four spaces, and every other control rune, ESC included, becomes �.
func sanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, strings.ReplaceAll(s, "\t", "    "))
}

// parentDir is the directory above dir, "" at the root.
func parentDir(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[:i]
	}
	return ""
}

// note is a single faint line for a state the column shows instead of content.
func note(s string) []string { return []string{style.Faint.Render(s)} }

// at returns lines[i], or "" past the end.
func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
