package app

// Keep the approved UI modules in the reproducible phase-1 vendor closure.
// Phase 2 replaces these temporary anchors with the actual UI imports.
import (
	_ "charm.land/bubbles/v2/textinput"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/charmbracelet/x/ansi"
)
