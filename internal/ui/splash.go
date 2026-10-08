package ui

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

const (
	splashFor   = 5 * time.Second
	splashFrame = 120 * time.Millisecond
	strikeEvery = 8
	skyW, skyH  = 30, 6
	strikeX     = 14 // under the centre of the hammer head
	sparkCount  = 12
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

var anvil = []string{
	"    ______________________",
	"    \\                    /====",
	"     \\__________________/",
	"           |      |",
	"         __|______|__",
	"        |____________|",
}

var hammer = []string{
	"           _______",
	"          |_______|=======",
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

// sky is the space above the anvil; the hammer lands on the last two frames of each cycle.
func (s splash) sky() [skyH][skyW]styledRune {
	var g [skyH][skyW]styledRune
	phase := s.frame % strikeEvery
	top := 0 // raised
	switch {
	case phase >= strikeEvery-2:
		top = skyH - len(hammer) // struck
	case phase == strikeEvery-3:
		top = (skyH - len(hammer)) / 2 // swinging
	}
	for i, l := range hammer {
		for x, r := range l {
			if r != ' ' && x < skyW {
				g[top+i][x] = styledRune{r, style.Text}
			}
		}
	}

	// Sparks fly from the last strike; the cycle number seeds them so each strike bursts differently.
	age := (s.frame - (strikeEvery - 2) + strikeEvery) % strikeEvery
	cycle := (s.frame - (strikeEvery - 2)) / strikeEvery
	if s.frame < strikeEvery-2 || age > 4 {
		return g
	}
	rng := rand.New(rand.NewPCG(uint64(cycle), 0x5eed))
	t := float64(age + 1)
	for range sparkCount {
		vx, vy := rng.Float64()*6-3, -(rng.Float64()*1.5 + 0.8)
		x, y := strikeX+int(math.Round(vx*t)), skyH-1+int(math.Round(vy*t+0.3*t*t))
		if x < 0 || x >= skyW || y < 0 || y >= skyH || g[y][x].r != 0 {
			continue
		}
		g[y][x] = sparkGlyph(age)
	}
	return g
}

type styledRune struct {
	r rune
	s lipgloss.Style
}

func sparkGlyph(age int) styledRune {
	switch age {
	case 0, 1:
		return styledRune{'*', style.SparkHot}
	case 2:
		return styledRune{'+', style.SparkHot}
	case 3:
		return styledRune{'·', style.SparkCool}
	default:
		return styledRune{'.', style.SparkCool}
	}
}

func (s splash) view(w, h int) string {
	block := []string{style.Brand.Render("lazyforge"), "", style.Faint.Render(s.tagline), "", style.HintText.Render("press any key")}
	if art := s.art(); w >= lipgloss.Width(art[0])+2 && h >= len(art)+len(block)+2 {
		block = slices.Concat(art, []string{""}, block)
	}
	return fitLines(strings.Split(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, block...)), "\n"), w, h)
}

func (s splash) art() []string {
	var out []string
	for _, row := range s.sky() {
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
	for _, l := range anvil {
		out = append(out, style.Faint.Render(l))
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
