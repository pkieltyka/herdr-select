package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
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
	r := cmd().(result)
	r.listedAt = time.Unix(110, 0)
	m.Update(r)
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

func TestAgeSnapshotChangesOnlyWithSuccessfulReload(t *testing.T) {
	m, b := ready(session("work", true))
	assertAge := func(want string) {
		t.Helper()
		if _, age := m.metadata(m.sessions[0]); age != want {
			t.Fatalf("age = %q, want %q", age, want)
		}
	}
	assertAge("10s ago")
	if cmd := m.Init(); cmd != nil {
		t.Fatal("idle picker scheduled background work")
	}
	for _, msg := range []tea.Msg{time.Unix(500, 0), tea.WindowSizeMsg{Width: 80, Height: 28}} {
		if _, cmd := m.Update(msg); cmd != nil {
			t.Fatal("idle update scheduled background work")
		}
		assertAge("10s ago")
	}
	press(m, "down")
	assertAge("10s ago")
	r := press(m, "r")().(result)
	r.listedAt = time.Unix(170, 0)
	m.Update(r)
	assertAge("1min ago")
	if m.selected != 1 || !m.sessions[0].ModifiedAt.Equal(time.Unix(100, 0)) {
		t.Fatal("refresh changed selection or underlying timestamp")
	}
	b.replies = []listReply{{err: errors.New("offline")}}
	run(m, press(m, "r"))
	assertAge("1min ago")
	stale := r
	stale.id--
	stale.listedAt = time.Unix(900, 0)
	m.Update(stale)
	assertAge("1min ago")

	// A lifecycle reload renews status and the age snapshot together.
	r = m.begin(mutate, "work", target{Name: "work", Running: true})().(result)
	r.listedAt = time.Unix(230, 0)
	m.Update(r)
	assertAge("2min ago")
	if m.sessions[0].Running || m.stale {
		t.Fatal("lifecycle reload did not restore fresh stopped state")
	}
}

func TestInitialLoadCapturesSnapshotWithoutTimer(t *testing.T) {
	b := &fakeBackend{sessions: []herdr.Session{session("work", true)}}
	m := New(b, time.Unix(110, 0), true)
	before := time.Now()
	msg := m.Init()()
	r, ok := msg.(result)
	if !ok {
		t.Fatalf("initialization scheduled %T instead of only loading sessions", msg)
	}
	if r.listedAt.Before(before) || r.listedAt.After(time.Now()) {
		t.Fatal("snapshot was not captured when discovery completed")
	}
	if _, cmd := m.Update(r); cmd != nil {
		t.Fatal("successful load scheduled background work")
	}
	if !m.now.Equal(r.listedAt) {
		t.Fatal("initial load did not adopt snapshot")
	}
}

func TestFormDividerAndCompactButtonSpacing(t *testing.T) {
	for _, size := range [][2]int{{100, 40}, {80, 28}, {81, 29}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ := ready()
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			press(m, "enter")
			if m.tooSmall() {
				t.Fatal("form should fit")
			}
			l := m.layout()
			lines := m.formBody(l)
			if ansi.Strip(lines[1]) != strings.Repeat("─", l.bodyWidth) {
				t.Fatal("title divider does not span the form interior")
			}
			buttons := ansi.Strip(lines[len(lines)-1])
			if !strings.Contains(buttons, "OK") || !strings.Contains(buttons, "Cancel") {
				t.Fatal("missing buttons")
			}
			left := len(buttons) - len(strings.TrimLeft(buttons, " "))
			right := len(buttons) - len(strings.TrimRight(buttons, " "))
			if abs(left-right) > 1 {
				t.Fatal("buttons are not centered")
			}
			blankRows := 0
			for i := len(lines) - 2; i >= 0 && strings.TrimSpace(ansi.Strip(lines[i])) == ""; i-- {
				blankRows++
			}
			if blankRows < 1 || blankRows > 2 {
				t.Fatalf("reserved %d blank rows above buttons, want 1–2", blankRows)
			}
			if !strings.Contains(strings.Join(strings.Fields(plain(m)), " "), "no spaces") {
				t.Fatal("naming hint missing")
			}
			assertBounds(t, m, size[0], size[1])
		})
	}
}

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
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
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
	m.width, m.height = 80, 28
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
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	if len(m.layout().logo) != 16 {
		t.Fatal("long list collapsed the logo")
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
	for _, width := range []int{100, 90, 81, 80} {
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
func TestFooterBackgroundSpansTerminal(t *testing.T) {
	m, _ := ready(session("work", true))
	m.selected = 1
	actions := []string{"Open [enter]", "Stop [shift+s]", "Delete [shift+d]", "Refresh [r]"}
	strip := color.RGBA{R: 0x17, G: 0x17, B: 0x17, A: 0xff}
	badge := color.RGBA{R: 0x29, G: 0x29, B: 0x33, A: 0xff}
	for _, width := range []int{80, 90, 100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
			lines := strings.Split(m.View().Content, "\n")
			for _, line := range lines[len(lines)-len(m.layout().footer):] {
				plain := ansi.Strip(line)
				cells := uv.NewStyledString(line).Lines(ansi.GraphemeWidth)[0]
				if len(cells) != width {
					t.Fatalf("footer width %d, want %d", len(cells), width)
				}
				want := make([]color.RGBA, width)
				for x := range want {
					want[x] = strip
				}
				for _, action := range actions {
					label := " " + action + " "
					if start := strings.Index(plain, label); start >= 0 {
						for x := start; x < start+len(label); x++ {
							want[x] = badge
						}
					}
				}
				for x, cell := range cells {
					if cell.Style.Bg == nil || color.RGBAModel.Convert(cell.Style.Bg) != want[x] {
						t.Errorf("footer column %d (%q): background %v, want %v", x, cell.Content, cell.Style.Bg, want[x])
					}
				}
			}
		})
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
	m.notice = strings.Repeat("Long diagnostic detail. ", 100)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	assertBounds(t, m, 80, 28)
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

func TestValidationKeepsMinimumSizeFormEditable(t *testing.T) {
	m, _ := ready()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
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
	assertBounds(t, m, 80, 28)
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

func TestMinimumSizePickerPreservesDiagnosticsAndActions(t *testing.T) {
	m, _ := ready(session("a", true), session("b", false))
	m.selected = 1
	for i := range 30 {
		m.sessions = append(m.sessions, session(fmt.Sprintf("session-%d", i), false))
	}
	m.notice = "Action failed: " + strings.Repeat("failure detail ", 20) + "END"
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	if m.tooSmall() {
		t.Fatal("picker and diagnostic should fit at minimum size")
	}
	view := plain(m)
	if !strings.Contains(view, "Action failed:") || !strings.Contains(view, "of 33") {
		t.Fatal("notice or position omitted")
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "Open [enter]") {
		t.Fatal("footer omitted")
	}
	assertBounds(t, m, 80, 28)
	for range 10 {
		press(m, "pgdown")
	}
	if !strings.Contains(plain(m), "END") {
		t.Fatal("full diagnostic is unreachable")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 7})
	if !m.tooSmall() || !strings.Contains(plain(m), "Terminal too small") {
		t.Fatal("undersized picker should offer resize or quit")
	}
	assertBounds(t, m, 80, 7)
	if m.notice == "" || m.selected != 1 {
		t.Fatal("small fallback lost state")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	if m.tooSmall() || !strings.Contains(plain(m), "END") {
		t.Fatal("resize did not restore diagnostic")
	}
}

func TestDiscoveryFailureExitStatusAndRecovery(t *testing.T) {
	b := &fakeBackend{replies: []listReply{{err: errors.New("startup discovery failed")}}}
	m := New(b, time.Now(), true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
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
	for _, view := range []viewKind{picker, newSession, confirm, startupError, discoveryError} {
		heights := []int{28, 29, 30, 31, 40}
		for _, height := range heights {
			t.Run(fmt.Sprintf("%d/%d-high", view, height), func(t *testing.T) {
				m, _ := ready(session("a", true), session("b", false))
				width := 80
				if view == newSession {
					press(m, "enter")
				}
				m.view = view
				m.confirmation = target{Name: strings.Repeat("x", 64), Running: true, Delete: true}
				m.Update(tea.WindowSizeMsg{Width: width, Height: height})
				var tokens []string
				for i := range 7 {
					tokens = append(tokens, fmt.Sprintf("LINE%d-", i)+strings.Repeat("x", m.layout().bodyWidth-10))
				}
				diagnostic := strings.Join(tokens, " ")
				if view == newSession {
					m.formError = diagnostic
				} else {
					m.notice = diagnostic
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

func TestFixedMinimumAcrossViews(t *testing.T) {
	for _, view := range []viewKind{loading, picker, newSession, confirm, startupError, discoveryError} {
		for _, size := range [][2]int{{79, 28}, {80, 27}, {79, 27}, {80, 28}, {81, 28}, {80, 29}} {
			t.Run(fmt.Sprintf("%d/%dx%d", view, size[0], size[1]), func(t *testing.T) {
				m, _ := ready(session("work", true))
				m.view = view
				m.notice = "underlying diagnostic"
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				wantSmall := size[0] < 80 || size[1] < 28
				if m.tooSmall() != wantSmall {
					t.Fatal("threshold depends on view")
				}
				assertBounds(t, m, size[0], size[1])
				rendered := plain(m)
				if wantSmall {
					for _, want := range []string{"Terminal too small", fmt.Sprintf("Current: %d × %d", size[0], size[1]), "Minimum: 80 × 28", "Resize to continue"} {
						if !strings.Contains(rendered, want) {
							t.Fatalf("missing %q", want)
						}
					}
					for _, hidden := range []string{"herdr", "underlying diagnostic", "[enter]", "Session name", "Cancel"} {
						if strings.Contains(rendered, hidden) {
							t.Fatalf("hidden view leaked %q", hidden)
						}
					}
				} else if strings.Contains(rendered, "Terminal too small") || len(m.layout().logo) != 16 {
					t.Fatal("minimum-sized normal view did not retain full logo")
				}
			})
		}
	}
}

func TestResizeMessageCenteringAndTinyBounds(t *testing.T) {
	m, _ := ready()
	m.Update(tea.WindowSizeMsg{Width: 68, Height: 28})
	lines := strings.Split(plain(m), "\n")
	for index, want := range []string{"Terminal too small", "", "Current: 68 × 28", "Minimum: 80 × 28", "", "Resize to continue"} {
		line := lines[11+index]
		if strings.TrimSpace(line) != want {
			t.Fatalf("message row %d = %q", index, line)
		}
		left := ansi.StringWidth(line) - ansi.StringWidth(strings.TrimLeft(line, " "))
		right := ansi.StringWidth(line) - ansi.StringWidth(strings.TrimRight(line, " "))
		if abs(left-right) > 1 {
			t.Fatal("message not centered")
		}
	}
	for _, size := range [][2]int{{79, 1}, {20, 4}, {18, 2}, {6, 2}, {1, 1}, {0, 0}, {0, 10}, {10, 0}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if size[0] == 0 || size[1] == 0 {
			if m.View().Content != "" {
				t.Fatal("zero-sized view should be empty")
			}
			continue
		}
		assertBounds(t, m, size[0], size[1])
		if strings.TrimSpace(plain(m)) == "" {
			t.Fatal("tiny view lost resize indication")
		}
	}
}

func TestUndersizedInputGuardAndRecovery(t *testing.T) {
	for _, view := range []viewKind{picker, newSession, confirm, discoveryError, startupError} {
		t.Run(fmt.Sprint(view), func(t *testing.T) {
			m, b := ready(session("work", true))
			press(m, "enter")
			m.input.SetValue("saved-name")
			m.input.CursorStart()
			m.view, m.selected, m.focus = view, 1, 0
			m.confirmation = target{Name: "work", Delete: true, Running: true}
			m.confirmAccept = true
			m.contentScroll = 2
			m.formError = "saved error"
			scroll, calls := m.scroll, len(b.calls)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 27})
			for _, key := range []string{"q", "r", "S", "D", "x", "backspace", "enter", "home", "up", "down", "right", "tab", "shift+tab", "pgdown", "pgup"} {
				if cmd := press(m, key); cmd != nil {
					t.Fatalf("hidden key %q scheduled action", key)
				}
			}
			for _, text := range []string{"pasted", "bad\ntext", strings.Repeat("x", 4097)} {
				m.Update(tea.PasteMsg{Content: text})
			}
			if m.view != view || m.selected != 1 || m.scroll != scroll || m.contentScroll != 2 || m.focus != 0 || !m.confirmAccept || m.input.Value() != "saved-name" || m.input.Position() != 0 || m.formError != "saved error" || len(b.calls) != calls {
				t.Fatal("undersized input or resize changed hidden state")
			}
			for _, key := range []string{"esc", "ctrl+c"} {
				cmd := press(m, key)
				if cmd == nil {
					t.Fatalf("%s cannot quit undersized view", key)
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("quit key performed a hidden action")
				}
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
			if m.view != view || m.input.Value() != "saved-name" || m.selected != 1 || m.confirmation.Name != "work" {
				t.Fatal("resize recovery lost state")
			}
			if view == newSession {
				press(m, "x")
				if m.input.Value() != "xsaved-name" {
					t.Fatal("form cursor/focus did not recover")
				}
			}
		})
	}
}

func TestUndersizedPendingMutationResults(t *testing.T) {
	for _, needsConfirmation := range []bool{false, true} {
		t.Run(fmt.Sprint(needsConfirmation), func(t *testing.T) {
			m, b := ready(session("work", !needsConfirmation))
			m.selected = 1
			run(m, press(m, "D"))
			press(m, "right")
			cmd := press(m, "enter")
			m.Update(tea.WindowSizeMsg{Width: 68, Height: 28})
			id := m.operationID
			for _, key := range []string{"esc", "ctrl+c", "q", "enter", "D"} {
				if press(m, key) != nil || m.operationID != id || m.busy == "" {
					t.Fatal("pending mutation abandoned or repeated")
				}
			}
			if needsConfirmation {
				b.sessions[0].Running = true
			}
			r := cmd().(result)
			r.listedAt = time.Unix(250, 0)
			m.Update(r)
			if m.busy != "" || !m.now.Equal(r.listedAt) {
				t.Fatal("pending result not processed while undersized")
			}
			if press(m, "enter") != nil {
				t.Fatal("result enabled blind second action")
			}
			if needsConfirmation {
				if m.view != confirm || m.confirmAccept || slices.Contains(b.calls, "stop:work") || slices.Contains(b.calls, "delete:work") {
					t.Fatal("new running state bypassed confirmation")
				}
			} else if len(m.sessions) != 0 || !slices.Contains(b.calls, "delete:work") {
				t.Fatal("pending deletion did not complete")
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
			if needsConfirmation && !strings.Contains(plain(m), "Stop and delete") {
				t.Fatal("reconfirmation not restored")
			}
		})
	}
}

func TestNoticeExpiryIdentityAndAsyncGuards(t *testing.T) {
	m, _ := ready(session("work", true))
	m.selected = 1
	run(m, press(m, "S"))
	press(m, "right")
	r := press(m, "enter")().(result)
	_, timer := m.Update(r)
	if timer == nil || m.noticeSeverity != noticeWarning || !strings.Contains(m.notice, "Stopped session") {
		t.Fatal("successful action did not schedule a transient notice")
	}
	oldID := m.noticeID
	m.View()
	m.Update(time.Now())
	m.Update(tea.WindowSizeMsg{Width: 68, Height: 28})
	if m.noticeID != oldID {
		t.Fatal("render or resize restarted notice lifetime")
	}
	pending := m.begin(refresh, "", target{})
	m.Update(noticeExpired{id: oldID})
	if m.notice != "" || m.busy == "" {
		t.Fatal("expiry blocked by undersized/pending state or abandoned operation")
	}
	run(m, pending)
	if m.setNotice("new warning", noticeWarning) == nil {
		t.Fatal("warning did not schedule expiry")
	}
	newID := m.noticeID
	m.Update(noticeExpired{id: oldID})
	if m.notice != "new warning" {
		t.Fatal("old timer cleared newer warning")
	}
	m.setNotice("persistent error", noticeError)
	m.Update(noticeExpired{id: newID})
	m.Update(noticeExpired{id: m.noticeID})
	if m.notice != "persistent error" {
		t.Fatal("expiry cleared persistent error")
	}
}

func TestErrorNoticesPersistAndUseRed(t *testing.T) {
	for _, failure := range []string{"mutation", "partial", "refresh", "missing-start", "discovery"} {
		t.Run(failure, func(t *testing.T) {
			m, b := ready(session("work", true))
			m.selected = 1
			var cmd tea.Cmd
			switch failure {
			case "mutation", "partial":
				run(m, press(m, "D"))
				press(m, "right")
				if failure == "mutation" {
					b.stopErr = errors.New("uncertain stop failure")
				} else {
					b.deleteErr = errors.New("delete failed")
				}
				cmd = press(m, "enter")
			case "refresh", "discovery":
				b.replies = []listReply{{err: errors.New("offline")}}
				if failure == "discovery" {
					m.loaded = false
				}
				cmd = m.begin(refresh, "", target{})
			case "missing-start":
				m.LaunchFailed(errors.New("missing executable"))
			}
			if cmd != nil {
				if _, timer := m.Update(cmd()); timer != nil {
					t.Fatal("error scheduled expiration")
				}
			}
			before := m.notice
			m.Update(noticeExpired{id: m.noticeID})
			if before == "" || m.notice != before || m.noticeSeverity != noticeError {
				t.Fatal("error was transient")
			}
			red := color.RGBA{R: 0xe0, G: 0x6c, B: 0x75, A: 0xff}
			found := false
			for _, line := range uv.NewStyledString(m.View().Content).Lines(ansi.GraphemeWidth) {
				for _, cell := range line {
					if cell.Style.Fg != nil && color.RGBAModel.Convert(cell.Style.Fg) == red {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("error has no red text")
			}
		})
	}
}

func TestPendingCreateResultAndNoticeExpiryPreserveHiddenForm(t *testing.T) {
	m, b := ready()
	m.setNotice("Earlier action completed.", noticeWarning)
	oldID := m.noticeID
	press(m, "enter")
	m.input.SetValue("new-name")
	b.replies = []listReply{{err: errors.New("duplicate check failed")}}
	cmd := press(m, "enter")
	m.Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	run(m, cmd)
	m.contentScroll = 1
	m.Update(noticeExpired{id: oldID})
	if m.busy != "" || m.formError != "duplicate check failed" || m.input.Value() != "new-name" || !m.input.Focused() || m.contentScroll != 1 {
		t.Fatal("pending result/old notice expiry lost form state")
	}
	if press(m, "enter") != nil || m.Launch != nil {
		t.Fatal("blind create retry was allowed")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	if !strings.Contains(plain(m), "duplicate check failed") {
		t.Fatal("failed check was not restored")
	}
	run(m, press(m, "enter"))
	if m.Launch == nil || m.Launch.Name != "new-name" {
		t.Fatal("visible retry failed after recovery")
	}
}

func TestResizeRecoveryRestartsHiddenFormCursor(t *testing.T) {
	m, _ := ready()
	styles := m.input.Styles()
	styles.Cursor.BlinkSpeed = time.Nanosecond
	m.input.SetStyles(styles)
	blink := press(m, "enter")
	m.input.SetValue("saved-name")
	m.input.SetCursor(3)
	visible := m.input.View()
	_, blink = m.Update(blink())
	hidden := m.input.View()
	if visible == hidden || blink == nil {
		t.Fatal("fixture did not enter the hidden cursor phase")
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 81, Height: 28}); cmd != nil || m.input.View() != hidden {
		t.Fatal("normal resize restarted cursor blinking")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 27})
	if _, cmd := m.Update(blink()); cmd != nil || m.input.View() != hidden {
		t.Fatal("hidden form processed cursor input")
	}
	_, blink = m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	if blink == nil || m.input.View() != visible {
		t.Fatal("resize recovery did not restore the cursor and schedule blinking")
	}
	if m.input.Value() != "saved-name" || m.input.Position() != 3 || !m.input.Focused() || m.focus != 0 {
		t.Fatal("resize recovery changed input state")
	}
	_, blink = m.Update(blink())
	if m.input.View() != hidden || blink == nil {
		t.Fatal("restored cursor did not continue blinking")
	}
	m.Update(blink())
	if m.input.View() != visible {
		t.Fatal("restored cursor did not reappear")
	}
}

func TestResizeRecoveryPreservesFormButtonFocusAndBusyState(t *testing.T) {
	for _, state := range []string{"OK", "Cancel", "submitting"} {
		t.Run(state, func(t *testing.T) {
			m, _ := ready()
			press(m, "enter")
			m.input.SetValue("saved-name")
			m.input.SetCursor(3)
			if state == "submitting" {
				press(m, "enter")
			} else {
				press(m, "tab")
				if state == "Cancel" {
					press(m, "tab")
				}
			}
			focus, busy := m.focus, m.busy
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 27})
			_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
			if cmd != nil || m.focus != focus || m.busy != busy || m.input.Focused() || m.input.Position() != 3 || m.input.Value() != "saved-name" {
				t.Fatal("resize recovery stole focus or changed pending input")
			}
		})
	}
}

func TestShortTerminalKeepsFullLogoAndControls(t *testing.T) {
	for _, height := range []int{28, 29, 30, 31, 36, 40} {
		for _, state := range []string{"loading", "picker", "unavailable", "form", "validation", "submitting", "stop", "delete", "stop-delete", "startup", "discovery"} {
			t.Run(fmt.Sprintf("%s/%d", state, height), func(t *testing.T) {
				m, _ := ready(session("work", true))
				var want []string
				switch state {
				case "loading":
					m.view = loading
					want = []string{"Loading sessions…"}
				case "picker", "unavailable":
					m.selected = 1
					want = []string{"New Session", "work", "10s", "Open [enter]", "Refresh [r]"}
					if state == "unavailable" {
						m.sessions[0].ConnectionError = "connection failed"
						want = append(want, "Unavailable: connection failed")
					} else {
						want = append(want, "Stop [shift+s]", "Delete [shift+d]")
					}
				case "form", "validation", "submitting":
					press(m, "enter")
					m.input.SetValue("new-name")
					want = []string{"New Session", "Session name", "new-name", "1-64 characters, no spaces", "OK", "Cancel"}
					if state == "validation" {
						m.formError = "Creation failed"
						want = append(want, "Creation failed")
					} else if state == "submitting" {
						press(m, "enter")
						want = append(want, "Checking session name…")
					}
				case "stop", "delete", "stop-delete":
					m.view = confirm
					m.confirmation = target{Name: "work", Running: state != "delete", Delete: state != "stop"}
					want = []string{m.confirmTitle(), "work", "Cancel", "Confirm [enter]"}
					if state != "delete" {
						want = append(want, "Running work will be interrupted.")
					}
					if state != "stop" {
						want = append(want, "The saved session will be removed.")
					}
				case "startup", "discovery":
					m.view = startupError
					want = []string{"Herdr Select", "Cannot connect", "Quit [q]"}
					if state == "discovery" {
						m.view = discoveryError
						want = []string{"Unable to load sessions", "Cannot connect", "Retry [r]", "Quit [q]"}
					}
					m.notice = "Cannot connect"
				}
				m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
				assertBounds(t, m, 80, height)
				rendered := plain(m)
				for _, text := range want {
					if !strings.Contains(rendered, text) {
						t.Fatalf("required content clipped: %q\n%s", text, rendered)
					}
				}
				lines := strings.Split(rendered, "\n")
				logo := strings.Split(strings.TrimRight(ram, "\n"), "\n")
				start := 0
				for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
					start++
				}
				for i, line := range logo {
					if start+i >= len(lines) || strings.TrimSpace(lines[start+i]) != strings.TrimSpace(line) {
						t.Fatalf("full logo row %d missing or clipped", i)
					}
				}
				if m.view == newSession {
					if strings.TrimSpace(lines[len(lines)-1]) != "" || strings.Contains(rendered, "[enter]") {
						t.Fatal("form footer reservation lost")
					}
					wantGap := min(2, height-28)
					// The panel's own top padding follows the decorative gap.
					titleRow := start + len(logo) + wantGap + 1
					if !strings.Contains(lines[titleRow], "New Session") {
						t.Fatal("form gap did not contract/expand with available height")
					}
					if height == 28 && (start != 0 || !strings.Contains(lines[25], "Cancel")) {
						t.Fatal("minimum form did not use all available rows")
					}
				} else {
					footer := strings.Join(m.footerLinesFor(m.footerActions()), "\n")
					if footer != "" && strings.TrimSpace(lines[len(lines)-1]) != strings.TrimSpace(footer) {
						t.Fatal("contextual footer clipped or displaced")
					}
				}
			})
		}
	}
}

func TestShortTerminalResizeKeepsSelectedSessionVisible(t *testing.T) {
	m, _ := ready()
	for i := range 40 {
		m.sessions = append(m.sessions, session(fmt.Sprintf("work-%02d", i), false))
	}
	m.setNotice("Completed operation", noticeWarning)
	for range len(m.sessions) {
		press(m, "down")
	}
	snapshot := m.now
	for _, height := range []int{28, 27, 28, 40, 29, 30, 28} {
		m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		if m.selected != len(m.sessions) || !m.now.Equal(snapshot) {
			t.Fatal("resize changed selection or recency snapshot")
		}
		if height < 28 {
			continue
		}
		if !strings.Contains(plain(m), "work-39") || !strings.Contains(plain(m), "Completed operation") {
			t.Fatal("resize lost selected row or notice")
		}
	}
	m.Update(noticeExpired{id: m.noticeID})
	if strings.Contains(plain(m), "Completed operation") || !strings.Contains(plain(m), "work-39") {
		t.Fatal("notice expiry lost selected row at minimum size")
	}
	press(m, "home")
	if !strings.Contains(plain(m), "New Session") || m.selected != 0 {
		t.Fatal("cannot reach top of list at minimum size")
	}
}

func TestSingleRowFormDiagnosticPreservesEveryCharacter(t *testing.T) {
	full := strings.Repeat("x", 67) + "ABC"
	for _, height := range []int{28, 29, 30, 31, 40} {
		for _, diagnostic := range []string{full, full + "\n" + strings.Repeat("y", 67) + "DEF", full + strings.Repeat("z", 67) + "GHI", strings.Repeat("界", 33) + "ABCD" + strings.Repeat("語", 33) + "EFGH"} {
			t.Run(fmt.Sprintf("%d/%d-characters", height, len(diagnostic)), func(t *testing.T) {
				m, _ := ready()
				press(m, "enter")
				m.formError = diagnostic
				m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
				var got strings.Builder
				for page := 0; ; page++ {
					if page > len(diagnostic) {
						t.Fatal("diagnostic paging did not stop")
					}
					lines := strings.Split(plain(m), "\n")
					found := false
					for i, line := range lines {
						if strings.Contains(line, "1-64 characters, no spaces") {
							for _, diagnosticLine := range lines[i+1:] {
								row := strings.TrimSpace(diagnosticLine)
								if strings.Contains(row, "Cancel") {
									break
								}
								if !strings.Contains(row, "↑↓ / pgup pgdown: more") {
									got.WriteString(strings.TrimSuffix(row, " ↕"))
								}
							}
							found = true
							break
						}
					}
					if !found {
						t.Fatal("form diagnostic row is hidden")
					}
					before := m.contentScroll
					press(m, "pgdown")
					if m.contentScroll == before {
						break
					}
				}
				if want := safe(diagnostic); got.String() != want {
					t.Fatalf("paging lost diagnostic characters:\n got %q\nwant %q", got.String(), want)
				}
			})
		}
	}
}

func TestFullWidthDiagnosticTailsAcrossViews(t *testing.T) {
	for _, view := range []viewKind{picker, newSession, confirm, startupError, discoveryError} {
		for _, height := range []int{28, 30, 31, 40} {
			t.Run(fmt.Sprintf("%d/%d", view, height), func(t *testing.T) {
				m, _ := ready()
				press(m, "enter")
				m.view = view
				m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
				diagnostic := strings.Repeat("x", 68) + "AB" + strings.Repeat("y", 68) + "CD" + strings.Repeat("z", 68) + "EF"
				m.notice, m.formError = diagnostic, diagnostic
				for _, key := range []string{"pgdown", "pgup"} {
					seen := ""
					for range 20 {
						seen += plain(m)
						press(m, key)
					}
					for _, tail := range []string{"AB", "CD", "EF"} {
						if !strings.Contains(seen, tail) {
							t.Fatalf("%s lost full-width line tail %q", key, tail)
						}
					}
				}
			})
		}
	}
}
