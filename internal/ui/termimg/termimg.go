// Package termimg emits kitty graphics Unicode-placeholder images and detects whether the terminal shows them.
// It imports nothing from internal/.
package termimg

import (
	"bytes"
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Support is what the terminal can do with images; the zero value means it can't show them.
type Support struct {
	OK           bool // placeholders will draw: allowlisted terminal, graphics answered, ANSI256 or better
	Tmux         bool // running inside tmux; graphics commands must be wrapped
	CellW, CellH int  // cell size in pixels; 8x16 when the terminal doesn't report it
}

// MaxSpan is the largest column or row count Cells can address: kitty.Diacritic repeats its first entry past the table.
const MaxSpan = 297

// Image IDs are 256-colour indexes from 16 up: 0-15 are the ANSI palette, which themes redefine.
const (
	minID = 16
	maxID = 255
)

// Transmit returns the sequences that send img as image id and place it as cols x rows cells.
// It returns "" for an out-of-range id or span, a nil or empty image, or an image that fails to encode.
func Transmit(id int, img image.Image, cols, rows int, tmux bool) string {
	if !validID(id) || !validSpan(cols, rows) || img == nil || img.Bounds().Empty() {
		return ""
	}
	var buf bytes.Buffer
	err := kitty.EncodeGraphics(&buf, img, &kitty.Options{
		Action:           kitty.TransmitAndPut,
		Transmission:     kitty.Direct,
		Format:           kitty.PNG,
		Quiet:            2,
		ID:               id,
		PlacementID:      1,
		VirtualPlacement: true,
		Columns:          cols,
		Rows:             rows,
		Chunk:            true,
		ChunkFormatter:   formatter(tmux),
	})
	if err != nil {
		return ""
	}
	return buf.String()
}

// Place returns the sequence that resizes the already transmitted image id to cols x rows, sending no data.
func Place(id, cols, rows int, tmux bool) string {
	if !validID(id) || !validSpan(cols, rows) {
		return ""
	}
	return command(kitty.Options{
		Action:           kitty.Put,
		Quiet:            2,
		ID:               id,
		PlacementID:      1,
		VirtualPlacement: true,
		Columns:          cols,
		Rows:             rows,
	}, tmux)
}

// Delete returns the sequence that frees image id and its data.
func Delete(id int, tmux bool) string {
	if !validID(id) {
		return ""
	}
	return command(kitty.Options{
		Action:          kitty.Delete,
		Quiet:           2,
		ID:              id,
		Delete:          kitty.DeleteID,
		DeleteResources: true,
	}, tmux)
}

// Cells returns rows placeholder rows for image id, each cols cells wide; nil when id or the span is out of range.
func Cells(id, cols, rows int) []string {
	if !validID(id) || !validSpan(cols, rows) {
		return nil
	}
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// command is one graphics sequence without payload, wrapped for tmux when asked.
func command(o kitty.Options, tmux bool) string {
	seq := ansi.KittyGraphics(nil, o.Options()...)
	if tmux {
		return ansi.TmuxPassthrough(seq)
	}
	return seq
}

func formatter(tmux bool) func(string) string {
	if tmux {
		return ansi.TmuxPassthrough
	}
	return nil
}

func validID(id int) bool {
	return id >= minID && id <= maxID
}

func validSpan(cols, rows int) bool {
	return cols >= 1 && cols <= MaxSpan && rows >= 1 && rows <= MaxSpan
}
