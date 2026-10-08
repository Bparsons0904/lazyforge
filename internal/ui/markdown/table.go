package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"
	east "github.com/yuin/goldmark/extension/ast"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const colGap = " │ "

// table falls back to stacked Header: value lines when the columns don't fit in w.
func (r renderer) table(t *east.Table, w int) []string {
	var header [][]atom
	var headerText []string
	var rows [][][]atom
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, isHeader := row.(*east.TableHeader)
		base := style.Text
		if isHeader {
			base = style.Heading
		}
		var cells [][]atom
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, r.inline(c, base))
			if isHeader {
				headerText = append(headerText, plain(r.src, c, true))
			}
		}
		if isHeader {
			header = cells
		} else {
			rows = append(rows, cells)
		}
	}
	cols := len(header)
	if cols == 0 {
		return nil
	}
	widths := make([]int, cols)
	for i, h := range header {
		widths[i] = lipgloss.Width(cellLine(h))
	}
	for _, row := range rows {
		for i := 0; i < cols && i < len(row); i++ {
			widths[i] = max(widths[i], lipgloss.Width(cellLine(row[i])))
		}
	}
	total := lipgloss.Width(colGap) * (cols - 1)
	for _, cw := range widths {
		total += cw
	}
	if total > w {
		return stacked(headerText, rows, w)
	}
	line := func(cells [][]atom) string {
		parts := make([]string, cols)
		for i := range cols {
			cell := ""
			if i < len(cells) {
				cell = cellLine(cells[i])
			}
			parts[i] = cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell))
		}
		return strings.Join(parts, style.Faint.Render(colGap))
	}
	out := []string{line(header)}
	rule := make([]string, cols)
	for i, cw := range widths {
		rule[i] = strings.Repeat("─", cw)
	}
	out = append(out, style.Faint.Render(strings.Join(rule, "─┼─")))
	for _, row := range rows {
		out = append(out, line(row))
	}
	return out
}

func cellLine(atoms []atom) string {
	var b strings.Builder
	for i, a := range atoms {
		if a.nl {
			b.WriteByte(' ')
			continue
		}
		if a.space && i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(render(a.text, a.st, a.url))
	}
	return b.String()
}

func stacked(header []string, rows [][][]atom, w int) []string {
	if len(rows) == 0 {
		return wrap(words(strings.Join(header, " · "), style.Heading), w)
	}
	var out []string
	for ri, row := range rows {
		if ri > 0 {
			out = append(out, "")
		}
		for i, cell := range row {
			if i >= len(header) {
				break
			}
			atoms := []atom{{text: header[i] + ":", st: style.Faint}}
			for j, a := range cell {
				if j == 0 {
					a.space = true
				}
				atoms = append(atoms, a)
			}
			out = append(out, wrap(atoms, w)...)
		}
	}
	return out
}
