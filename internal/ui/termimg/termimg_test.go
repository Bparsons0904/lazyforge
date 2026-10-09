package termimg_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const chunkSize = 4096

// wrapTmux is the tmux passthrough: every ESC doubled inside one DCS.
func wrapTmux(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// splitCommands cuts graphics output into commands, each keeping its string terminator.
func splitCommands(t *testing.T, out string) []string {
	t.Helper()
	var cmds []string
	for out != "" {
		end := strings.Index(out, "\x1b\\")
		if end < 0 {
			t.Fatalf("command without string terminator: %q", out)
		}
		cmds = append(cmds, out[:end+2])
		out = out[end+2:]
	}
	return cmds
}

// command is one graphics command split into its control keys and payload.
type command struct {
	keys       string
	payload    string
	hasPayload bool
}

func parseCommand(t *testing.T, cmd string) command {
	t.Helper()
	body, ok := strings.CutPrefix(cmd, "\x1b_G")
	if !ok || !strings.HasSuffix(body, "\x1b\\") {
		t.Fatalf("not a graphics command: %q", cmd)
	}
	body = strings.TrimSuffix(body, "\x1b\\")
	keys, payload, hasPayload := strings.Cut(body, ";")
	return command{keys: keys, payload: payload, hasPayload: hasPayload}
}

// decodeTransmit joins the payloads of a plain transmit and returns the PNG they carry.
func decodeTransmit(t *testing.T, out string) image.Image {
	t.Helper()
	var b64 strings.Builder
	for _, c := range splitCommands(t, out) {
		b64.WriteString(parseCommand(t, c).payload)
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload is not a PNG: %v", err)
	}
	return img
}

// assertTmuxTransmit checks that the wrapped output is each plain command wrapped in turn.
func assertTmuxTransmit(t *testing.T, plain, wrapped string) {
	t.Helper()
	var want strings.Builder
	cmds := splitCommands(t, plain)
	for _, c := range cmds {
		want.WriteString(wrapTmux(c))
	}
	if wrapped != want.String() {
		t.Fatalf("tmux transmit = %q, want %q", wrapped, want.String())
	}
	if n := strings.Count(wrapped, "\x1bPtmux;"); n != len(cmds) {
		t.Fatalf("tmux segments = %d, want %d", n, len(cmds))
	}
}

func assertSamePixels(t *testing.T, want, got image.Image) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("decoded bounds = %v, want %v", got.Bounds(), want.Bounds())
	}
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := got.At(x, y).RGBA()
			if wr != gr || wg != gg || wb != gb || wa != ga {
				t.Fatalf("pixel (%d,%d) differs after decoding", x, y)
			}
		}
	}
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// smallImage is 2x2 with distinct pixels, so a wrong decode shows up.
func smallImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(1, 0, color.NRGBA{0, 255, 0, 255})
	img.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	img.Set(1, 1, color.NRGBA{255, 255, 255, 255})
	return img
}

// noiseRGBA is an opaque seeded-noise image; its PNG does not compress, so it spans several chunks.
func noiseRGBA(seed int64, w, h int) *image.RGBA {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Intn(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	return img
}

func TestDeleteAndPlaceExactStrings(t *testing.T) {
	for _, tc := range []struct{ id, cols, rows int }{{16, 3, 2}, {255, 1, 4}} {
		if got, want := termimg.Delete(tc.id, false), fmt.Sprintf("\x1b_Gq=2,i=%d,d=I,a=d\x1b\\", tc.id); got != want {
			t.Errorf("Delete(%d) = %q, want %q", tc.id, got, want)
		}
		want := fmt.Sprintf("\x1b_Gq=2,i=%d,p=1,U=1,c=%d,r=%d,a=p\x1b\\", tc.id, tc.cols, tc.rows)
		if got := termimg.Place(tc.id, tc.cols, tc.rows, false); got != want {
			t.Errorf("Place(%d, %d, %d) = %q, want %q", tc.id, tc.cols, tc.rows, got, want)
		}
	}
}

func TestTransmitSingleChunkExactString(t *testing.T) {
	img := smallImage()
	b64 := base64.StdEncoding.EncodeToString(encodePNG(t, img))
	for _, id := range []int{16, 255} {
		want := fmt.Sprintf("\x1b_Gf=100,q=2,i=%d,p=1,U=1,c=2,r=2,a=T;%s\x1b\\", id, b64)
		got := termimg.Transmit(id, img, 2, 2, false)
		if got != want {
			t.Fatalf("Transmit(%d) = %q, want %q", id, got, want)
		}
		assertSamePixels(t, img, decodeTransmit(t, got))
	}
}

func TestTmuxWrapsGraphicsCommands(t *testing.T) {
	img := smallImage()
	for _, id := range []int{16, 255} {
		if got, want := termimg.Delete(id, true), wrapTmux(termimg.Delete(id, false)); got != want {
			t.Errorf("Delete(%d, tmux) = %q, want %q", id, got, want)
		}
		if got, want := termimg.Place(id, 3, 2, true), wrapTmux(termimg.Place(id, 3, 2, false)); got != want {
			t.Errorf("Place(%d, tmux) = %q, want %q", id, got, want)
		}
		assertTmuxTransmit(t, termimg.Transmit(id, img, 2, 2, false), termimg.Transmit(id, img, 2, 2, true))
	}
}

func TestTransmitSplitsLargePayloads(t *testing.T) {
	img := noiseRGBA(66, 64, 64)
	out := termimg.Transmit(16, img, 4, 3, false)

	b64Len := base64.StdEncoding.EncodedLen(len(encodePNG(t, img)))
	cmds := splitCommands(t, out)
	if want := (b64Len + chunkSize - 1) / chunkSize; len(cmds) != want || len(cmds) < 2 {
		t.Fatalf("got %d commands for %d base64 bytes, want %d chunks", len(cmds), b64Len, want)
	}
	for i, c := range cmds {
		cmd := parseCommand(t, c)
		last := i == len(cmds)-1
		switch {
		case i == 0:
			if want := "f=100,q=2,i=16,p=1,U=1,c=4,r=3,a=T,m=1"; cmd.keys != want {
				t.Errorf("first chunk keys = %q, want %q", cmd.keys, want)
			}
		case last:
			if cmd.keys != "q=2,m=0" {
				t.Errorf("last chunk keys = %q, want %q", cmd.keys, "q=2,m=0")
			}
		default:
			if cmd.keys != "q=2,m=1" {
				t.Errorf("middle chunk keys = %q, want %q", cmd.keys, "q=2,m=1")
			}
		}
		if !last && len(cmd.payload) != chunkSize {
			t.Errorf("chunk %d payload is %d bytes, want %d", i, len(cmd.payload), chunkSize)
		}
		if last && (len(cmd.payload) == 0 || len(cmd.payload) > chunkSize) {
			t.Errorf("last chunk payload is %d bytes, want 1..%d", len(cmd.payload), chunkSize)
		}
	}
	assertSamePixels(t, img, decodeTransmit(t, out))
	assertTmuxTransmit(t, out, termimg.Transmit(16, img, 4, 3, true))
}

// boundaryImage returns seeded noise whose base64 PNG is at least one chunk and an exact multiple of chunkSize.
func boundaryImage(t *testing.T) image.Image {
	t.Helper()
	rng := rand.New(rand.NewSource(66))
	const maxHeight = 20000
	noise := image.NewGray(image.Rect(0, 0, 1, maxHeight))
	for i := range noise.Pix {
		noise.Pix[i] = byte(rng.Intn(256))
	}
	b64Len := func(h int) (image.Image, int) {
		img := noise.SubImage(image.Rect(0, 0, 1, h))
		return img, base64.StdEncoding.EncodedLen(len(encodePNG(t, img)))
	}
	// Each encode allocates a full deflate state, so a linear scan from one row is slow under -race.
	// Noise barely compresses, so size grows with height: jump to the first prefix of a full chunk, then scan up.
	first := sort.Search(maxHeight, func(i int) bool {
		_, n := b64Len(i + 1)
		return n >= chunkSize
	}) + 1
	for h := first; h <= maxHeight && h < first+1000; h++ {
		if img, n := b64Len(h); n%chunkSize == 0 {
			return img
		}
	}
	t.Fatal("no seeded image has a base64 PNG that is an exact multiple of 4096 bytes")
	return nil
}

func TestTransmitExactMultipleOfChunkSize(t *testing.T) {
	img := boundaryImage(t)
	out := termimg.Transmit(16, img, 2, 2, false)

	cmds := splitCommands(t, out)
	if len(cmds) < 2 {
		t.Fatalf("got %d commands, want several chunks", len(cmds))
	}
	if last := cmds[len(cmds)-1]; last != "\x1b_Gq=2,m=0\x1b\\" {
		t.Errorf("final command = %q, want the empty terminator %q", last, "\x1b_Gq=2,m=0\x1b\\")
	}
	for _, c := range cmds[:len(cmds)-1] {
		if keys := parseCommand(t, c).keys; !strings.HasSuffix(keys, "m=1") {
			t.Errorf("command %q is not marked as continued", keys)
		}
	}
	assertSamePixels(t, img, decodeTransmit(t, out))
	assertTmuxTransmit(t, out, termimg.Transmit(16, img, 2, 2, true))
}

func TestCellsRowsSpellPlaceholdersWithDiacritics(t *testing.T) {
	for _, tc := range []struct{ id, cols, rows int }{
		{16, 1, 1},
		{16, 4, 3},
		{255, 4, 3},
		{200, termimg.MaxSpan, 2},
	} {
		got := termimg.Cells(tc.id, tc.cols, tc.rows)
		if len(got) != tc.rows {
			t.Fatalf("Cells(%d, %d, %d) has %d rows, want %d", tc.id, tc.cols, tc.rows, len(got), tc.rows)
		}
		prefix := "\x1b[38;5;" + strconv.Itoa(tc.id) + "m"
		for r, row := range got {
			body, ok := strings.CutPrefix(row, prefix)
			if !ok {
				t.Fatalf("row %d does not start with %q: %q", r, prefix, row)
			}
			body, ok = strings.CutSuffix(body, "\x1b[39m")
			if !ok {
				t.Fatalf("row %d does not end with the foreground reset: %q", r, row)
			}
			var want []rune
			for c := 0; c < tc.cols; c++ {
				want = append(want, kitty.Placeholder, kitty.Diacritic(r), kitty.Diacritic(c))
			}
			if body != string(want) {
				t.Errorf("row %d cells = %q, want %q", r, body, string(want))
			}
			if w := ansi.StringWidth(row); w != tc.cols {
				t.Errorf("row %d width = %d, want %d", r, w, tc.cols)
			}
		}
	}
}

func TestInvalidIDsReturnEmpty(t *testing.T) {
	img := smallImage()
	for _, id := range []int{15, 256} {
		if got := termimg.Transmit(id, img, 2, 2, false); got != "" {
			t.Errorf("Transmit(%d) = %q, want empty", id, got)
		}
		if got := termimg.Place(id, 2, 2, false); got != "" {
			t.Errorf("Place(%d) = %q, want empty", id, got)
		}
		if got := termimg.Delete(id, false); got != "" {
			t.Errorf("Delete(%d) = %q, want empty", id, got)
		}
		if got := termimg.Cells(id, 2, 2); got != nil {
			t.Errorf("Cells(%d) = %q, want nil", id, got)
		}
	}
}

func TestOutOfRangeSpansReturnEmpty(t *testing.T) {
	img := smallImage()
	for _, span := range []int{0, termimg.MaxSpan + 1} {
		if got := termimg.Transmit(16, img, span, 1, false); got != "" {
			t.Errorf("Transmit with cols %d = %q, want empty", span, got)
		}
		if got := termimg.Transmit(16, img, 1, span, false); got != "" {
			t.Errorf("Transmit with rows %d = %q, want empty", span, got)
		}
		if got := termimg.Place(16, span, 1, false); got != "" {
			t.Errorf("Place with cols %d = %q, want empty", span, got)
		}
		if got := termimg.Place(16, 1, span, false); got != "" {
			t.Errorf("Place with rows %d = %q, want empty", span, got)
		}
		if got := termimg.Cells(16, span, 1); got != nil {
			t.Errorf("Cells with cols %d = %q, want nil", span, got)
		}
		if got := termimg.Cells(16, 1, span); got != nil {
			t.Errorf("Cells with rows %d = %q, want nil", span, got)
		}
	}
}

func TestMaxSpanIsAccepted(t *testing.T) {
	img := smallImage()
	if got := termimg.Transmit(16, img, termimg.MaxSpan, 1, false); got == "" {
		t.Error("Transmit with cols MaxSpan = empty, want a command")
	}
	if got := termimg.Transmit(16, img, 1, termimg.MaxSpan, false); got == "" {
		t.Error("Transmit with rows MaxSpan = empty, want a command")
	}
	if got := termimg.Place(16, termimg.MaxSpan, 1, false); got == "" {
		t.Error("Place with cols MaxSpan = empty, want a command")
	}
	if got := termimg.Cells(16, termimg.MaxSpan, 1); len(got) != 1 {
		t.Errorf("Cells with cols MaxSpan has %d rows, want 1", len(got))
	}
}

func TestTransmitRejectsNilAndEmptyImages(t *testing.T) {
	if got := termimg.Transmit(16, nil, 2, 2, false); got != "" {
		t.Errorf("Transmit of nil image = %q, want empty", got)
	}
	empty := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if got := termimg.Transmit(16, empty, 2, 2, false); got != "" {
		t.Errorf("Transmit of empty-bounds image = %q, want empty", got)
	}
}
