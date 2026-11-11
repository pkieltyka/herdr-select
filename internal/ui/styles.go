package ui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	background      = lipgloss.Color("#0b0b0b")
	panelBackground = lipgloss.Color("#171717")
	foreground      = lipgloss.Color("#dddddd")
	muted           = lipgloss.Color("#999999")
	cyan            = lipgloss.Color("#4b9eab")
	textStyle       = lipgloss.NewStyle().Foreground(foreground).Background(panelBackground)
	mutedStyle      = textStyle.Foreground(muted)
	selectedStyle   = lipgloss.NewStyle().Foreground(background).Background(cyan)
	warningStyle    = textStyle.Foreground(lipgloss.Color("#d7af5f"))
)

// Escape controls rather than stripping escape sequences, so diagnostics cannot
// change terminal state and their original content remains recognizable.
func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func fit(s string, width int) string {
	width = max(0, width)
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
func centered(s string, width int, style lipgloss.Style) string {
	left := max(0, (width-ansi.StringWidth(s))/2)
	return style.Render(fit(strings.Repeat(" ", left)+s, width))
}
func wrap(s string, width int) []string {
	if s == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(safe(s), max(1, width), ""), "\n")
}

// Lip Gloss preserves nested SGR resets. Reapply the containing surface after
// each reset so styled labels do not leave terminal-background holes in panels.
func surface(content string, style lipgloss.Style) string {
	marker := style.Render("\x00")
	prefix := strings.SplitN(marker, "\x00", 2)[0]
	content = strings.ReplaceAll(content, "\x1b[m", "\x1b[m"+prefix)
	content = strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+prefix)
	return style.Render(content)
}
