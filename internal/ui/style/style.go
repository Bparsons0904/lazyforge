// Package style holds the UI's palette and Lip Gloss styles; views take every color from here.
package style

import (
	"encoding/hex"
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// The mockup's terminal palette.
var (
	Bg     = lipgloss.Color("#1e1e2e")
	Fg     = lipgloss.Color("#cdd6f4")
	Dim    = lipgloss.Color("#7f849c")
	Sel    = lipgloss.Color("#313244")
	Border = lipgloss.Color("#45475a")
	Blue   = lipgloss.Color("#89b4fa")
	Green  = lipgloss.Color("#a6e3a1")
	Red    = lipgloss.Color("#f38ba8")
	Yellow = lipgloss.Color("#f9e2af")
	Mauve  = lipgloss.Color("#cba6f7")
	Peach  = lipgloss.Color("#fab387")
)

// Styles used by the views.
var (
	PaneBorder   = lipgloss.NewStyle().Foreground(Border)
	ActiveBorder = lipgloss.NewStyle().Foreground(Blue)
	ActiveTitle  = lipgloss.NewStyle().Foreground(Blue).Bold(true)
	Count        = lipgloss.NewStyle().Foreground(Dim)
	RepoStale    = lipgloss.NewStyle().Foreground(Dim)
	RepoOpenPRs  = lipgloss.NewStyle().Foreground(Mauve).Bold(true)
	Selected     = lipgloss.NewStyle().Background(Sel)
	Text         = lipgloss.NewStyle().Foreground(Fg)
	Faint        = lipgloss.NewStyle().Foreground(Dim)
	Virtual      = lipgloss.NewStyle().Foreground(Peach)
	Tab          = lipgloss.NewStyle().Foreground(Dim)
	CurrentTab   = lipgloss.NewStyle().Foreground(Fg).Underline(true)

	Brand        = lipgloss.NewStyle().Foreground(Mauve).Bold(true)
	Crumb        = lipgloss.NewStyle().Foreground(Dim)
	CurrentCrumb = lipgloss.NewStyle().Foreground(Fg).Bold(true)
	CrumbSep     = lipgloss.NewStyle().Foreground(Border)

	ModeRepos    = mode(Blue)
	ModeBoxes    = mode(Mauve)
	ModeDetails  = mode(Peach)
	ModeHosts    = mode(Green)
	ModeSettings = mode(Yellow)
	ModeSetup    = mode(Dim)

	HintKey    = lipgloss.NewStyle().Foreground(Fg).Bold(true)
	HintText   = lipgloss.NewStyle().Foreground(Dim)
	HelpKey    = lipgloss.NewStyle().Foreground(Peach)
	StatusErr  = lipgloss.NewStyle().Foreground(Red)
	StatusInfo = lipgloss.NewStyle().Foreground(Yellow)

	CIPass    = lipgloss.NewStyle().Foreground(Green)
	CIFail    = lipgloss.NewStyle().Foreground(Red)
	CIRunning = lipgloss.NewStyle().Foreground(Yellow)
	CIOther   = lipgloss.NewStyle().Foreground(Dim)

	Heading = lipgloss.NewStyle().Foreground(Fg).Bold(true)
	Link    = lipgloss.NewStyle().Foreground(Blue).Underline(true)
	Code    = lipgloss.NewStyle().Foreground(Peach)
	Mark    = lipgloss.NewStyle().Foreground(Mauve).Bold(true)

	SparkHot  = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	SparkCool = lipgloss.NewStyle().Foreground(Peach)
)

// Accents is one hue per box on a page; an index past the end wraps, so extra boxes repeat hues.
type Accents []color.Color

// Box accents: the repo page's pull requests, issues and actions, and the ★ Renovate page's five boxes.
var (
	RepoAccents = Accents{Mauve, Yellow, Green}
	StarAccents = Accents{Peach, Blue, Mauve, Green, Red}
)

// Title is bold when the box is focused.
func (a Accents) Title(i int, focused bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(a[i%len(a)]).Bold(focused)
}

// Border is the full accent when focused and a darker shade otherwise.
func (a Accents) Border(i int, focused bool) lipgloss.Style {
	c := a[i%len(a)]
	if !focused {
		c = lipgloss.Darken(c, 0.5)
	}
	return lipgloss.NewStyle().Foreground(c)
}

// Text is plain accent colour, with no bold or dimming.
func (a Accents) Text(i int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(a[i%len(a)])
}

// UpdateType is red for major, yellow for minor, green for patch, and dim for anything else.
func UpdateType(t string) lipgloss.Style {
	switch t {
	case "major":
		return lipgloss.NewStyle().Foreground(Red)
	case "minor":
		return lipgloss.NewStyle().Foreground(Yellow)
	case "patch":
		return lipgloss.NewStyle().Foreground(Green)
	default:
		return Faint
	}
}

func mode(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Bg).Background(c).Bold(true).Padding(0, 1)
}

// Label uses the forge color when valid, or a stable palette color based on the name.
func Label(name, rawColor string) lipgloss.Style {
	value := strings.TrimPrefix(rawColor, "#")
	if _, err := hex.DecodeString(value); len(value) == 6 && err == nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#" + value))
	}
	palette := []color.Color{Blue, Green, Red, Yellow, Mauve, Peach}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(palette[int(h.Sum32()%uint32(len(palette)))])
}
