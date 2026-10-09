package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi/kitty"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/markdown"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const (
	shotBody   = "intro\n\n![shot](/attachments/x.png)\n\noutro"
	shotAsset  = "/attachments/x.png"
	shotKey    = "o/r /attachments/x.png"
	shotLink   = "🖼 shot"
	shotPrefix = "o/r "
)

var (
	shotRepo    = domain.RepoRef{Owner: "o", Name: "r"}
	testSupport = termimg.Support{OK: true, CellW: 8, CellH: 16}
	enableShots = imagesMsg{support: testSupport, show: true}
)

// cachedImageModel returns a loaded model whose service holds one fetched image.
func cachedImageModel(t *testing.T) (Model, domain.RepoRef, string) {
	t.Helper()
	const raw = "/attachments/x.png"
	ref := domain.RepoRef{Owner: "o", Name: "r"}
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h", User: "bob", ChangeRequestTerm: "PR"})
	f.AddAsset("/attachments/x.png", forgetest.DemoPNG)
	m := boot(t, seededWith(t, f))
	if _, err := m.svc.Image(context.Background(), ref, raw); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.svc.PeekImage(ref, raw); !ok {
		t.Fatal("the image is not cached before the test acts")
	}
	return m, ref, raw
}

func TestRefreshKeyClearsImageCache(t *testing.T) {
	m, ref, raw := cachedImageModel(t)
	m = press(t, m, "r")
	if _, ok, _ := m.svc.PeekImage(ref, raw); ok {
		t.Error("the r key left the image cached")
	}
}

func TestRefreshTickKeepsImageCache(t *testing.T) {
	m, ref, raw := cachedImageModel(t)
	m = run(t, m, refreshTickMsg{})
	if _, ok, _ := m.svc.PeekImage(ref, raw); !ok {
		t.Error("the five-minute refresh tick cleared the image cache")
	}
}

// shotFake returns a Forgejo fake whose o/r repo has one open PR with body, and asset served at the image path when it isn't nil.
func shotFake(body string, asset []byte) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://h", User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: shotRepo})
	f.AddChangeRequest(shotRepo, domain.ChangeRequest{Number: 1, Title: "Add a shot", Author: "bob", Body: body})
	if asset != nil {
		f.AddAsset(shotAsset, asset)
	}
	return f
}

// shotModelSized returns a model over f sized w×h, with the PR's details open and images off.
func shotModelSized(t *testing.T, f forge.Forge, w, h int) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, core.Options{}))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	return press(t, sizedWith(t, m, w, h), "j", "l", "l")
}

func shotModel(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return shotModelSized(t, f, 120, 40)
}

// settle feeds msg and the messages its commands produce back into m, except image loads: it counts those and
// leaves them unfed, so a test decides when a fetch lands. It returns the model and the number of loads started.
func settle(t *testing.T, m Model, msg tea.Msg) (Model, int) {
	t.Helper()
	loads := 0
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > maxSettleSteps {
			t.Fatalf("messages did not settle after %d steps: a command keeps producing messages", maxSettleSteps)
		}
		next, cmd := m.Update(queue[0])
		m, queue = next.(Model), queue[1:]
		for _, out := range exec(t, cmd) {
			if _, ok := out.(imageLoadedMsg); ok {
				loads++
				continue
			}
			queue = append(queue, out)
		}
	}
	return m, loads
}

// turnOn enables images with the default support, as App does once detection finishes.
func turnOn(t *testing.T, m Model) (Model, int) {
	t.Helper()
	return settle(t, m, enableShots)
}

// loadsIn counts the image loads among msgs.
func loadsIn(msgs []tea.Msg) int {
	n := 0
	for _, msg := range msgs {
		if _, ok := msg.(imageLoadedMsg); ok {
			n++
		}
	}
	return n
}

// findLoaded returns the first image load among msgs, failing the test when there is none.
func findLoaded(t *testing.T, msgs []tea.Msg) imageLoadedMsg {
	t.Helper()
	for _, msg := range msgs {
		if l, ok := msg.(imageLoadedMsg); ok {
			return l
		}
	}
	t.Fatalf("no image load among %v", msgs)
	return imageLoadedMsg{}
}

// findPlaced returns the first placed message among msgs, failing the test when there is none.
func findPlaced(t *testing.T, msgs []tea.Msg) imagePlacedMsg {
	t.Helper()
	for _, msg := range msgs {
		if p, ok := msg.(imagePlacedMsg); ok {
			return p
		}
	}
	t.Fatalf("no placed message among %v", msgs)
	return imagePlacedMsg{}
}

// rawsOf returns the payload of every tea.RawMsg among msgs, in order.
func rawsOf(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		if raw, ok := msg.(tea.RawMsg); ok {
			s, _ := raw.Msg.(string)
			out = append(out, s)
		}
	}
	return out
}

// deliverLoad feeds the image load among msgs into m and returns the model and the messages that delivery produced.
func deliverLoad(t *testing.T, m Model, msgs []tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	next, cmd := m.Update(findLoaded(t, msgs))
	return next.(Model), exec(t, cmd)
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// pngOf encodes a blank w×h px image.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// entryAt returns the image entry for key in m, or nil when images are off or the key has none.
func entryAt(m Model, key string) *imageEntry {
	if m.details.img == nil {
		return nil
	}
	return m.details.img.entries[key]
}

// needShotEntry returns the image entry for the fixture image in m, failing the test when there is none.
func needShotEntry(t *testing.T, m Model) *imageEntry {
	t.Helper()
	e := entryAt(m, shotKey)
	if e == nil {
		t.Fatalf("no image entry for %q", shotKey)
	}
	return e
}

// pane returns the content width and visible height that m's images are fitted to, failing the test when images are off.
func pane(t *testing.T, m Model) (int, int) {
	t.Helper()
	if m.details.img == nil {
		t.Fatal("images are off, so there is no pane to fit")
	}
	return m.details.img.paneW, m.details.img.paneH
}

// spanIn returns the cell span m's pane gives img, failing the test when the pane can't hold it.
func spanIn(t *testing.T, m Model, img image.Image) (int, int) {
	t.Helper()
	b := img.Bounds()
	w, h := pane(t, m)
	cols, rows, ok := imageCells(b.Dx(), b.Dy(), testSupport, w, h)
	if !ok {
		t.Fatalf("a %dx%d px image has no span in a %dx%d pane", b.Dx(), b.Dy(), w, h)
	}
	return cols, rows
}

// transmitID returns the image ID in 16..255 that payload transmits img as at cols×rows, failing the test when none does.
func transmitID(t *testing.T, payload string, img image.Image, cols, rows int) int {
	t.Helper()
	for id := 16; id <= 255; id++ {
		if payload == termimg.Transmit(id, img, cols, rows, false) {
			return id
		}
	}
	t.Fatalf("payload transmits no image in 16..255 at %dx%d: %q", cols, rows, payload)
	return 0
}

// transmit feeds the successful load of img into m and returns the model, the image's ID and span, and the placed
// message its sequence ends with. The placed message is left unfed so a test decides when it lands.
func transmit(t *testing.T, m Model, img image.Image) (Model, int, int, int, imagePlacedMsg) {
	t.Helper()
	next, cmd := m.Update(imageLoadedMsg{set: m.details.img, key: shotKey, img: img})
	m = next.(Model)
	cols, rows := spanIn(t, m, img)
	out := exec(t, cmd)
	if len(out) != 2 {
		t.Fatalf("a load sent %d messages, want its Transmit and placed message: %v", len(out), out)
	}
	raw, ok := out[0].(tea.RawMsg)
	if !ok {
		t.Fatalf("first message is %T, want tea.RawMsg", out[0])
	}
	payload, _ := raw.Msg.(string)
	id := transmitID(t, payload, img, cols, rows)
	placed := findPlaced(t, out)
	if placed.id != id || placed.cols != cols || placed.rows != rows {
		t.Fatalf("placed message %+v, want id %d at %dx%d", placed, id, cols, rows)
	}
	return m, id, cols, rows, placed
}

// place is transmit followed by the placed message, which it feeds back into m.
func place(t *testing.T, m Model, img image.Image) (Model, int, int, int) {
	t.Helper()
	m, id, cols, rows, placed := transmit(t, m, img)
	next, _ := m.Update(placed)
	return next.(Model), id, cols, rows
}

// wantCells fails the test unless view holds the placeholder rows of image id at cols×rows.
func wantCells(t *testing.T, view string, id, cols, rows int) {
	t.Helper()
	for _, row := range termimg.Cells(id, cols, rows) {
		if !strings.Contains(view, row) {
			t.Fatalf("view lacks placeholder row %q", row)
		}
	}
}

// fitsWidth fails the test if any View line is wider than m's terminal.
func fitsWidth(t *testing.T, m Model) {
	t.Helper()
	for i, l := range strings.Split(m.View().Content, "\n") {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("view line %d is %d wide in a %d-wide terminal: %q", i, w, m.width, strip(l))
		}
	}
}

// placeholderLines counts the View lines that carry placeholder cells.
func placeholderLines(m Model) int {
	n := 0
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.ContainsRune(l, kitty.Placeholder) {
			n++
		}
	}
	return n
}

// firstPlacedRow returns the placeholder row that the first placeholder line of View carries, or -1 when there is none.
func firstPlacedRow(m Model, rows int) int {
	for _, l := range strings.Split(m.View().Content, "\n") {
		if !strings.ContainsRune(l, kitty.Placeholder) {
			continue
		}
		for r := range rows {
			if strings.Contains(l, string(kitty.Placeholder)+string(kitty.Diacritic(r))) {
				return r
			}
		}
		return -1
	}
	return -1
}

// silentLink fails the test unless m shows the plain link with an empty status bar and no placeholder cells.
func silentLink(t *testing.T, m Model) {
	t.Helper()
	if m.status != "" || m.statusErr {
		t.Errorf("status bar shows %q (error %v)", m.status, m.statusErr)
	}
	if placeholderLines(m) != 0 {
		t.Errorf("view holds placeholder cells:\n%s", strip(m.View().Content))
	}
	if view := osc8RE.ReplaceAllString(strip(m.View().Content), ""); !strings.Contains(view, shotLink) {
		t.Errorf("view lacks the link %q:\n%s", shotLink, view)
	}
}

// osc8RE matches the OSC 8 hyperlink markers the link renderer wraps each word in.
var osc8RE = regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)

func TestImageCellsFitsTheWorkedTable(t *testing.T) {
	tests := []struct {
		name               string
		px, py, maxC, maxR int
		cols, rows         int
		ok                 bool
	}{
		{"fits at natural size, not enlarged", 80, 32, 50, 20, 10, 2, true},
		{"width-bound", 800, 400, 50, 20, 50, 13, true},
		{"height-bound", 100, 1600, 50, 20, 3, 20, true},
		{"banner is one row", 4000, 16, 50, 20, 50, 1, true},
		{"no visible rows", 80, 32, 50, 0, 0, 0, false},
		{"no width", 80, 32, 0, 20, 0, 0, false},
		{"no pixels", 0, 32, 50, 20, 0, 0, false},
		{"span clamps to the terminal span limit", 4000, 16, 1000, 20, termimg.MaxSpan, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows, ok := imageCells(tt.px, tt.py, testSupport, tt.maxC, tt.maxR)
			if ok != tt.ok || (ok && (cols != tt.cols || rows != tt.rows)) {
				t.Fatalf("imageCells = %d×%d ok %v, want %d×%d ok %v", cols, rows, ok, tt.cols, tt.rows, tt.ok)
			}
		})
	}
	if _, _, ok := imageCells(80, 32, termimg.Support{OK: true, CellW: 0, CellH: 16}, 50, 20); ok {
		t.Error("a zero cell width gave a span")
	}
}

func TestBlockImageLoadsOncePerKey(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, loads := turnOn(t, m)
	m, resized := settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, scrolled := settle(t, m, keyMsg("j"))
	if total := loads + resized + scrolled; total != 1 {
		t.Fatalf("image loads = %d over enabling and two updates, want 1", total)
	}
	if m.details.img == nil || len(m.details.img.entries) != 1 {
		t.Fatalf("image entries = %v, want only the key %q", m.details.img, shotKey)
	}
	if e := needShotEntry(t, m); e.state != imageLoading {
		t.Fatalf("state = %v, want loading until the fetch lands", e.state)
	}
}

func TestBlockImageDrawsInDetails(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, cols, rows := place(t, m, decodePNG(t, forgetest.DemoPNG))
	wantCells(t, m.View().Content, id, cols, rows)
	fitsWidth(t, m)
	if strings.Contains(strip(m.View().Content), shotLink) {
		t.Fatalf("the link shows for a placed image:\n%s", strip(m.View().Content))
	}
}

func TestScrolledImageShowsItsLowerRows(t *testing.T) {
	png := pngOf(t, 16, 640)
	m := shotModel(t, shotFake(shotBody, png))
	m, _ = turnOn(t, m)
	m, id, cols, rows := place(t, m, decodePNG(t, png))
	for range 40 {
		if firstPlacedRow(m, rows) == 1 {
			break
		}
		m = press(t, m, "j")
	}
	if got := firstPlacedRow(m, rows); got != 1 {
		t.Fatalf("first visible placeholder row = %d, want 1", got)
	}
	view := m.View().Content
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, string(kitty.Placeholder)+string(kitty.Diacritic(1))) {
			if !strings.Contains(l, fmt.Sprintf("\x1b[38;5;%dm", id)) {
				t.Fatalf("row 1 lacks the image's foreground: %q", l)
			}
			break
		}
	}
	if strings.Contains(view, termimg.Cells(id, cols, rows)[0]) {
		t.Fatal("the scrolled-off row 0 is still in view")
	}
}

func TestNarrowTerminalKeepsTallImageInside(t *testing.T) {
	png := pngOf(t, 16, 640)
	m := shotModelSized(t, shotFake(shotBody, png), 80, 24)
	m, _ = turnOn(t, m)
	m, _, _, rows := place(t, m, decodePNG(t, png))
	fitsWidth(t, m)
	_, paneH := pane(t, m)
	if rows != paneH {
		t.Fatalf("a tall image spans %d rows, want the %d visible rows", rows, paneH)
	}
	if n := placeholderLines(m); n > paneH {
		t.Fatalf("%d placeholder lines in a %d-row pane", n, paneH)
	}
}

func TestResizeRefitsPlacedImage(t *testing.T) {
	png := pngOf(t, 16, 640)
	img := decodePNG(t, png)
	m := shotModel(t, shotFake(shotBody, png))
	m, _ = turnOn(t, m)
	m, id, _, rows := place(t, m, img)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(Model)
	cols2, rows2 := spanIn(t, m, img)
	if rows2 == rows {
		t.Fatalf("setup: the resize kept %d rows", rows)
	}
	out := exec(t, cmd)
	if got := rawsOf(out); !slices.Equal(got, []string{termimg.Place(id, cols2, rows2, false)}) {
		t.Fatalf("resize sent %q, want one Place at %d×%d", got, cols2, rows2)
	}
	placed := findPlaced(t, out)

	next, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); len(got) != 0 {
		t.Fatalf("a resize during a Place sent %q", got)
	}

	next, cmd = m.Update(placed)
	m = next.(Model)
	cols3, rows3 := spanIn(t, m, img)
	out = exec(t, cmd)
	if got := rawsOf(out); !slices.Equal(got, []string{termimg.Place(id, cols3, rows3, false)}) {
		t.Fatalf("placed message sent %q, want the pending Place at %d×%d", got, cols3, rows3)
	}
	m, _ = settle(t, m, findPlaced(t, out))
	// The image starts below the intro, so its last rows fall under the pane's bottom edge.
	want := termimg.Cells(id, cols3, rows3)
	got := 0
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.ContainsRune(l, kitty.Placeholder) {
			if got >= len(want) || !strings.Contains(l, want[got]) {
				t.Fatalf("placeholder line %d = %q, want row %d of the %d×%d image", got, l, got, cols3, rows3)
			}
			got++
		}
	}
	if got == 0 {
		t.Fatal("view holds no placeholder rows")
	}
}

func TestShrinkingPaneDeletesPlacedImage(t *testing.T) {
	img := decodePNG(t, forgetest.DemoPNG)
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, _, _ := place(t, m, img)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 4})
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); !slices.Equal(got, []string{termimg.Delete(id, false)}) {
		t.Fatalf("shrink sent %q, want the Delete of %d", got, id)
	}
	if e := entryAt(m, shotKey); e == nil || e.state != imageFailed {
		t.Fatalf("entry after the shrink = %+v, want failed", e)
	}
}

func TestPlacedMessageAfterShrinkDeletesImage(t *testing.T) {
	img := decodePNG(t, forgetest.DemoPNG)
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, _, _, placed := transmit(t, m, img)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 4})
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); len(got) != 0 {
		t.Fatalf("shrinking a placing image sent %q", got)
	}
	next, cmd = m.Update(placed)
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); !slices.Equal(got, []string{termimg.Delete(id, false)}) {
		t.Fatalf("late placed message sent %q, want the Delete of %d", got, id)
	}
	if e := entryAt(m, shotKey); e == nil || e.state != imageFailed {
		t.Fatalf("entry = %+v, want failed", e)
	}
}

// blindForge serves the test image for any URL and counts the fetches, so only core's same-host check keeps an
// off-host image out. The fake would refuse on its own.
type blindForge struct {
	*forgetest.Fake
	opened *int
}

func (f blindForge) OpenAsset(context.Context, *url.URL) (io.ReadCloser, error) {
	*f.opened++
	return io.NopCloser(bytes.NewReader(forgetest.DemoPNG)), nil
}

func TestImageFromAnotherHostStaysLink(t *testing.T) {
	const key = "o/r https://elsewhere.example/a.png"
	opened := 0
	f := blindForge{Fake: shotFake("intro\n\n![shot](https://elsewhere.example/a.png)\n\noutro", nil), opened: &opened}
	m := shotModel(t, f)
	next, cmd := m.Update(enableShots)
	m = next.(Model)
	loaded := findLoaded(t, exec(t, cmd))
	if loaded.key != key {
		t.Fatalf("load key = %q, want %q", loaded.key, key)
	}
	m, out := deliverLoad(t, m, []tea.Msg{loaded})
	if opened != 0 {
		t.Fatalf("the off-host image was fetched %d times", opened)
	}
	if got := rawsOf(out); len(got) != 0 {
		t.Fatalf("an off-host image sent %q", got)
	}
	if e := entryAt(m, key); e == nil || e.state != imageFailed {
		t.Fatalf("entry = %+v, want failed", e)
	}
	silentLink(t, m)
}

func TestImageShowsLinkSilently(t *testing.T) {
	t.Run("unsupported terminal", func(t *testing.T) {
		m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
		next, cmd := m.Update(imagesMsg{support: termimg.Support{}, show: true})
		m = next.(Model)
		out := exec(t, cmd)
		if n := loadsIn(out); n != 0 || len(rawsOf(out)) != 0 {
			t.Fatalf("unsupported terminal sent %d loads and %d raw sequences", n, len(rawsOf(out)))
		}
		silentLink(t, m)
	})
	t.Run("load in flight", func(t *testing.T) {
		m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
		m, loads := turnOn(t, m)
		if loads != 1 {
			t.Fatalf("enabling started %d loads, want 1", loads)
		}
		next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		m = next.(Model)
		if out := exec(t, cmd); loadsIn(out) != 0 || len(rawsOf(out)) != 0 {
			t.Fatalf("a sync during the fetch sent %v", out)
		}
		silentLink(t, m)
	})
	t.Run("failed load", func(t *testing.T) {
		m := shotModel(t, shotFake(shotBody, nil))
		next, cmd := m.Update(enableShots)
		m = next.(Model)
		m, out := deliverLoad(t, m, exec(t, cmd))
		if got := rawsOf(out); len(got) != 0 {
			t.Fatalf("a failed load sent %q", got)
		}
		silentLink(t, m)
	})
}

func TestImagesOutsideTopLevelParagraphsStayLinks(t *testing.T) {
	bodies := map[string]string{
		"inline text": "see ![shot](/attachments/x.png) here",
		"list item":   "- ![shot](/attachments/x.png)",
		"blockquote":  "> ![shot](/attachments/x.png)",
		"table cell":  "| h |\n|---|\n| ![shot](/attachments/x.png) |",
		"html img":    `<img src="/attachments/x.png">`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			m := shotModel(t, shotFake(body, forgetest.DemoPNG))
			m, loads := turnOn(t, m)
			m, more := settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
			if loads+more != 0 {
				t.Fatalf("image loads = %d, want 0", loads+more)
			}
			if md := m.details.md; md.out != markdown.Render(body, md.width) {
				t.Fatalf("details render differs from markdown.Render:\n got %q\nwant %q", md.out, markdown.Render(body, md.width))
			}
		})
	}
}

func TestLoadsWaitForTheDetailsPane(t *testing.T) {
	m := sizedWith(t, seededWith(t, shotFake(shotBody, forgetest.DemoPNG)), 120, 40)
	m, loads := turnOn(t, m)
	m, moved := settle(t, m, keyMsg("j"))
	m, resized := settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if total := loads + moved + resized; total != 0 {
		t.Fatalf("loads at the repo list = %d, want 0", total)
	}
	if _, entered := settle(t, m, keyMsg("l")); entered != 1 {
		t.Fatalf("entering the boxes started %d loads, want 1", entered)
	}
}

func TestLoadLandsAfterLeavingDetails(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	next, cmd := m.Update(enableShots)
	m = next.(Model)
	loaded := findLoaded(t, exec(t, cmd))
	m = press(t, m, "h", "h")
	m, out := deliverLoad(t, m, []tea.Msg{loaded})
	if got := rawsOf(out); len(got) != 1 {
		t.Fatalf("a load that lands at the repo list sent %d raw sequences, want its Transmit", len(got))
	}
	if e := entryAt(m, shotKey); e == nil || e.state == imageFailed {
		t.Fatalf("entry = %+v, want the landed image kept", e)
	}
	if m.status != "" {
		t.Fatalf("status bar shows %q", m.status)
	}
}

func TestSameImageTwiceDrawsTwiceLoadsOnce(t *testing.T) {
	small := pngOf(t, 8, 16)
	m := shotModel(t, shotFake("![shot](/attachments/x.png)\n\n![again](/attachments/x.png)", small))
	m, loads := turnOn(t, m)
	if loads != 1 {
		t.Fatalf("loads = %d, want 1", loads)
	}
	m, id, cols, rows := place(t, m, decodePNG(t, small))
	if n := strings.Count(m.View().Content, termimg.Cells(id, cols, rows)[0]); n != 2 {
		t.Fatalf("the image draws %d times, want 2", n)
	}
	if got, want := m.details.img.release(), termimg.Delete(id, false); got != want {
		t.Fatalf("release = %q, want one Delete of %d", got, id)
	}
}

func TestTurningImagesOffReleasesAndOnReloads(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, cols, rows := place(t, m, decodePNG(t, forgetest.DemoPNG))

	next, cmd := m.Update(imagesMsg{support: testSupport, show: false})
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); !slices.Equal(got, []string{termimg.Delete(id, false)}) {
		t.Fatalf("turning off sent %q, want the Delete of %d", got, id)
	}
	if strings.Contains(m.View().Content, termimg.Cells(id, cols, rows)[0]) {
		t.Fatal("placeholders still show with images off")
	}
	silentLink(t, m)

	next, cmd = m.Update(imagesMsg{support: testSupport, show: false})
	m = next.(Model)
	if out := exec(t, cmd); len(out) != 0 {
		t.Fatalf("a repeated off sent %v", out)
	}

	next, cmd = m.Update(enableShots)
	m = next.(Model)
	out := exec(t, cmd)
	if len(out) != 1 {
		t.Fatalf("turning on sent %d messages, want the one load", len(out))
	}
	findLoaded(t, out)

	_, cmd = m.Update(enableShots)
	if out := exec(t, cmd); len(out) != 0 {
		t.Fatalf("a repeated on sent %v", out)
	}
}

func TestWarmMemoLoadsWhenImagesTurnOn(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	next, cmd := m.Update(enableShots)
	out := exec(t, cmd)
	m = next.(Model)
	if len(out) != 1 {
		t.Fatalf("enabling sent %d messages, want the one load", len(out))
	}
	findLoaded(t, out)
	if e := entryAt(m, shotKey); e == nil || e.state != imageLoading {
		t.Fatalf("entry = %+v, want loading", e)
	}
}

func TestRefreshRetriesFailedImage(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, nil))
	next, cmd := m.Update(enableShots)
	m = next.(Model)
	m, _ = deliverLoad(t, m, exec(t, cmd))

	_, cmd = m.Update(keyMsg("r"))
	if n := loadsIn(exec(t, cmd)); n != 1 {
		t.Fatalf("r started %d loads for the failed image, want 1", n)
	}
}

func TestRefreshLeavesReadyImagePlaced(t *testing.T) {
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, cols, rows := place(t, m, decodePNG(t, forgetest.DemoPNG))

	next, cmd := m.Update(keyMsg("r"))
	m = next.(Model)
	out := exec(t, cmd)
	if loadsIn(out) != 0 || len(rawsOf(out)) != 0 {
		t.Fatalf("r re-sent a ready image: %d loads, raws %q", loadsIn(out), rawsOf(out))
	}
	if e := entryAt(m, shotKey); e == nil || e.state != imageReady {
		t.Fatalf("entry = %+v, want ready", e)
	}
	wantCells(t, m.View().Content, id, cols, rows)
}

func TestRetryAfterShrinkTransmitsUnderTheOldID(t *testing.T) {
	png := pngOf(t, 16, 640)
	img := decodePNG(t, png)
	m := shotModel(t, shotFake(shotBody, png))
	m, _ = turnOn(t, m)
	m, id, cols, rows := place(t, m, img)

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 4})
	m = next.(Model)
	exec(t, cmd)
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if n := loadsIn(exec(t, cmd)); n != 0 {
		t.Fatalf("restoring the pane reloaded a failed image %d times", n)
	}

	next, cmd = m.Update(keyMsg("r"))
	m = next.(Model)
	_, out := deliverLoad(t, m, exec(t, cmd))
	if got, want := rawsOf(out), []string{termimg.Transmit(id, img, cols, rows, false)}; !slices.Equal(got, want) {
		t.Fatalf("reload sent %q, want the Transmit under the old ID %d", got, id)
	}
}

func TestCancelledLoadIsRequestedAgain(t *testing.T) {
	t.Run("live session", func(t *testing.T) {
		m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
		m, _ = turnOn(t, m)
		next, cmd := m.Update(imageLoadedMsg{set: m.details.img, key: shotKey, err: context.Canceled})
		m = next.(Model)
		// The same Update's sync drops the cancelled entry and asks for the image again.
		if n := loadsIn(exec(t, cmd)); n != 1 {
			t.Fatalf("the sync after a cancelled load started %d loads, want 1", n)
		}
		if e := entryAt(m, shotKey); e == nil || e.state != imageLoading || e.id != 0 {
			t.Fatalf("entry = %+v, want a fresh loading entry", e)
		}
	})
	t.Run("ended session", func(t *testing.T) {
		m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
		m, _ = turnOn(t, m)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m.ctx = ctx
		next, _ := m.Update(imageLoadedMsg{set: m.details.img, key: shotKey, err: context.Canceled})
		m = next.(Model)
		if e := entryAt(m, shotKey); e == nil || e.state != imageFailed {
			t.Fatalf("entry = %+v, want failed when the session ended", e)
		}
		m, loads := settle(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
		if loads != 0 {
			t.Fatalf("the next sync started %d loads after the session ended, want 0", loads)
		}
	})
}

func TestPlacedAfterSwitchingOffIsDropped(t *testing.T) {
	img := decodePNG(t, forgetest.DemoPNG)
	m := shotModel(t, shotFake(shotBody, forgetest.DemoPNG))
	m, _ = turnOn(t, m)
	m, id, _, _, placed := transmit(t, m, img)

	next, cmd := m.Update(imagesMsg{support: testSupport, show: false})
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); !slices.Equal(got, []string{termimg.Delete(id, false)}) {
		t.Fatalf("turning off mid-Transmit sent %q, want the Delete of %d", got, id)
	}
	next, cmd = m.Update(placed)
	m = next.(Model)
	if got := rawsOf(exec(t, cmd)); len(got) != 0 {
		t.Fatalf("a placed message from the dropped set sent %q", got)
	}
	silentLink(t, m)
}

func TestImageSetReleaseBlocksAndForget(t *testing.T) {
	s := &imageSet{entries: map[string]*imageEntry{
		shotPrefix + "/a.png": {state: imageReady, id: 17, cols: 3, rows: 2},
		shotPrefix + "/b.png": {state: imageReady, id: 16, cols: 1, rows: 1},
		shotPrefix + "/c.png": {state: imageLoading, id: 17},
		shotPrefix + "/d.png": {state: imageFailed},
	}}
	if got, want := s.release(), termimg.Delete(16, false)+termimg.Delete(17, false); got != want {
		t.Fatalf("release = %q, want each held ID once, ascending", got)
	}
	if got := (*imageSet)(nil).release(); got != "" {
		t.Fatalf("nil set release = %q", got)
	}
	want := map[string][]string{
		"/a.png": termimg.Cells(17, 3, 2),
		"/b.png": termimg.Cells(16, 1, 1),
	}
	if got := s.blocks(shotRepo, []string{"/a.png", "/b.png", "/c.png", "/d.png"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("blocks = %q, want the ready images only", got)
	}
	if got := (*imageSet)(nil).blocks(shotRepo, []string{"/a.png"}); got != nil {
		t.Fatalf("nil set blocks = %q", got)
	}
	s.forgetFailed()
	if _, ok := s.entries[shotPrefix+"/d.png"]; ok {
		t.Fatal("forgetFailed kept the failed entry")
	}
	if _, ok := s.entries[shotPrefix+"/c.png"]; !ok {
		t.Fatal("forgetFailed dropped a loading entry")
	}
	(*imageSet)(nil).forgetFailed()
}
