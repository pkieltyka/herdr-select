// Package ui implements the keyboard-driven selector without owning interactive children.
package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/pkieltyka/herdr-select/internal/herdr"
)

type viewKind int

const (
	loading viewKind = iota
	picker
	newSession
	confirm
	startupError
	discoveryError
)

type LaunchRequest struct {
	Name   string
	Create bool
}
type Model struct {
	backend                         herdr.Backend
	view                            viewKind
	sessions                        []herdr.Session
	selected, scroll, width, height int
	input                           textinput.Model
	focus                           int // input, OK, Cancel
	attempted                       bool
	formError                       string
	confirmation                    target
	confirmAccept                   bool
	contentScroll                   int
	busy                            string
	operationID                     uint64
	loaded, stale                   bool
	notice                          string
	now                             time.Time
	compact                         bool
	formFooterHeight                int
	Launch                          *LaunchRequest
	ExitCode                        int
}

func New(backend herdr.Backend, now time.Time, compact bool) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "e.g. my-project"
	input.SetVirtualCursor(true)
	input.KeyMap.Paste.SetEnabled(false)
	styles := input.Styles()
	styles.Focused.Text = textStyle
	styles.Blurred.Text = textStyle
	styles.Focused.Placeholder = mutedStyle
	styles.Blurred.Placeholder = mutedStyle
	input.SetStyles(styles)
	return &Model{backend: backend, input: input, now: now, compact: compact, view: loading}
}

func Warning(message string, now time.Time, compact bool) *Model {
	m := New(nil, now, compact)
	m.view, m.notice, m.ExitCode = startupError, message, 1
	return m
}

func (m *Model) Init() tea.Cmd {
	if m.view == loading {
		return m.begin(refresh, "", target{})
	}
	if m.view == newSession && m.focus == 0 {
		return m.input.Focus()
	}
	return nil
}
func (m *Model) begin(kind action, name string, confirmed target) tea.Cmd {
	m.operationID++
	switch kind {
	case refresh:
		m.busy = "Loading sessions…"
	case create:
		m.busy = "Checking session name…"
	case open:
		m.busy = "Preparing to open…"
	case prepareStop, prepareDelete:
		m.busy = "Checking session…"
	case mutate:
		if confirmed.Delete {
			m.busy = "Deleting…"
			if confirmed.Running {
				m.busy = "Stopping / deleting…"
			}
		} else {
			m.busy = "Stopping…"
		}
	}
	return operation(m.backend, m.operationID, kind, name, confirmed)
}
func (m *Model) current() (herdr.Session, bool) {
	if m.selected < 1 || m.selected > len(m.sessions) {
		return herdr.Session{}, false
	}
	return m.sessions[m.selected-1], true
}
func (m *Model) replaceSessions(sessions []herdr.Session) {
	old, had := m.current()
	m.sessions = sessions
	if !m.loaded {
		m.selected = 0
		for i, s := range sessions {
			if s.Default {
				m.selected = i + 1
				break
			}
		}
	} else if had {
		m.selected = min(m.selected, len(sessions))
		for i, s := range sessions {
			if s.Name == old.Name {
				m.selected = i + 1
				break
			}
		}
	}
	m.loaded, m.stale = true, false
	m.ExitCode = 0
	m.ensureVisible()
}
func (m *Model) LaunchFailed(err error) {
	m.Launch = nil
	m.busy = ""
	if m.view == newSession {
		m.formError = "Could not start Herdr: " + err.Error()
		m.focus = 0
		m.input.Focus()
	} else {
		m.view = picker
		m.notice = "Could not start Herdr: " + err.Error()
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, m.panelWidth()-12))
		m.ensureVisible()
		return m, nil
	case result:
		if msg.id != m.operationID || m.busy == "" {
			return m, nil
		}
		m.busy = ""
		if msg.listed {
			// Approved UI feedback replaces ticking ages with a session-load snapshot.
			m.now = msg.listedAt
			m.replaceSessions(msg.sessions)
		}
		if msg.refreshErr != nil || !msg.listed && msg.kind != create {
			m.stale = true
		}
		if msg.kind == create {
			if msg.err != nil {
				m.formError = msg.err.Error()
				m.focus = 0
				return m, m.input.Focus()
			}
		} else if msg.kind == refresh && msg.err != nil && !m.loaded {
			m.view, m.notice, m.ExitCode = discoveryError, msg.err.Error(), 1
			return m, nil
		} else {
			m.view = picker
		}
		m.notice = msg.notice
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		if msg.stoppedButNotDeleted {
			m.notice = "Session stopped, but not deleted. " + m.notice
		}
		if msg.refreshErr != nil {
			m.notice += " Refresh failed: " + msg.refreshErr.Error()
		}
		if m.stale {
			m.notice += " Session list is stale; refresh before continuing."
		}
		m.contentScroll = 0
		if msg.confirmation != nil {
			m.confirmation = *msg.confirmation
			m.confirmAccept = false
			m.view = confirm
		}
		if msg.launch != nil {
			m.Launch = msg.launch
			return m, tea.Quit
		}
		m.ensureVisible()
		return m, nil
	}
	if m.busy != "" {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		k := key.String()
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		if m.tooSmall() {
			if k == "q" || k == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch m.view {
		case startupError:
			if k == "q" || k == "esc" || k == "enter" {
				return m, tea.Quit
			}
			m.scrollContent(k)
		case loading:
			if k == "q" || k == "esc" {
				return m, tea.Quit
			}
		case discoveryError:
			if k == "q" || k == "esc" {
				return m, tea.Quit
			}
			if k == "r" || k == "enter" {
				return m, m.begin(refresh, "", target{})
			}
			m.scrollContent(k)
		case picker:
			switch k {
			case "q", "esc":
				return m, tea.Quit
			case "up":
				m.selected = max(0, m.selected-1)
				m.contentScroll = 0
			case "down":
				m.selected = min(len(m.sessions), m.selected+1)
				m.contentScroll = 0
			case "home":
				m.selected = 0
				m.contentScroll = 0
			case "pgup", "pgdown":
				m.scrollContent(k)
			case "r":
				return m, m.begin(refresh, "", target{})
			case "enter":
				if m.stale {
					return m, nil
				}
				if s, ok := m.current(); ok {
					return m, m.begin(open, s.Name, target{})
				}
				m.view = newSession
				m.input.SetValue("")
				m.formError = ""
				m.attempted = false
				m.focus = 0
				m.formFooterHeight = len(m.footerLinesFor([]string{"New [enter]", "Refresh [r]"}))
				return m, m.input.Focus()
			case "S", "shift+s", "D", "shift+d":
				s, ok := m.current()
				if !ok || m.stale || s.ConnectionError != "" {
					return m, nil
				}
				if (k == "S" || k == "shift+s") && s.Running {
					return m, m.begin(prepareStop, s.Name, target{})
				}
				if (k == "D" || k == "shift+d") && !s.Default {
					return m, m.begin(prepareDelete, s.Name, target{})
				}
			}
			m.ensureVisible()
		case confirm:
			switch k {
			case "esc":
				m.view = picker
				m.notice = ""
				m.contentScroll = 0
			case "left", "right", "tab", "shift+tab":
				m.confirmAccept = !m.confirmAccept
			case "enter":
				if !m.confirmAccept {
					m.view = picker
					m.notice = ""
					m.contentScroll = 0
				} else {
					return m, m.begin(mutate, m.confirmation.Name, m.confirmation)
				}
			default:
				m.scrollContent(k)
			}
		case newSession:
			switch k {
			case "pgup", "pgdown":
				m.scrollContent(k)
				return m, nil
			case "esc":
				m.cancelForm()
				return m, nil
			case "tab", "shift+tab":
				direction := 1
				if k == "shift+tab" {
					direction = -1
				}
				m.focus = (m.focus + direction + 3) % 3
				if m.focus == 1 && !m.validForm() {
					m.focus = (m.focus + direction + 3) % 3
				}
				if m.focus == 0 {
					return m, m.input.Focus()
				}
				m.input.Blur()
				return m, nil
			case "enter":
				if m.focus == 2 {
					m.cancelForm()
					return m, nil
				}
				m.attempted = true
				if !m.validForm() {
					return m, nil
				}
				m.formError = ""
				m.input.Blur()
				return m, m.begin(create, m.input.Value(), target{})
			}
		}
	}
	if m.view == newSession && m.focus == 0 {
		if paste, ok := msg.(tea.PasteMsg); ok {
			if len(paste.Content) > 4096 {
				m.formError = "Paste is too large (maximum 4 KiB)."
				return m, nil
			}
			if strings.ContainsFunc(paste.Content, unicode.IsControl) {
				m.formError = "Pasted control characters are not allowed."
				return m, nil
			}
		}
		old := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != old {
			m.formError = ""
		}
		return m, cmd
	}
	return m, nil
}
func (m *Model) validForm() bool { return herdr.ValidateNewName(m.input.Value(), m.sessions) == nil }
func (m *Model) validationMessage() string {
	if m.formError != "" {
		return m.formError
	}
	if m.input.Value() == "" && !m.attempted {
		return ""
	}
	if err := herdr.ValidateNewName(m.input.Value(), m.sessions); err != nil {
		return err.Error()
	}
	return ""
}
func (m *Model) cancelForm() {
	m.view = picker
	m.selected = 0
	m.scroll = 0
	m.input.Blur()
	m.notice = ""
	m.contentScroll = 0
}
func (m *Model) scrollContent(key string) {
	pageSize, maximum := m.contentScrollMetrics()
	switch key {
	case "down":
		m.contentScroll++
	case "up":
		m.contentScroll = max(0, m.contentScroll-1)
	case "pgdown":
		m.contentScroll += pageSize
	case "pgup":
		m.contentScroll = max(0, m.contentScroll-pageSize)
	case "home":
		m.contentScroll = 0
	}
	m.contentScroll = min(m.contentScroll, maximum)
}
func (m *Model) String() string {
	return fmt.Sprintf("view=%d selected=%d busy=%s", m.view, m.selected, m.busy)
}
