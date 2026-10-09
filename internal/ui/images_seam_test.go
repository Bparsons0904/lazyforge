package ui

import (
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

// TestImageCellsWorkedTable checks the span rules against values worked out by hand, not against imageCells' own output.
func TestImageCellsWorkedTable(t *testing.T) {
	s := termimg.Support{OK: true, CellW: 8, CellH: 16}
	for _, tc := range []struct {
		px, py, maxC, maxR int
		cols, rows         int
		ok                 bool
	}{
		{80, 32, 50, 20, 10, 2, true},
		{800, 400, 50, 20, 50, 13, true},
		{100, 1600, 50, 20, 3, 20, true},
		{4000, 16, 50, 20, 50, 1, true},
		{80, 32, 50, 0, 0, 0, false},
	} {
		cols, rows, ok := imageCells(tc.px, tc.py, s, tc.maxC, tc.maxR)
		if cols != tc.cols || rows != tc.rows || ok != tc.ok {
			t.Errorf("imageCells(%dx%d, %dx%d) = %d,%d,%v; want %d,%d,%v", tc.px, tc.py, tc.maxC, tc.maxR, cols, rows, ok, tc.cols, tc.rows, tc.ok)
		}
	}
}
