package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

// frame draws a w×h rounded box with title set into the top edge, which Lip Gloss borders can't do.
// Each content line gets one column of padding per side and is clipped to fit.
func frame(title string, lines []string, w, h int, active bool) string {
	bs := style.PaneBorder
	if active {
		bs = style.ActiveBorder
	}
	return frameWith(bs, title, lines, w, h)
}

func frameWith(bs lipgloss.Style, title string, lines []string, w, h int) string {
	if w < 2 || h < 2 {
		return fitLines(nil, w, h)
	}
	top := bs.Render("╭─")
	if title != "" && w >= 6 {
		t := clip(title, w-5)
		top += " " + t + " " + bs.Render(strings.Repeat("─", w-4-lipgloss.Width(t)-1)) + bs.Render("╮")
	} else {
		top += bs.Render(strings.Repeat("─", w-3) + "╮")
	}
	out := make([]string, 0, h)
	out = append(out, top)
	side := bs.Render("│")
	for i := range h - 2 {
		var l string
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, side+fitLine(" "+l, w-2)+side)
	}
	out = append(out, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

// fitLines returns lines as exactly h lines of exactly w columns, clipping or padding as needed.
func fitLines(lines []string, w, h int) string {
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fitLine(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", max(w, 0))
		}
	}
	return strings.Join(out, "\n")
}

func fitLine(s string, w int) string {
	s = clip(s, w)
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// clip cuts styled text to w columns without an ellipsis; plain text should go through truncate instead.
func clip(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// truncate shortens plain text to w columns, ending in … when it had to cut.
func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if n+rw > w-1 {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String() + "…"
}

// row lays out label on the left and meta on the right of a w-column line; only label is truncated.
func row(label, meta string, w int, base lipgloss.Style) string {
	return tagRow("", lipgloss.NewStyle(), label, style.Text, meta, w, base)
}

func tagRow(tag string, ts lipgloss.Style, label string, ls lipgloss.Style, meta string, w int, base lipgloss.Style) string {
	// lipgloss.Width counts a tab as 0 but Render expands it, so an unreplaced control char overflows w.
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, s)
	}
	tag = clean(tag)
	// The tag shares the label's truncation budget so a long one can't push meta off the line.
	full := truncate(tag+clean(label), w-lipgloss.Width(meta)-1)
	gap := max(w-lipgloss.Width(full)-lipgloss.Width(meta), 0)
	n := min(len([]rune(tag)), len([]rune(full)))
	head, rest := []rune(full)[:n], []rune(full)[n:]
	return ts.Inherit(base).Render(string(head)) + ls.Inherit(base).Render(string(rest)+strings.Repeat(" ", gap)) + meta
}

func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func ciIcon(s domain.CIState, base lipgloss.Style) string {
	switch s {
	case domain.CIPass:
		return style.CIPass.Inherit(base).Render("✓")
	case domain.CIFail:
		return style.CIFail.Inherit(base).Render("✗")
	case domain.CIRunning, domain.CIPending:
		return style.CIRunning.Inherit(base).Render("●")
	default:
		return style.CIOther.Inherit(base).Render("○")
	}
}

func ciText(s domain.CIState) string {
	switch s {
	case domain.CIPass:
		return "CI passing"
	case domain.CIFail:
		return "CI failing"
	case domain.CIRunning:
		return "CI running"
	case domain.CIPending:
		return "CI pending"
	case domain.CICancelled:
		return "CI cancelled"
	case domain.CISkipped:
		return "CI skipped"
	default:
		return "no CI"
	}
}
