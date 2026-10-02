// Package style holds the UI's palette and Lip Gloss styles; views take every color from here.
package style

import (
	"image/color"

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
	PaneTitle    = lipgloss.NewStyle().Foreground(Dim)
	ActiveTitle  = lipgloss.NewStyle().Foreground(Blue).Bold(true)
	BoxNumber    = lipgloss.NewStyle().Foreground(Peach)
	Count        = lipgloss.NewStyle().Foreground(Dim)
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

	ModeRepos   = mode(Blue)
	ModeBoxes   = mode(Mauve)
	ModeDetails = mode(Peach)

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
)

func mode(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Bg).Background(c).Bold(true).Padding(0, 1)
}
