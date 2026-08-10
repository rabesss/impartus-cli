package tui

import (
	"charm.land/bubbles/v2/key"
)

type keyMap struct {
	Navigate key.Binding
	Select   key.Binding
	Play     key.Binding
	Download key.Binding
	Library  key.Binding
	Filter   key.Binding
	Doctor   key.Binding
	Details  key.Binding
	Back     key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Navigate: key.NewBinding(key.WithKeys("up", "down", "k", "j"), key.WithHelp("↑/↓", "move")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select/play")),
		Play:     key.NewBinding(key.WithKeys("space", "left", "right", "m", "v", "+", "=", "-", "[", "]"), key.WithHelp("space/←/→/+/-/[]", "control mpv")),
		Download: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download")),
		Library:  key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "library")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Doctor:   key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "diagnostics")),
		Details:  key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "details")),
		Back:     key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (keys keyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Navigate, keys.Select, keys.Details, keys.Download, keys.Library, keys.Filter, keys.Doctor, keys.Back, keys.Quit}
}

func (keys keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Navigate, keys.Select, keys.Play},
		{keys.Details, keys.Download, keys.Library, keys.Filter, keys.Doctor},
		{keys.Back, keys.Quit},
	}
}
