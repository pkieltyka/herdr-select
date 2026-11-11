package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkieltyka/herdr-select/internal/herdr"
)

type listReply struct {
	sessions []herdr.Session
	err      error
}
type fakeBackend struct {
	sessions           []herdr.Session
	replies            []listReply
	calls              []string
	stopErr, deleteErr error
}

func (b *fakeBackend) List(context.Context) ([]herdr.Session, error) {
	b.calls = append(b.calls, "list")
	if len(b.replies) > 0 {
		r := b.replies[0]
		b.replies = b.replies[1:]
		return slices.Clone(r.sessions), r.err
	}
	return slices.Clone(b.sessions), nil
}
func (b *fakeBackend) Stop(_ context.Context, name string) error {
	b.calls = append(b.calls, "stop:"+name)
	if b.stopErr == nil {
		for i := range b.sessions {
			if b.sessions[i].Name == name {
				b.sessions[i].Running = false
			}
		}
	}
	return b.stopErr
}
func (b *fakeBackend) Delete(_ context.Context, name string) error {
	b.calls = append(b.calls, "delete:"+name)
	if b.deleteErr == nil {
		b.sessions = slices.DeleteFunc(b.sessions, func(s herdr.Session) bool { return s.Name == name })
	}
	return b.deleteErr
}
func session(name string, running bool) herdr.Session {
	return herdr.Session{Name: name, Running: running, Default: name == "default", ModifiedAt: time.Unix(100, 0), HasModifiedAt: true}
}
func ready(sessions ...herdr.Session) (*Model, *fakeBackend) {
	b := &fakeBackend{sessions: sessions}
	m := New(b, time.Unix(110, 0), false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	cmd := m.begin(refresh, "", target{})
	m.Update(cmd())
	return m, b
}
func press(m *Model, key string) tea.Cmd {
	k := tea.Key{Text: key}
	switch key {
	case "enter":
		k = tea.Key{Code: tea.KeyEnter}
	case "backspace":
		k = tea.Key{Code: tea.KeyBackspace}
	case "esc":
		k = tea.Key{Code: tea.KeyEscape}
	case "up":
		k = tea.Key{Code: tea.KeyUp}
	case "down":
		k = tea.Key{Code: tea.KeyDown}
	case "home":
		k = tea.Key{Code: tea.KeyHome}
	case "tab":
		k = tea.Key{Code: tea.KeyTab}
	case "shift+tab":
		k = tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}
	case "ctrl+c":
		k = tea.Key{Code: 'c', Mod: tea.ModCtrl}
	case "right":
		k = tea.Key{Code: tea.KeyRight}
	case "pgdown":
		k = tea.Key{Code: tea.KeyPgDown}
	case "pgup":
		k = tea.Key{Code: tea.KeyPgUp}
	default:
		if len([]rune(key)) == 1 {
			k.Code = []rune(key)[0]
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg(k))
	return cmd
}
func run(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}
func plain(m *Model) string { return ansi.Strip(m.View().Content) }

func TestPickerSelectionAndRefresh(t *testing.T) {
	m, b := ready(session("fresh", true), session("default", false), session("old", false))
	if m.selected != 2 {
		t.Fatalf("default index = %d", m.selected)
	}
	b.sessions = []herdr.Session{session("old", false), session("fresh", true), session("default", false)}
	run(m, press(m, "r"))
	if m.selected != 3 {
		t.Fatal("refresh lost identity")
	}
	b.sessions = b.sessions[:2]
	run(m, press(m, "r"))
	if m.selected != 2 {
		t.Fatal("missing selection did not clamp")
	}
	press(m, "home")
	if m.selected != 0 {
		t.Fatal("home")
	}
	press(m, "up")
	if m.selected != 0 {
		t.Fatal("wrapped")
	}
}
func TestFootersAndUnavailableActions(t *testing.T) {
	unavailable := session("broken", false)
	unavailable.ConnectionError = "permission denied"
	m, b := ready(session("running", true), session("stopped", false), session("default", true), unavailable)
	tests := []struct {
		index  int
		want   string
		absent []string
	}{{0, "New [enter]", []string{"Stop", "Delete"}}, {1, "Open [enter]", nil}, {2, "Resume [enter]", []string{"Stop"}}, {3, "Stop [shift+s]", []string{"Delete"}}, {4, "Open [enter]", []string{"Stop", "Delete", "Stopped"}}}
	for _, tt := range tests {
		m.selected = tt.index
		actions := strings.Join(m.footerActions(), " ")
		if !strings.Contains(actions, tt.want) {
			t.Errorf("%d: %s", tt.index, actions)
		}
		for _, bad := range tt.absent {
			if strings.Contains(actions, bad) {
				t.Errorf("unexpected %s", bad)
			}
		}
	}
	for _, index := range []int{0, 3, 4} {
		m.selected = index
		if press(m, "D") != nil {
			t.Fatalf("delete allowed for %d", index)
		}
	}
	m.selected = 2
	if press(m, "S") != nil {
		t.Fatal("stop on stopped")
	}
	if len(b.calls) != 1 {
		t.Fatal(b.calls)
	}
}
func TestFormFocusValidationAndPaste(t *testing.T) {
	m, _ := ready(session("taken", false))
	press(m, "enter")
	if m.view != newSession || !m.input.Focused() || strings.Contains(plain(m), "Refresh [r]") {
		t.Fatal("form initial state")
	}
	if m.validationMessage() != "" {
		t.Fatal("initial empty error")
	}
	press(m, "tab")
	if m.focus != 2 {
		t.Fatal("disabled OK received focus")
	}
	press(m, "shift+tab")
	if m.focus != 0 {
		t.Fatal("reverse focus")
	}
	for _, key := range []string{"q", "r", "S", "D"} {
		press(m, key)
	}
	if m.input.Value() != "qrSD" {
		t.Fatal(m.input.Value())
	}
	for _, text := range []string{"ab\tcd", "bad\nname", strings.Repeat("x", 4097)} {
		m.Update(tea.PasteMsg{Content: text})
		if m.input.Value() != "qrSD" {
			t.Fatal("unsafe paste changed name")
		}
	}
	m.Update(tea.PasteMsg{Content: " "})
	if !strings.Contains(m.validationMessage(), "Spaces") {
		t.Fatal(m.validationMessage())
	}
	m.input.SetValue(strings.Repeat("x", 65))
	if !strings.Contains(m.validationMessage(), "64") {
		t.Fatal("overlong name")
	}
	m.input.SetValue("taken")
	if m.validForm() {
		t.Fatal("duplicate")
	}
	m.input.SetValue("Good.Name")
	m.formError = ""
	press(m, "tab")
	if m.focus != 1 {
		t.Fatal("valid OK skipped")
	}
	press(m, "esc")
	if m.view != picker || m.selected != 0 {
		t.Fatal("cancel state")
	}
	press(m, "enter")
	press(m, "enter")
	if !strings.Contains(m.validationMessage(), "Enter") {
		t.Fatal("empty submit")
	}
}
func TestCreateFreshCheckFailureAndRecovery(t *testing.T) {
	m, b := ready()
	press(m, "enter")
	m.input.SetValue("new")
	b.sessions = []herdr.Session{session("new", false)}
	cmd := press(m, "enter")
	if press(m, "enter") != nil {
		t.Fatal("duplicate submission")
	}
	if strings.Contains(plain(m), "[enter]") {
		t.Fatal("busy form footer")
	}
	run(m, cmd)
	if m.Launch != nil || !strings.Contains(m.validationMessage(), "already exists") || !m.input.Focused() {
		t.Fatal("late duplicate")
	}
	m.input.SetValue("another")
	m.formError = ""
	b.replies = []listReply{{err: errors.New("offline")}}
	run(m, press(m, "enter"))
	if m.input.Value() != "another" || m.formError != "offline" {
		t.Fatal("failed check lost input")
	}
	run(m, press(m, "enter"))
	if m.Launch == nil || m.Launch.Name != "another" || !m.Launch.Create {
		t.Fatal("fresh check did not launch")
	}
	m.LaunchFailed(errors.New("missing executable"))
	if m.Launch != nil || !m.input.Focused() || !strings.Contains(m.formError, "missing executable") {
		t.Fatal("start recovery")
	}
}
func TestStaleResultAndBusyInput(t *testing.T) {
	m, b := ready(session("one", true))
	m.selected = 1
	cmd := press(m, "r")
	original := m.operationID
	m.Update(result{id: original - 1, listed: true, sessions: nil})
	if len(m.sessions) != 1 || m.busy == "" {
		t.Fatal("accepted stale result")
	}
	if press(m, "q") != nil || press(m, "D") != nil {
		t.Fatal("busy keys accepted")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	run(m, cmd)
	b.replies = []listReply{{err: errors.New("offline")}}
	run(m, press(m, "r"))
	if !m.stale || press(m, "enter") != nil || press(m, "S") != nil {
		t.Fatal("stale action allowed")
	}
	run(m, press(m, "r"))
	if m.stale {
		t.Fatal("refresh did not recover")
	}
}
func TestOpenMissingAndDiscoveryError(t *testing.T) {
	m, b := ready(session("gone", false))
	m.selected = 1
	b.sessions = nil
	run(m, press(m, "enter"))
	if m.Launch != nil || !strings.Contains(m.notice, "no longer exists") {
		t.Fatal("missing opened")
	}
	b.replies = []listReply{{err: errors.New("invalid JSON")}}
	m = New(b, time.Now(), true)
	m.width, m.height = 80, 24
	run(m, m.begin(refresh, "", target{}))
	if m.view != discoveryError || !strings.Contains(plain(m), "Retry [r]") {
		t.Fatal("error is not retryable")
	}
	run(m, press(m, "r"))
	if m.view != picker {
		t.Fatal("retry")
	}
}
func TestConfirmationCancellationAndIdentity(t *testing.T) {
	m, b := ready(session("a", true), session("b", false))
	m.selected = 1
	run(m, press(m, "D"))
	if m.view != confirm || m.confirmAccept {
		t.Fatal("cancel not default")
	}
	press(m, "enter")
	if m.view != picker || len(b.calls) != 2 {
		t.Fatal("cancel mutated")
	}
	run(m, press(m, "D"))
	press(m, "right")
	b.sessions = []herdr.Session{session("b", false), session("a", true)}
	run(m, press(m, "enter"))
	if !slices.Contains(b.calls, "delete:a") || slices.Contains(b.calls, "delete:b") {
		t.Fatal(b.calls)
	}
	if m.selected != 1 || m.sessions[0].Name != "b" {
		t.Fatal("deletion selection")
	}
}
func TestLifecycleSequencing(t *testing.T) {
	fail := errors.New("failed")
	tests := []struct {
		name                     string
		initial                  herdr.Session
		confirmed                target
		replies                  []listReply
		stopErr, deleteErr       error
		wantCalls                []string
		wantPartial, wantConfirm bool
		notice                   string
	}{
		{name: "running delete", initial: session("x", true), confirmed: target{Name: "x", Delete: true, Running: true}, wantCalls: []string{"list", "stop:x", "list", "delete:x", "list"}},
		{name: "stop fails", initial: session("x", true), confirmed: target{Name: "x", Delete: true, Running: true}, stopErr: fail, wantCalls: []string{"list", "stop:x", "list"}},
		{name: "delete fails after stop", initial: session("x", true), confirmed: target{Name: "x", Delete: true, Running: true}, deleteErr: fail, wantPartial: true, wantCalls: []string{"list", "stop:x", "list", "delete:x", "list"}},
		{name: "stopped delete", initial: session("x", false), confirmed: target{Name: "x", Delete: true}, wantCalls: []string{"list", "delete:x", "list"}},
		{name: "now running", initial: session("x", true), confirmed: target{Name: "x", Delete: true}, wantConfirm: true, wantCalls: []string{"list"}},
		{name: "default", initial: session("default", true), confirmed: target{Name: "default", Delete: true, Running: true}, wantCalls: []string{"list"}},
		{name: "already stopped", initial: session("x", false), confirmed: target{Name: "x", Running: true}, wantCalls: []string{"list", "list"}, notice: "already stopped"},
		{name: "restarted", initial: session("x", true), confirmed: target{Name: "x", Delete: true, Running: true}, replies: []listReply{{sessions: []herdr.Session{session("x", true)}}, {sessions: []herdr.Session{session("x", true)}}}, wantPartial: true, wantCalls: []string{"list", "stop:x", "list", "list"}},
		{name: "post-stop list fails", initial: session("x", true), confirmed: target{Name: "x", Delete: true, Running: true}, replies: []listReply{{sessions: []herdr.Session{session("x", true)}}, {err: fail}}, wantPartial: true, wantCalls: []string{"list", "stop:x", "list", "list"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBackend{sessions: []herdr.Session{tt.initial}, replies: tt.replies, stopErr: tt.stopErr, deleteErr: tt.deleteErr}
			r := operation(b, 1, mutate, tt.confirmed.Name, tt.confirmed)().(result)
			if !slices.Equal(b.calls, tt.wantCalls) {
				t.Errorf("calls %v want %v", b.calls, tt.wantCalls)
			}
			if r.stoppedButNotDeleted != tt.wantPartial || (r.confirmation != nil) != tt.wantConfirm {
				t.Errorf("result %+v", r)
			}
			if tt.notice != "" && !strings.Contains(r.notice, tt.notice) {
				t.Error(r.notice)
			}
		})
	}
}
func TestResizeScrollAndRenderBounds(t *testing.T) {
	var sessions []herdr.Session
	for i := range 50 {
		sessions = append(sessions, session(fmt.Sprintf("%02d-%s", i, strings.Repeat("x", 60)), i%2 == 0))
	}
	sessions[40] = session("default", true)
	m, _ := ready(sessions...)
	for _, size := range [][2]int{{120, 48}, {80, 24}, {44, 18}, {32, 12}, {10, 3}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertBounds(t, m, size[0], size[1])
		if m.selected != 41 {
			t.Fatal("resize lost identity")
		}
		if !m.tooSmall() && (m.selected < m.scroll || m.selected >= m.scroll+m.listCapacity(m.layout())) {
			t.Fatal("selected invisible")
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	if strings.Contains(plain(m), "⣀") {
		t.Fatal("logo did not collapse")
	}
	if !strings.Contains(plain(m), "Running") || !strings.Contains(plain(m), "10s ago") {
		t.Fatal("metadata lost")
	}
	press(m, "home")
	press(m, "enter")
	m.input.SetValue(strings.Repeat("a", 64))
	for _, size := range [][2]int{{100, 40}, {44, 22}, {32, 22}, {4, 2}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertBounds(t, m, size[0], size[1])
		if m.input.Value() != strings.Repeat("a", 64) {
			t.Fatal("input lost")
		}
	}
}
func assertBounds(t *testing.T, m *Model, width, height int) {
	t.Helper()
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) != height {
		t.Fatalf("height %d want %d", len(lines), height)
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w != width {
			t.Fatalf("line width %d want %d: %q", w, width, ansi.Strip(line))
		}
	}
}
func TestFooterCenterAndHiddenForm(t *testing.T) {
	m, _ := ready(session("work", true))
	m.selected = 1
	for _, width := range []int{100, 60, 40, 32} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		lines := strings.Split(plain(m), "\n")
		for _, line := range lines[len(lines)-len(m.layout().footer):] {
			left := len(line) - len(strings.TrimLeft(line, " "))
			right := len(line) - len(strings.TrimRight(line, " "))
			if abs(left-right) > 1 {
				t.Errorf("not centered: %q", line)
			}
		}
	}
	press(m, "home")
	press(m, "enter")
	m.input.SetValue("bad name")
	if strings.Contains(plain(m), "Refresh") || strings.Contains(plain(m), "New [enter]") {
		t.Fatal("form footer")
	}
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func TestLongConfirmationAndEscapedDiagnostics(t *testing.T) {
	m, _ := ready(session(strings.Repeat("x", 64), true))
	m.selected = 1
	run(m, press(m, "D"))
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 15})
	assertBounds(t, m, 40, 15)
	if !strings.Contains(plain(m), "Cancel") || !strings.Contains(plain(m), "Stop and delete") {
		t.Fatal("buttons not visible")
	}
	before := plain(m)
	press(m, "pgdown")
	if plain(m) == before {
		t.Fatal("confirmation did not scroll")
	}
	m.view = picker
	m.notice = "attack\x1b[2J\x07\u202e"
	m.contentScroll = 0
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if strings.Contains(plain(m), "\x07") || !strings.Contains(plain(m), "\\u001b") {
		t.Fatal("unsafe diagnostic")
	}
}

func TestValidationKeepsSmallFormEditable(t *testing.T) {
	m, _ := ready()
	m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	press(m, "enter")
	if m.tooSmall() {
		t.Fatal("empty form should fit")
	}
	buttonRow := func() int {
		for i, line := range strings.Split(plain(m), "\n") {
			if strings.Contains(line, "Cancel") {
				return i
			}
		}
		return -1
	}
	before := buttonRow()
	press(m, "!")
	if m.tooSmall() || !strings.Contains(plain(m), "Use only letters") {
		t.Fatal("validation hid the editable form")
	}
	if buttonRow() != before {
		t.Fatal("validation shifted buttons")
	}
	assertBounds(t, m, 44, 18)
	press(m, "backspace")
	if m.input.Value() != "" || m.tooSmall() {
		t.Fatal("cannot correct invalid input")
	}
	press(m, "!")
	press(m, "esc")
	if m.view != picker {
		t.Fatal("cannot cancel invalid form")
	}
}

func TestSmallPickerPreservesDiagnosticsAndActions(t *testing.T) {
	m, _ := ready(session("a", true), session("b", false))
	m.selected = 1
	m.notice = "Action failed: " + strings.Repeat("failure detail ", 20) + "END"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 9})
	if m.tooSmall() {
		t.Fatal("one row, indicator and diagnostic should fit")
	}
	view := plain(m)
	if !strings.Contains(view, "Action failed:") || !strings.Contains(view, "of 3") {
		t.Fatal("notice or position omitted")
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "Open [enter]") {
		t.Fatal("footer omitted")
	}
	assertBounds(t, m, 80, 9)
	for range 10 {
		press(m, "pgdown")
	}
	if !strings.Contains(plain(m), "END") {
		t.Fatal("full diagnostic is unreachable")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 7})
	if !m.tooSmall() || !strings.Contains(plain(m), "Resize terminal or quit") {
		t.Fatal("undersized picker should offer resize or quit")
	}
	assertBounds(t, m, 80, 7)
	if m.notice == "" || m.selected != 1 {
		t.Fatal("small fallback lost state")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 9})
	if m.tooSmall() || !strings.Contains(plain(m), "END") {
		t.Fatal("resize did not restore diagnostic")
	}
}

func TestDiscoveryFailureExitStatusAndRecovery(t *testing.T) {
	b := &fakeBackend{replies: []listReply{{err: errors.New("startup discovery failed")}}}
	m := New(b, time.Now(), true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	run(m, m.begin(refresh, "", target{}))
	if m.view != discoveryError || m.ExitCode != 1 || press(m, "q") == nil {
		t.Fatal("startup discovery failure should quit unsuccessfully")
	}
	run(m, press(m, "r"))
	if m.view != picker || m.ExitCode != 0 {
		t.Fatal("successful discovery did not clear startup failure")
	}
	b.replies = []listReply{{err: errors.New("later refresh failed")}}
	run(m, press(m, "r"))
	if m.ExitCode != 0 || press(m, "q") == nil {
		t.Fatal("recoverable refresh failure changed normal quit status")
	}
}

func TestEveryDiagnosticLineReachable(t *testing.T) {
	for _, view := range []string{"picker", "form"} {
		for _, capacity := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("%s/%d-rows", view, capacity), func(t *testing.T) {
				m, _ := ready(session("a", true), session("b", false))
				width, height := 80, 8+capacity
				if view == "form" {
					press(m, "enter")
					width, height = 44, 16+capacity
				}
				m.Update(tea.WindowSizeMsg{Width: width, Height: height})
				var tokens []string
				for i := range 7 {
					tokens = append(tokens, fmt.Sprintf("LINE%d-", i)+strings.Repeat("x", m.layout().bodyWidth-10))
				}
				diagnostic := strings.Join(tokens, " ")
				if view == "picker" {
					m.notice = diagnostic
				} else {
					m.formError = diagnostic
				}
				if m.tooSmall() {
					t.Fatal("diagnostic viewport should fit")
				}
				if lines := wrap(diagnostic, m.layout().bodyWidth); len(lines) != len(tokens) {
					t.Fatalf("fixture wrapped into %d lines", len(lines))
				}
				for _, key := range []string{"pgdown", "pgup"} {
					seen := make(map[string]bool)
					for range len(tokens) + 1 {
						rendered := plain(m)
						for _, token := range tokens {
							if strings.Contains(rendered, token) {
								seen[token] = true
							}
						}
						press(m, key)
					}
					for _, token := range tokens {
						if !seen[token] {
							t.Errorf("%s skipped diagnostic %s", key, token)
						}
					}
				}
			})
		}
	}
}
