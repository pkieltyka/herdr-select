package ui

import (
	_ "embed"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkieltyka/herdr-select/internal/herdr"
)

//go:embed ram.txt
var ram string

const (
	minimumWidth = 80
	// Full ram (16), minimum form (9), panel padding (2), footer reserve (1).
	minimumHeight = 28
)

type layout struct {
	panelWidth, bodyWidth, available, capacity int
	logoGap                                    int
	logo                                       []string
	footer                                     []string
	small                                      bool
}

func (m *Model) panelWidth() int { return max(1, min(76, m.width-4)) }
func (m *Model) footerActions() []string {
	if m.view == newSession {
		return nil
	}
	if m.busy != "" {
		return []string{m.busy}
	}
	switch m.view {
	case startupError:
		return []string{"Quit [q]"}
	case discoveryError:
		return []string{"Retry [r]", "Quit [q]"}
	case confirm:
		return []string{"Confirm [enter]"}
	case loading:
		return nil
	}
	if m.stale {
		return []string{"Refresh [r]"}
	}
	s, ok := m.current()
	if !ok {
		return []string{"New [enter]", "Refresh [r]"}
	}
	if s.ConnectionError != "" {
		return []string{"Open [enter]", "Refresh [r]"}
	}
	actions := []string{"Resume [enter]"}
	if s.Running {
		actions = []string{"Open [enter]", "Stop [shift+s]"}
	}
	if !s.Default {
		actions = append(actions, "Delete [shift+d]")
	}
	return append(actions, "Refresh [r]")
}
func (m *Model) footerLinesFor(actions []string) []string {
	var lines []string
	line := ""
	for _, action := range actions {
		badge := " " + action + " "
		if ansi.StringWidth(line)+ansi.StringWidth(badge)+1 > m.width && line != "" {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += badge
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
func (m *Model) layout() layout {
	l := layout{panelWidth: m.panelWidth()}
	l.bodyWidth = max(1, l.panelWidth-6)
	l.footer = m.footerLinesFor(m.footerActions())
	reserve := len(l.footer)
	if m.view == newSession {
		reserve = min(m.formFooterHeight, max(0, m.height-15))
	}
	l.available = max(0, m.height-reserve)
	l.logo = strings.Split(strings.TrimRight(ram, "\n"), "\n")
	if m.compact {
		l.logo = []string{"herdr"}
	}
	// Preserve enough room for the complete form before adding decorative space.
	// Budget for the form in every view to keep decorative spacing consistent.
	l.logoGap = min(2, max(0, l.available-len(l.logo)-2-m.formHeight(l.bodyWidth)))
	l.capacity = max(0, l.available-len(l.logo)-l.logoGap-2)
	l.small = m.tooSmall()
	return l
}
func longest(lines []string) string {
	s := ""
	for _, line := range lines {
		if ansi.StringWidth(line) > ansi.StringWidth(s) {
			s = line
		}
	}
	return s
}
func (m *Model) tooSmall() bool {
	return m.width < minimumWidth || m.height < minimumHeight
}

func (m *Model) resizeMessage() []string {
	current := fmt.Sprintf("Current: %d × %d", m.width, m.height)
	minimum := fmt.Sprintf("Minimum: %d × %d", minimumWidth, minimumHeight)
	dimensions := fmt.Sprintf("%d×%d → %d×%d", m.width, m.height, minimumWidth, minimumHeight)
	candidates := [][]string{
		{"Terminal too small", "", current, minimum, "", "Resize to continue"},
		{"Terminal too small", current, minimum, "Resize to continue"},
		{"Resize to continue", dimensions},
		{"Resize " + dimensions},
	}
	for _, lines := range candidates {
		if len(lines) <= m.height && ansi.StringWidth(longest(lines)) <= m.width {
			return lines
		}
	}
	return []string{ansi.Truncate("Resize "+dimensions, m.width, "…")}
}
func (m *Model) pickerNotice() string {
	n := m.notice
	if s, ok := m.current(); ok && s.ConnectionError != "" {
		if n != "" {
			n += " "
		}
		n += "Unavailable: " + s.ConnectionError
	}
	return n
}
func (m *Model) pickerSpace(l layout) (rows, diagnostics, separator int) {
	capacity := l.capacity
	if m.pickerNotice() != "" {
		minimumList := 1
		if len(m.sessions) > 0 {
			minimumList++
		}
		diagnostics = min(3, max(1, capacity-minimumList))
		if capacity-minimumList-diagnostics > 0 {
			separator = 1
		}
		capacity -= diagnostics + separator
	}
	if len(m.sessions)+1 > capacity {
		capacity--
	}
	return max(1, capacity), diagnostics, separator
}
func (m *Model) listCapacity(l layout) int {
	rows, _, _ := m.pickerSpace(l)
	return rows
}
func (m *Model) ensureVisible() {
	if m.tooSmall() {
		return
	}
	cap := m.listCapacity(m.layout())
	m.scroll = min(m.scroll, max(0, len(m.sessions)+1-cap))
	if m.selected < m.scroll {
		m.scroll = m.selected
	}
	if m.selected >= m.scroll+cap {
		m.scroll = m.selected - cap + 1
	}
	m.scroll = max(0, m.scroll)
}
func (m *Model) View() tea.View {
	l := m.layout()
	base := lipgloss.NewStyle().Foreground(foreground).Background(background)
	if m.width <= 0 || m.height <= 0 {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	var screen []string
	if l.small {
		message := m.resizeMessage()
		for range max(0, (m.height-len(message))/2) {
			screen = append(screen, base.Render(strings.Repeat(" ", m.width)))
		}
		for i, line := range message {
			style := base
			if i == 0 {
				style = style.Foreground(cyan).Bold(true)
			}
			screen = append(screen, centered(line, m.width, style))
		}
	} else {
		var body []string
		switch m.view {
		case picker:
			body = m.pickerBody(l)
		case newSession:
			body = m.formBody(l)
		case confirm:
			buttons := m.confirmButtons(l.bodyWidth)
			body = m.scrolled(m.confirmLines(l.bodyWidth), max(1, l.capacity-len(buttons)-1), l.bodyWidth)
			body = append(body, "")
			body = append(body, buttons...)
		case loading:
			body = []string{"Loading sessions…"}
		default:
			title := "Unable to load sessions"
			if m.view == startupError {
				title = "Herdr Select"
			}
			body = []string{textStyle.Bold(true).Render(title), ""}
			for _, line := range m.scrolled(wrap(m.notice, l.bodyWidth), max(1, l.capacity-2), l.bodyWidth) {
				body = append(body, errorStyle.Render(line))
			}
		}
		groupHeight := len(l.logo) + l.logoGap + len(body) + 2
		top := max(0, (l.available-groupHeight)/2)
		for range top {
			screen = append(screen, base.Render(strings.Repeat(" ", m.width)))
		}
		logoStyle := base.Foreground(lipgloss.Color("#bcbcbc"))
		// Center the art as a block, preserving the silhouette's leading spaces.
		logoWidth := ansi.StringWidth(longest(l.logo))
		for _, line := range l.logo {
			screen = append(screen, centered(fit(line, logoWidth), m.width, logoStyle))
		}
		for range l.logoGap {
			screen = append(screen, base.Render(strings.Repeat(" ", m.width)))
		}
		panel := lipgloss.NewStyle().Background(panelBackground).Foreground(foreground)
		screen = append(screen, centered(panel.Render(strings.Repeat(" ", l.panelWidth)), m.width, base))
		for index, line := range body {
			// Picker rows already include their inner padding and full selection bar.
			row := surface("   "+fit(line, l.bodyWidth)+"   ", panel)
			if m.view == picker && index == m.selected-m.scroll {
				row = surface("  "+selectedStyle.Render(" ")+fit(line, l.bodyWidth)+selectedStyle.Render(" ")+"  ", panel)
			}
			screen = append(screen, centered(row, m.width, base))
		}
		screen = append(screen, centered(panel.Render(strings.Repeat(" ", l.panelWidth)), m.width, base))
	}
	for len(screen) < m.height-len(l.footer) {
		screen = append(screen, base.Render(strings.Repeat(" ", m.width)))
	}
	if !l.small {
		for _, line := range l.footer {
			screen = append(screen, m.styledFooter(line))
		}
	}
	for len(screen) < m.height {
		screen = append(screen, base.Render(strings.Repeat(" ", m.width)))
	}
	if len(screen) > m.height {
		screen = screen[:m.height]
	}
	v := tea.NewView(strings.Join(screen, "\n"))
	v.AltScreen = true
	v.BackgroundColor = background
	v.ForegroundColor = foreground
	return v
}
func (m *Model) styledFooter(line string) string {
	// Badges have a lighter surface; the inter-badge gaps keep the strip color.
	badgeStyle := lipgloss.NewStyle().Background(lipgloss.Color("#292933")).Foreground(foreground)
	var styled []string
	for _, badge := range strings.Split(strings.TrimSpace(line), "   ") {
		if at := strings.Index(badge, "["); at >= 0 {
			styled = append(styled, badgeStyle.Render(" "+badge[:at])+badgeStyle.Foreground(muted).Render(badge[at:]+" "))
		} else {
			styled = append(styled, badgeStyle.Render(" "+badge+" "))
		}
	}
	content := centered(strings.Join(styled, " "), m.width, lipgloss.NewStyle())
	return surface(content, lipgloss.NewStyle().Background(panelBackground))
}
func (m *Model) pickerBody(l layout) []string {
	capacity, diagnostics, separator := m.pickerSpace(l)
	start := min(m.scroll, len(m.sessions))
	end := min(len(m.sessions)+1, start+capacity)
	statusWidth, ageWidth := 0, 0
	for i := start; i < end; i++ {
		if i > 0 {
			status, age := m.metadata(m.sessions[i-1])
			statusWidth = max(statusWidth, ansi.StringWidth(status))
			ageWidth = max(ageWidth, ansi.StringWidth(age))
		}
	}
	var lines []string
	for i := start; i < end; i++ {
		name := "New Session"
		status, age := "", ""
		if i > 0 {
			name = safe(m.sessions[i-1].Name)
			status, age = m.metadata(m.sessions[i-1])
		}
		nameWidth := l.bodyWidth
		if i > 0 {
			nameWidth = max(1, l.bodyWidth-statusWidth-ageWidth-4)
		}
		label := fit(name, nameWidth)
		tail := ""
		if i > 0 {
			tail = "  " + fit(status, statusWidth) + "  " + strings.Repeat(" ", max(0, ageWidth-ansi.StringWidth(age))) + age
		}
		if i == m.selected {
			lines = append(lines, selectedStyle.Bold(true).Render(label)+selectedStyle.Render(tail))
		} else {
			lines = append(lines, textStyle.Render(label)+mutedStyle.Render(tail))
		}
	}
	if len(m.sessions)+1 > capacity {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("%d–%d of %d", start+1, end, len(m.sessions)+1)))
	}
	if notice := m.pickerNotice(); notice != "" {
		if separator > 0 {
			lines = append(lines, "")
		}
		for _, line := range m.scrolled(wrap(notice, l.bodyWidth), diagnostics, l.bodyWidth) {
			lines = append(lines, m.noticeStyle().Render(line))
		}
	}

	return lines
}
func (m *Model) noticeStyle() lipgloss.Style {
	if m.noticeSeverity == noticeError {
		return errorStyle
	}
	if s, ok := m.current(); m.view == picker && ok && s.ConnectionError != "" {
		return errorStyle
	}
	return warningStyle
}
func (m *Model) metadata(s herdr.Session) (string, string) {
	status := "Stopped"
	if s.Running {
		status = "Running"
	}
	if s.ConnectionError != "" {
		status = "Unavailable"
	}
	age := "—"
	if s.HasModifiedAt {
		age = herdr.RelativeAge(s.ModifiedAt, m.now)
	}
	return status, age
}
func (m *Model) formHeight(width int) int {
	// One diagnostic row is essential; longer messages scroll within the space
	// available at this terminal size and cannot make the input uneditable.
	return 8 + len(wrap("1-64 characters, no spaces: A-Z a-z 0-9 . _ -", width))
}
func (m *Model) formBody(l layout) []string {
	border := muted
	if m.focus == 0 && m.busy == "" {
		border = cyan
	}
	field := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Background(lipgloss.Color("#202024")).Width(l.bodyWidth - 2).Render(surface(fit(m.input.View(), l.bodyWidth-4), textStyle.Background(lipgloss.Color("#202024"))))
	divider := textStyle.Foreground(lipgloss.Color("#444444")).Render(strings.Repeat("─", l.bodyWidth))
	lines := []string{textStyle.Bold(true).Render("New Session"), divider, textStyle.Render("Session name")}
	lines = append(lines, strings.Split(field, "\n")...)
	for _, line := range wrap("1-64 characters, no spaces: A-Z a-z 0-9 . _ -", l.bodyWidth) {
		lines = append(lines, mutedStyle.Render(line))
	}
	diagnostic := m.validationMessage()
	if m.busy != "" {
		diagnostic = m.busy
	}
	errors := wrap(diagnostic, l.bodyWidth)
	if len(errors) == 0 {
		errors = []string{""}
	}
	diagnosticCapacity := m.formDiagnosticCapacity(l)
	visibleErrors := m.scrolled(errors, diagnosticCapacity, l.bodyWidth)
	for len(visibleErrors) < diagnosticCapacity {
		visibleErrors = append(visibleErrors, "")
	}
	diagnosticStyle := errorStyle
	if m.busy != "" {
		diagnosticStyle = mutedStyle
	}
	for _, line := range visibleErrors {
		lines = append(lines, diagnosticStyle.Render(line))
	}
	lines = append(lines, centered(m.button("OK", m.focus == 1, m.validForm() && m.busy == "")+"   "+m.button("Cancel", m.focus == 2, m.busy == ""), l.bodyWidth, textStyle))
	return lines
}
func (m *Model) button(label string, focused, enabled bool) string {
	style := textStyle.Background(lipgloss.Color("#292933"))
	if !enabled {
		style = style.Foreground(lipgloss.Color("#666666"))
	} else if focused {
		style = selectedStyle
	}
	return style.Render(" " + label + " ")
}
func (m *Model) confirmTitle() string {
	if m.confirmation.Delete {
		if m.confirmation.Running {
			return "Stop and delete session"
		}
		return "Delete session"
	}
	return "Stop session"
}
func (m *Model) confirmLines(width int) []string {
	title := m.confirmTitle()
	lines := []string{warningStyle.Bold(true).Render(title), ""}
	lines = append(lines, wrap(title+" ‘"+m.confirmation.Name+"’?", width)...)
	consequence := "Running work will be interrupted. Saved session state will remain."
	if m.confirmation.Delete {
		consequence = "The saved session will be removed."
		if m.confirmation.Running {
			consequence = "Running work will be interrupted. The saved session will be removed."
		}
	}
	lines = append(lines, "")
	lines = append(lines, wrap(consequence, width)...)
	if m.notice != "" {
		lines = append(lines, "")
		for _, line := range wrap(m.notice, width) {
			lines = append(lines, m.noticeStyle().Render(line))
		}
	}
	return lines
}
func (m *Model) confirmButtons(width int) []string {
	action := "Stop"
	if m.confirmation.Delete {
		action = "Delete"
		if m.confirmation.Running {
			action = "Stop and delete"
		}
	}
	cancel, accept := m.button("Cancel", !m.confirmAccept, true), m.button(action, m.confirmAccept, true)
	if ansi.StringWidth(cancel)+ansi.StringWidth(accept)+2 > width {
		return []string{centered(cancel, width, textStyle), centered(accept, width, textStyle)}
	}
	return []string{centered(cancel+"  "+accept, width, textStyle)}
}
func (m *Model) formDiagnosticCapacity(l layout) int {
	return min(2, max(1, l.capacity-(m.formHeight(l.bodyWidth)-1)))
}

func contentScrollMetrics(lineCount, capacity int) (visible, maximum int) {
	capacity = max(1, capacity)
	if lineCount <= capacity {
		return capacity, 0
	}
	// A separate scroll hint consumes one row unless the viewport is a single row.
	visible = max(1, capacity-1)
	return visible, lineCount - visible
}

func scrollContentLines(lines []string, capacity, width int) []string {
	if capacity > 1 || len(lines) <= 1 {
		return lines
	}
	// A one-row viewport shares its row with the scroll hint. Rewrap rather
	// than truncate so every character remains reachable on subsequent pages.
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Wrap(line, max(1, width-2), ""), "\n")...)
	}
	return wrapped
}

func (m *Model) contentScrollMetrics() (int, int) {
	l := m.layout()
	var lines []string
	capacity := l.capacity - 2
	switch m.view {
	case picker:
		_, capacity, _ = m.pickerSpace(l)
		lines = wrap(m.pickerNotice(), l.bodyWidth)
	case newSession:
		capacity = m.formDiagnosticCapacity(l)
		lines = wrap(m.validationMessage(), l.bodyWidth)
	case confirm:
		capacity = l.capacity - len(m.confirmButtons(l.bodyWidth)) - 1
		lines = m.confirmLines(l.bodyWidth)
	default:
		lines = wrap(m.notice, l.bodyWidth)
	}
	return contentScrollMetrics(len(scrollContentLines(lines, capacity, l.bodyWidth)), capacity)
}

func (m *Model) scrolled(lines []string, capacity, width int) []string {
	lines = scrollContentLines(lines, capacity, width)
	if len(lines) <= capacity {
		return lines
	}
	capacity = max(1, capacity)
	if capacity == 1 {
		return []string{lines[min(m.contentScroll, len(lines)-1)] + " ↕"}
	}
	visible, maximum := contentScrollMetrics(len(lines), capacity)
	start := min(m.contentScroll, maximum)
	out := append([]string(nil), lines[start:min(len(lines), start+visible)]...)
	return append(out, mutedStyle.Render("↑↓ / pgup pgdown: more"))
}
