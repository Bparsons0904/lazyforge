package ui

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const (
	splashFor            = 5 * time.Second
	splashFrame          = 120 * time.Millisecond
	workshopW, workshopH = 48, 14
)

var taglines = []string{
	"Hammering out your reviews.",
	"Forged in the terminal.",
	"Strike while the CI is green.",
	"Renovate updates, beaten into shape.",
	"Lazy by name, quick by keystroke.",
	"No mouse was harmed.",
	"Hot off the anvil.",
}

type (
	splashFrameMsg struct{}
	splashDoneMsg  struct{}
)

type splash struct {
	frame   int
	tagline string
}

func newSplash() splash { return splash{tagline: taglines[rand.IntN(len(taglines))]} }

func (splash) start() tea.Cmd {
	return tea.Batch(nextSplashFrame(), tea.Tick(splashFor, func(time.Time) tea.Msg { return splashDoneMsg{} }))
}

func nextSplashFrame() tea.Cmd {
	return tea.Tick(splashFrame, func(time.Time) tea.Msg { return splashFrameMsg{} })
}

type styledRune struct {
	r rune
	s lipgloss.Style
}

// workshop keeps the tools and anvil still while fire and a sparse plume animate.
func (s splash) workshop() [workshopH][workshopW]styledRune {
	var g [workshopH][workshopW]styledRune
	put := func(x, y int, lines []string, color lipgloss.Style) {
		for dy, line := range lines {
			for dx, r := range []rune(line) {
				if r != ' ' && y+dy >= 0 && y+dy < workshopH && x+dx >= 0 && x+dx < workshopW {
					g[y+dy][x+dx] = styledRune{r, color}
				}
			}
		}
	}
	put(2, 3, []string{
		"  ___________",
		" |___|___|___|",
		` |_/       \_|`,
		" | |       | |",
		" |_|       |_|",
		" | |_______| |",
		" |___|___|___|",
		"    |     |",
		"  __|_____|__",
	}, style.Faint)
	flames := [][]string{
		{" ) ( ", "( ^ )", `/^^^\`},
		{" ( ) ", " )^( ", "/^^^^"},
		{"  (  ", "( )^ ", `/^^^\`},
	}
	put(7, 6, flames[s.frame%len(flames)], style.SparkCool)
	put(7, 8, []string{"*****"}, style.SparkHot)
	put(24, 9, []string{
		"  ______________",
		`<=\____________/`,
		"      |    |",
		"    __|____|__",
		"   |__________|",
	}, style.Faint)
	put(29, 9, []string{"━━━━━"}, style.SparkHot)
	put(26, 1, []string{"──────────────────"}, style.Faint)
	put(29, 2, []string{"┬"}, style.Faint)
	put(38, 2, []string{"┬"}, style.Faint)
	put(27, 3, []string{"┌────┐", "└─┬──┘"}, style.Text)
	put(29, 5, []string{"│", "│", "╵"}, style.Virtual)
	put(37, 3, []string{`╲ ╱`, " ╳ ", `╱ ╲`, "│ │"}, style.Text)
	for i := range 4 {
		drift := math.Mod(float64(s.frame)*splashFrame.Seconds()*0.38+float64(i)*0.25, 1)
		x := 9 + int(math.Round(math.Sin(float64(i)*2.4)*drift*5))
		y := int(math.Round(2 - drift*3))
		glyph := "."
		if drift > 0.65 {
			glyph = "·"
		}
		if y >= 0 {
			put(x, y, []string{glyph}, style.Faint)
		}
	}
	return g
}

func (s splash) view(w, h int) string {
	block := []string{style.Brand.Render("lazyforge"), "", style.Faint.Render(s.tagline), "", style.HintText.Render("press any key")}
	if art := s.art(); w >= lipgloss.Width(art[0])+2 && h >= len(art)+len(block)+2 {
		block = append(append(art, ""), block...)
	}
	return fitLines(strings.Split(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, block...)), "\n"), w, h)
}

func (s splash) art() []string {
	var out []string
	for _, row := range s.workshop() {
		var b strings.Builder
		for _, c := range row {
			if c.r == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(c.s.Render(string(c.r)))
		}
		out = append(out, b.String())
	}
	// Equal widths keep the art in one piece when the block is centred line by line.
	w := 0
	for _, l := range out {
		w = max(w, lipgloss.Width(l))
	}
	for i, l := range out {
		out[i] = fitLine(l, w)
	}
	return out
}
