package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/mili/moxie/internal/engine"
)

// ─── Style Palette ─────────────────────────────────────────────────────────
//
// Adaptive theme: detects terminal background and applies appropriate palette.

var (
	purple    lipgloss.Color
	purpleDim lipgloss.Color
	purpleBg  lipgloss.Color
	darkBg    lipgloss.Color
	subtle    lipgloss.Color
	white     lipgloss.Color
	red       lipgloss.Color
	redBg     lipgloss.Color
	green     lipgloss.Color
	yellow    lipgloss.Color
	cyan      lipgloss.Color

	titleStyle          lipgloss.Style
	accentStyle         lipgloss.Style
	subtleStyle         lipgloss.Style
	errorStyle          lipgloss.Style
	noticeStyle         lipgloss.Style
	labelStyle          lipgloss.Style
	valueStyle          lipgloss.Style
	statusBarStyle      lipgloss.Style
	boxStyle            lipgloss.Style
	helpBoxStyle        lipgloss.Style
	deletePromptStyle   lipgloss.Style
	appStyle            lipgloss.Style
	separatorStyle      lipgloss.Style
	filterActiveStyle   lipgloss.Style
	filterInputStyle    lipgloss.Style
	updateAvailableStyle lipgloss.Style
	greenStyle          lipgloss.Style
	redStyle            lipgloss.Style
	noPathStyle         lipgloss.Style
	sectionHeaderStyle  lipgloss.Style
	tagStyle            lipgloss.Style
	copyHintStyle       lipgloss.Style
	statusOptionStyle   lipgloss.Style
)

var (
	statusStyles map[string]lipgloss.Style
	engineStyles map[string]lipgloss.Style
)

func init() {
	initStyles()
}

// initStyles initializes all colors and styles based on the terminal's
// background color (dark or light). Called once at package init.
func initStyles() {
	isDark := lipgloss.HasDarkBackground()
	if isDark {
		purple = lipgloss.Color("99")
		purpleDim = lipgloss.Color("57")
		purpleBg = lipgloss.Color("55")
		darkBg = lipgloss.Color("236")
		subtle = lipgloss.Color("241")
		white = lipgloss.Color("255")
	} else {
		purple = lipgloss.Color("93")
		purpleDim = lipgloss.Color("99")
		purpleBg = lipgloss.Color("189")
		darkBg = lipgloss.Color("254")
		subtle = lipgloss.Color("243")
		white = lipgloss.Color("16")
	}
	red = lipgloss.Color("196")
	redBg = lipgloss.Color("52")
	green = lipgloss.Color("82")
	yellow = lipgloss.Color("220")
	cyan = lipgloss.Color("45")

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
		Foreground(white).
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

	greenStyle = lipgloss.NewStyle().
		Foreground(green).
		Bold(true)

	redStyle = lipgloss.NewStyle().
		Foreground(red).
		Bold(true)

	// noPathStyle is used for games that are not found on disk.
	noPathStyle = lipgloss.NewStyle().
		Foreground(subtle)

	// sectionHeaderStyle renders section headers in the detail view.
	sectionHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(white).
		Background(purpleBg).
		Padding(0, 1).
		MarginTop(1).
		MarginBottom(1)

	// tagStyle renders individual tags in the detail view.
	tagStyle = lipgloss.NewStyle().
		Foreground(purple).
		Background(lipgloss.Color("237")).
		Padding(0, 1).
		MarginRight(1)

	// copyHintStyle renders hints for copyable URLs.
	copyHintStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Italic(true)

	// statusOptionStyle renders interactive status selector options.
	statusOptionStyle = lipgloss.NewStyle().
		Foreground(white).
		Bold(true)

	statusStyles = map[string]lipgloss.Style{
		"active":    lipgloss.NewStyle().Foreground(green),
		"completed": lipgloss.NewStyle().Foreground(cyan),
		"abandoned": lipgloss.NewStyle().Foreground(red),
		"on_hold":   lipgloss.NewStyle().Foreground(yellow),
		"unknown":   lipgloss.NewStyle().Foreground(subtle),
	}

	engineStyles = map[string]lipgloss.Style{
		"Unity":        lipgloss.NewStyle().Foreground(engineColor("Unity")),
		"RenPy":        lipgloss.NewStyle().Foreground(engineColor("RenPy")),
		"RPGM":         lipgloss.NewStyle().Foreground(engineColor("RPGM")),
		"UnrealEngine": lipgloss.NewStyle().Foreground(engineColor("UnrealEngine")),
		"Godot":        lipgloss.NewStyle().Foreground(engineColor("Godot")),
		"HTML":         lipgloss.NewStyle().Foreground(engineColor("HTML")),
		"Flash":        lipgloss.NewStyle().Foreground(engineColor("Flash")),
		"Java":         lipgloss.NewStyle().Foreground(engineColor("Java")),
		"ADRIFT":       lipgloss.NewStyle().Foreground(engineColor("ADRIFT")),
		"QSP":          lipgloss.NewStyle().Foreground(engineColor("QSP")),
		"RAGS":         lipgloss.NewStyle().Foreground(engineColor("RAGS")),
		"Tads":         lipgloss.NewStyle().Foreground(engineColor("Tads")),
		"WebGL":        lipgloss.NewStyle().Foreground(engineColor("WebGL")),
		"WolfRPG":      lipgloss.NewStyle().Foreground(engineColor("WolfRPG")),
		"Others":       lipgloss.NewStyle().Foreground(engineColor("Others")),
	}
}

// statusStyle returns a cached style for the given status, falling back
// to a dynamically created style for unknown values.
func statusStyle(s string) lipgloss.Style {
	if st, ok := statusStyles[s]; ok {
		return st
	}
	return lipgloss.NewStyle().Foreground(statusColor(s))
}

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

// engineStyle returns a cached style for the given engine name, falling back
// to a dynamically created style for unknown engines.
func engineStyle(engine string) lipgloss.Style {
	if s, ok := engineStyles[engine]; ok {
		return s
	}
	return lipgloss.NewStyle().Foreground(engineColor(engine))
}

// engineColor returns the canonical F95Zone-mirrored color for the given
// game engine, falling back to the subtle default for unknown engines.
// The palette lives in internal/engine/engine-colors.json (single source of
// truth, shared with the desktop frontend) — see internal/engine/colors.go.
func engineColor(e string) lipgloss.Color {
	if c := engine.EngineColor(e); c != "" {
		return lipgloss.Color(c)
	}
	return subtle
}
