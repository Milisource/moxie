package tui

import "github.com/charmbracelet/lipgloss"

// ─── Style Palette ─────────────────────────────────────────────────────────
//
// Dark theme with vibrant purple/magenta accents.

var (
	purple    = lipgloss.Color("99")
	purpleDim = lipgloss.Color("57")
	purpleBg  = lipgloss.Color("55")
	darkBg    = lipgloss.Color("236")
	subtle    = lipgloss.Color("241")
	white     = lipgloss.Color("255")
	red       = lipgloss.Color("196")
	redBg     = lipgloss.Color("52")
	green     = lipgloss.Color("82")
	yellow    = lipgloss.Color("220")
	cyan      = lipgloss.Color("45")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(purple)

	accentStyle = lipgloss.NewStyle().
			Foreground(purple).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(subtle)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	noticeStyle = lipgloss.NewStyle().
			Foreground(yellow)

	labelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(purple).
			Width(14).
			Align(lipgloss.Right)

	valueStyle = lipgloss.NewStyle().
			Foreground(white)

	statusBarStyle = lipgloss.NewStyle().
			Background(darkBg).
			Foreground(lipgloss.Color("250")).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1)

	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(purple).
			Padding(1, 2)

	deletePromptStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(red).
				Background(redBg).
				Bold(true).
				Padding(1, 2)

	appStyle = lipgloss.NewStyle().Margin(0, 1)

	separatorStyle = lipgloss.NewStyle().
			Foreground(purpleDim)

	filterActiveStyle = lipgloss.NewStyle().
				Foreground(green).
				Bold(true)

	filterInputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1)

	updateAvailableStyle = lipgloss.NewStyle().
				Foreground(yellow)
)

// statusColor returns a lipgloss color for the given game status.
func statusColor(s string) lipgloss.Color {
	switch s {
	case "active":
		return green
	case "completed":
		return cyan
	case "abandoned":
		return red
	case "on_hold":
		return yellow
	default:
		return subtle
	}
}

// engineColor returns a distinct lipgloss color for the given game engine.
func engineColor(e string) lipgloss.Color {
	switch e {
	case "Unity":
		return cyan
	case "RenPy":
		return lipgloss.Color("201") // magenta
	case "RPGM":
		return green
	case "UnrealEngine":
		return yellow
	case "HTML":
		return lipgloss.Color("39") // blue
	case "Flash":
		return lipgloss.Color("208") // orange
	case "Java":
		return lipgloss.Color("130") // brown-ish
	case "ADRIFT":
		return lipgloss.Color("141") // light purple
	case "QSP":
		return lipgloss.Color("75") // teal
	case "RAGS":
		return lipgloss.Color("172") // burnt orange
	case "Tads":
		return lipgloss.Color("42") // dark green
	case "WebGL":
		return lipgloss.Color("111") // sky blue
	case "WolfRPG":
		return lipgloss.Color("170") // warm orange
	case "Others":
		return subtle
	default:
		return subtle
	}
}
