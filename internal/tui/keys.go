package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all key bindings for the TUI.
type KeyMap struct {
	// Navigation
	Up       key.Binding
	Down     key.Binding
	Back     key.Binding
	GoDetail key.Binding

	// Library actions
	Sort       key.Binding
	Reverse    key.Binding
	Filter     key.Binding
	Delete     key.Binding
	EngFilter  key.Binding
	StatFilter key.Binding
	PrevPage   key.Binding
	NextPage   key.Binding

	// Detail actions
	EditTitle   key.Binding
	SetExe      key.Binding
	SetURL      key.Binding
	Play        key.Binding
	Download    key.Binding
	ShowPath    key.Binding
	CycleStatus key.Binding

	// Global
	Help      key.Binding
	Quit      key.Binding
	ForceQuit key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Navigation
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "left", "backspace"),
			key.WithHelp("esc", "back"),
		),
		GoDetail: key.NewBinding(
			key.WithKeys("enter", "e"),
			key.WithHelp("enter", "details"),
		),

		// Library actions
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "cycle sort"),
		),
		Reverse: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reverse sort"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		EngFilter: key.NewBinding(
			key.WithKeys("ctrl+e"),
			key.WithHelp("ctrl+e", "engine filter"),
		),
		StatFilter: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "status filter"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("left", "pgup"),
			key.WithHelp("←/pgup", "prev page"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("right", "pgdn"),
			key.WithHelp("→/pgdn", "next page"),
		),

		// Detail actions
		EditTitle: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit title"),
		),
		SetExe: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "set exe"),
		),
		SetURL: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "set URL"),
		),
		Play: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "play"),
		),
		Download: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "download"),
		),
		ShowPath: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "show path"),
		),
		CycleStatus: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "cycle status"),
		),

		// Global
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
		),
	}
}

// ShortHelp implements help.KeyMap interface.
func (km KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		km.Up, km.Down, km.GoDetail, km.Filter, km.Sort, km.Reverse,
		km.Delete, km.PrevPage, km.NextPage, km.Help, km.Quit,
	}
}

// FullHelp implements help.KeyMap interface.
func (km KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.Up, km.Down, km.Back, km.GoDetail},
		{km.Sort, km.Reverse, km.Filter, km.Delete, km.EngFilter, km.StatFilter, km.PrevPage, km.NextPage},
		{km.EditTitle, km.CycleStatus, km.SetExe, km.SetURL, km.Play, km.Download, km.ShowPath},
		{km.Help, km.Quit, km.ForceQuit},
	}
}
