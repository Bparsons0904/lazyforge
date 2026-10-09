package ui

import (
	"context"
	"errors"
	"image"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

// imagesMsg turns the session's images on or off; App sends it on detection, config change and session start.
type imagesMsg struct {
	support termimg.Support
	show    bool
}

type imageState int

const (
	imageLoading imageState = iota
	imageReady
	imageFailed // shows the link until r
)

type imageEntry struct {
	state      imageState
	id         int  // termimg image ID; 0 when none is placed
	px, py     int  // decoded size in pixels
	cols, rows int  // span the last imagePlacedMsg confirmed
	placing    bool // a Transmit or Place sequence is in flight
}

// imageSet is the session's image state while images are on; a nil set means off.
type imageSet struct {
	support      termimg.Support
	ids          *termimg.IDs
	entries      map[string]*imageEntry
	paneW, paneH int // content width and visible height the spans were computed for
}

// imageKey is repo-scoped because a raw path such as /attachments/x.png resolves against that repo's own page.
func imageKey(repo domain.RepoRef, raw string) string { return repo.String() + " " + raw }

type imageLoadedMsg struct {
	set *imageSet
	key string
	img image.Image
	err error
}

// imagePlacedMsg rides in the same sequence as the bytes it confirms, so it can't overtake them.
type imagePlacedMsg struct {
	set        *imageSet
	key        string
	id         int
	cols, rows int
}

func loadImage(ctx context.Context, svc *core.Service, set *imageSet, repo domain.RepoRef, raw string) tea.Cmd {
	return func() tea.Msg {
		img, err := svc.Image(ctx, repo, raw)
		return imageLoadedMsg{set: set, key: imageKey(repo, raw), img: img, err: err}
	}
}

// imageCells returns the cell span that shows a px x py image within maxCols x maxRows without upscaling,
// keeping its aspect ratio; ok is false when an input or the cell size is below 1.
func imageCells(px, py int, s termimg.Support, maxCols, maxRows int) (cols, rows int, ok bool) {
	cw, ch := s.CellW, s.CellH
	if px < 1 || py < 1 || maxCols < 1 || maxRows < 1 || cw < 1 || ch < 1 {
		return 0, 0, false
	}
	lc := min(maxCols, termimg.MaxSpan)
	lr := min(maxRows, termimg.MaxSpan)
	switch {
	case px <= lc*cw && py <= lr*ch:
		cols, rows = ceilDiv(px, cw), ceilDiv(py, ch)
	case lc*cw*py <= lr*ch*px:
		cols, rows = lc, ceilDiv(py*lc*cw, px*ch)
	default:
		cols, rows = ceilDiv(px*lr*ch, py*cw), lr
	}
	return min(max(cols, 1), lc), min(max(rows, 1), lr), true
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// placeSeq sends seq and then reports the placement. Both sit in one sequence, so the message can't overtake the bytes.
func placeSeq(set *imageSet, key string, id, cols, rows int, seq string) tea.Cmd {
	return tea.Sequence(rawCmd(seq), func() tea.Msg {
		return imagePlacedMsg{set: set, key: key, id: id, cols: cols, rows: rows}
	})
}

// rawCmd is tea.Raw(s), or nil for an empty s, which tea.Sequence drops.
func rawCmd(s string) tea.Cmd {
	if s == "" {
		return nil
	}
	return tea.Raw(s)
}

// release returns the Delete sequences for every distinct held ID, ascending; "" on a nil set.
func (s *imageSet) release() string {
	if s == nil {
		return ""
	}
	var ids []int
	for _, e := range s.entries {
		if e.id != 0 && !slices.Contains(ids, e.id) {
			ids = append(ids, e.id)
		}
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(termimg.Delete(id, s.support.Tmux))
	}
	return b.String()
}

// blocks returns the cells of each of raws that is ready under repo; nil on a nil set.
func (s *imageSet) blocks(repo domain.RepoRef, raws []string) map[string][]string {
	if s == nil {
		return nil
	}
	out := map[string][]string{}
	for _, raw := range raws {
		if e, ok := s.entries[imageKey(repo, raw)]; ok && e.state == imageReady {
			out[raw] = termimg.Cells(e.id, e.cols, e.rows)
		}
	}
	return out
}

// forgetFailed drops failed entries so they load again; a no-op on a nil set.
func (s *imageSet) forgetFailed() {
	if s == nil {
		return
	}
	for k, e := range s.entries {
		if e.state == imageFailed {
			delete(s.entries, k)
		}
	}
}

// syncImages re-fits the ready images to the pane and starts loads for the block images on screen.
func (m *Model) syncImages() tea.Cmd {
	img := m.details.img
	if img == nil {
		return nil
	}
	bodyH, _, rightW := m.layout()
	var cmds []tea.Cmd
	if pw, ph := contentWidth(rightW), max(bodyH-2, 0); pw != img.paneW || ph != img.paneH {
		img.paneW, img.paneH = pw, ph
		for key, e := range img.entries {
			if e.state == imageReady && !e.placing {
				cmds = append(cmds, m.fitImage(key, e))
			}
		}
	}
	// The repo list's right column shows the boxes preview, not these details, so loading there wastes ID slots.
	if m.level == levelRepos {
		return tea.Batch(cmds...)
	}
	for _, w := range m.details.want {
		key := imageKey(w.repo, w.raw)
		if _, ok := img.entries[key]; ok {
			continue
		}
		img.entries[key] = &imageEntry{state: imageLoading}
		cmds = append(cmds, loadImage(m.ctx, m.svc, img, w.repo, w.raw))
	}
	return tea.Batch(cmds...)
}

// fitImage re-fits the ready entry e to the pane and returns the terminal output the change needs, if any.
func (m *Model) fitImage(key string, e *imageEntry) tea.Cmd {
	img := m.details.img
	cols, rows, ok := imageCells(e.px, e.py, img.support, img.paneW, img.paneH)
	switch {
	case !ok:
		del := termimg.Delete(e.id, img.support.Tmux)
		e.state, e.id = imageFailed, 0
		m.details.imgGen++
		return rawCmd(del)
	case cols == e.cols && rows == e.rows:
		return nil
	}
	e.placing = true
	return placeSeq(img, key, e.id, cols, rows, termimg.Place(e.id, cols, rows, img.support.Tmux))
}

func (m *Model) imagesChanged(msg imagesMsg) tea.Cmd {
	enabled := msg.support.OK && msg.show
	cur := m.details.img
	if enabled == (cur != nil) && (cur == nil || cur.support == msg.support) {
		return nil
	}
	del := cur.release()
	m.details.img = nil
	if enabled {
		m.details.img = &imageSet{support: msg.support, ids: termimg.NewIDs(msg.support.Tmux), entries: map[string]*imageEntry{}}
	}
	m.details.imgGen++
	return rawCmd(del)
}

func (m *Model) imageLoaded(msg imageLoadedMsg) tea.Cmd {
	img := m.details.img
	if img == nil || msg.set != img {
		return nil
	}
	e, ok := img.entries[msg.key]
	if !ok || e.state != imageLoading || e.id != 0 {
		return nil
	}
	switch {
	case errors.Is(msg.err, context.Canceled) && m.ctx.Err() == nil:
		delete(img.entries, msg.key)
		return nil
	case msg.err != nil:
		e.state = imageFailed
		return nil
	}
	px, py := msg.img.Bounds().Dx(), msg.img.Bounds().Dy()
	cols, rows, ok := imageCells(px, py, img.support, img.paneW, img.paneH)
	if !ok {
		e.state = imageFailed
		return nil
	}
	id, _, del := img.ids.Acquire(msg.key)
	if del != "" {
		// Acquire reused id from an evicted key; that key's entry no longer owns the image.
		for k, o := range img.entries {
			if o.id == id {
				delete(img.entries, k)
				m.details.imgGen++
			}
		}
	}
	e.id, e.px, e.py, e.placing = id, px, py, true
	return placeSeq(img, msg.key, id, cols, rows, del+termimg.Transmit(id, msg.img, cols, rows, img.support.Tmux))
}

func (m *Model) imagePlaced(msg imagePlacedMsg) tea.Cmd {
	img := m.details.img
	if img == nil || msg.set != img {
		return nil
	}
	e, ok := img.entries[msg.key]
	if !ok || e.id != msg.id {
		return nil
	}
	e.state, e.cols, e.rows, e.placing = imageReady, msg.cols, msg.rows, false
	m.details.imgGen++
	return m.fitImage(msg.key, e)
}
