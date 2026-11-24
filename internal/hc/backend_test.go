package hc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pkieltyka/herdr-select/internal/session"
)

// The test binary is the fake hc: with HC_TEST_FIXTURE set it records its
// invocation and replays a canned response. No test runs the real hc.
type fixture struct {
	Stdout, Stderr, Capture string
	Exit                    int
	Sleep                   bool
}
type invocation struct {
	Args     []string
	Dir      string
	Sessions []string // every HC_SESSION entry the child saw
}

func TestMain(m *testing.M) {
	if path := os.Getenv("HC_TEST_FIXTURE"); path != "" {
		os.Exit(fakeHC(path))
	}
	os.Exit(m.Run())
}

func fakeHC(path string) int {
	var f fixture
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &f) != nil {
		return 90
	}
	dir, _ := os.Getwd()
	var sessions []string
	for _, entry := range os.Environ() {
		if value, ok := strings.CutPrefix(entry, "HC_SESSION="); ok {
			sessions = append(sessions, value)
		}
	}
	if data, _ = json.Marshal(invocation{os.Args[1:], dir, sessions}); os.WriteFile(f.Capture, data, 0o600) != nil {
		return 91
	}
	if f.Sleep {
		time.Sleep(10 * time.Second)
	}
	fmt.Fprint(os.Stdout, f.Stdout)
	fmt.Fprint(os.Stderr, f.Stderr)
	return f.Exit
}

// world is an isolated herdr-core layout: a state root and a runtime folder.
type world struct {
	root  string
	paths Paths
}

func newWorld(t *testing.T) world {
	t.Helper()
	root := shortDir(t)
	return world{root, Paths{Home: filepath.Join(root, "home"), StateRoot: filepath.Join(root, "s"), RuntimeDir: filepath.Join(root, "r"), MaxSocket: 107}}
}

func (w world) session(t *testing.T, name string, files ...string) string {
	t.Helper()
	dir := w.paths.SessionDir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func (w world) runtime(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(w.paths.RuntimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// backend builds a Backend whose hc is the fake, answering with f.
func (w world) backend(t *testing.T, f fixture, env Env, client string) (*Backend, string) {
	t.Helper()
	f.Capture = filepath.Join(w.root, "invocation.json")
	path := filepath.Join(w.root, "fixture.json")
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	environ := []string{"HC_TEST_FIXTURE=" + path, "HC_SESSION=other", "MARKER=kept", "HC_SESSION=other2", "GORACE=atexit_sleep_ms=0",
		"GOCOVERDIR=" + os.Getenv("GOCOVERDIR")} // a -cover test binary warns on stderr without it
	b := New(w.paths, env, environ, executable, client, w.root)
	b.stopTimeout = 2 * time.Second
	return b, f.Capture
}

func readInvocation(t *testing.T, path string) invocation {
	t.Helper()
	var got invocation
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func requireError(t *testing.T, err error, code string, uncertain bool) *session.CommandError {
	t.Helper()
	var got *session.CommandError
	if !errors.As(err, &got) {
		t.Fatalf("expected CommandError, got %v", err)
	}
	if got.Code != code || got.Uncertain != uncertain {
		t.Fatalf("got %+v; want code %q uncertain %v", got, code, uncertain)
	}
	return got
}

func TestListEmpty(t *testing.T) {
	w := newWorld(t)
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	sessions, err := b.List(context.Background())
	if err != nil || sessions == nil || len(sessions) != 0 {
		t.Fatalf("missing root: %v %v", sessions, err)
	}
	if exists(w.paths.StateRoot) || exists(w.paths.RuntimeDir) {
		t.Fatal("listing created a folder")
	}
	if err = os.MkdirAll(w.paths.StateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if sessions, err = b.List(context.Background()); err != nil || len(sessions) != 0 {
		t.Fatalf("empty root: %v %v", sessions, err)
	}
}

func TestList(t *testing.T) {
	w := newWorld(t)
	w.runtime(t)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	work := w.session(t, "my work", "core.json", "terminals.json")
	touch(t, filepath.Join(work, "core.json"), base)
	touch(t, filepath.Join(work, "terminals.json"), base.Add(2*time.Minute))
	// Neither a subfolder nor a symlink counts towards recency.
	if err := os.Mkdir(filepath.Join(work, "newer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("core.json", filepath.Join(work, "link.json")); err != nil {
		t.Fatal(err)
	}
	listen(t, w.paths.Socket("my work"))
	old := w.session(t, "old", "docs.json")
	touch(t, filepath.Join(old, "docs.json"), base.Add(time.Minute))
	stale(t, w.paths.Socket("old"))
	w.session(t, "default")
	odd := w.session(t, "odd", "core.json")
	touch(t, filepath.Join(odd, "core.json"), base.Add(3*time.Minute))
	if err := os.WriteFile(w.paths.Socket("odd"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Ignored: a regular file, a symlinked folder, invalid names, and names hc
	// would trim to another session.
	if err := os.WriteFile(filepath.Join(w.paths.StateRoot, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(work, filepath.Join(w.paths.StateRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	w.session(t, "bad\x01name")
	w.session(t, " lead")
	w.session(t, "  ")

	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	got, err := b.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []session.Session{
		{Name: "odd", Dir: odd, SocketPath: w.paths.Socket("odd"), ConnectionError: w.paths.Socket("odd") + " is not a socket", ModifiedAt: base.Add(3 * time.Minute), HasModifiedAt: true},
		{Name: "my work", Dir: work, SocketPath: w.paths.Socket("my work"), Running: true, ModifiedAt: base.Add(2 * time.Minute), HasModifiedAt: true},
		{Name: "old", Dir: old, SocketPath: w.paths.Socket("old"), ModifiedAt: base.Add(time.Minute), HasModifiedAt: true},
		{Name: "default", Dir: w.paths.SessionDir("default"), SocketPath: w.paths.Socket("default"), Default: true},
	}
	for i := range got {
		got[i].ModifiedAt = got[i].ModifiedAt.Truncate(time.Second)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestListPerSessionSocketFolder(t *testing.T) {
	w := newWorld(t)
	w.paths.RuntimeDir = ""
	w.session(t, "a")
	listen(t, filepath.Join(w.paths.SessionDir("a"), "a.sock"))
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	got, err := b.List(context.Background())
	if err != nil || len(got) != 1 || !got[0].Running || got[0].HasModifiedAt {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestListFailures(t *testing.T) {
	w := newWorld(t)
	w.session(t, "a")
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	// An unreadable session folder is a missing age, not a failed list.
	if err := os.Chmod(w.paths.SessionDir("a"), 0); err != nil {
		t.Fatal(err)
	}
	if got, err := b.List(context.Background()); err != nil || len(got) != 1 || got[0].HasModifiedAt {
		t.Fatalf("unreadable session folder: %+v %v", got, err)
	}
	if err := os.Chmod(w.paths.StateRoot, 0); err != nil {
		t.Fatal(err)
	}
	_, err := b.List(context.Background())
	requireError(t, err, "state_root", false)
	if err := os.Chmod(w.paths.StateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.List(ctx)
	requireError(t, err, "timeout", false)
}

func TestStop(t *testing.T) {
	w := newWorld(t)
	b, capture := w.backend(t, fixture{Stdout: `{"stopped": true}`}, nil, "rodeo")
	if err := b.Stop(context.Background(), "my work"); err != nil {
		t.Fatal(err)
	}
	got := readInvocation(t, capture)
	want := invocation{Args: []string{"server", "stop", "--json"}, Dir: w.root, Sessions: []string{"my work"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestStopFailures(t *testing.T) {
	cases := []struct {
		name    string
		f       fixture
		code    string
		message string
	}{
		{"not stopped", fixture{Stdout: `{"stopped": false}`}, "invalid_response", "Invalid hc response"},
		{"missing ack", fixture{Stdout: `{}`}, "invalid_response", "missing stop acknowledgement"},
		{"malformed", fixture{Stdout: `{"stopped": true} {}`}, "invalid_response", "unexpected data"},
		{"hc error with hint", fixture{Exit: 1, Stderr: `{"error":{"kind":"unavailable","message":"core is busy","hint":"try again"}}`}, "unavailable", "stop: core is busy try again"},
		{"hc error", fixture{Exit: 1, Stderr: `{"error":{"kind":"usage","message":"bad","hint":""}}`}, "usage", "stop: bad"},
		{"plain stderr", fixture{Exit: 2, Stderr: "  boom\n"}, "process", "stop: boom"},
		{"timeout", fixture{Sleep: true}, "timeout", "timed out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			b, _ := w.backend(t, c.f, nil, "rodeo")
			if c.f.Sleep {
				b.stopTimeout = 100 * time.Millisecond
			}
			got := requireError(t, b.Stop(context.Background(), "a"), c.code, true)
			if !strings.Contains(got.Error(), c.message) {
				t.Fatalf("message %q lacks %q", got.Error(), c.message)
			}
		})
	}
	w := newWorld(t)
	b, capture := w.backend(t, fixture{Stdout: `{"stopped": true}`}, nil, "rodeo")
	requireError(t, b.Stop(context.Background(), "a/b"), "invalid_name", false)
	if exists(capture) {
		t.Fatal("hc ran for an invalid name")
	}
}

func TestDelete(t *testing.T) {
	w := newWorld(t)
	w.runtime(t)
	for _, name := range []string{"x[1]", "keep"} {
		w.session(t, name, "core.json")
		stale(t, w.paths.Socket(name))
		stale(t, filepath.Join(w.paths.RuntimeDir, name+".holder.abcd1234.sock"))
	}
	// A session named like x[1]'s holder sockets keeps its own sockets.
	w.session(t, "x[1].holder.other")
	listen(t, w.paths.Socket("x[1].holder.other"))
	w.session(t, "x[1].holder")
	listen(t, w.paths.Socket("x[1].holder"))
	listen(t, filepath.Join(w.paths.RuntimeDir, "x[1].holder.other.holder.abcd1234.sock"))
	// A matching name that is not a socket is not removed.
	plain := filepath.Join(w.paths.RuntimeDir, "x[1].holder.file.sock")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	if err := b.Delete(context.Background(), "x[1]"); err != nil {
		t.Fatal(err)
	}
	if exists(w.paths.SessionDir("x[1]")) || exists(w.paths.Socket("x[1]")) || exists(filepath.Join(w.paths.RuntimeDir, "x[1].holder.abcd1234.sock")) {
		t.Fatal("session was not fully removed")
	}
	entries, _ := os.ReadDir(w.paths.RuntimeDir)
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	slices.Sort(left)
	want := []string{"keep.holder.abcd1234.sock", "keep.sock", "x[1].holder.file.sock", "x[1].holder.other.holder.abcd1234.sock", "x[1].holder.other.sock", "x[1].holder.sock"}
	if !reflect.DeepEqual(left, want) || !exists(w.paths.SessionDir("keep")) {
		t.Fatalf("runtime folder: %v", left)
	}
}

func TestDeletePerSessionSocketFolder(t *testing.T) {
	w := newWorld(t)
	w.paths.RuntimeDir = ""
	dir := w.session(t, "a", "core.json")
	stale(t, w.paths.Socket("a"))
	// A symlink inside the folder is removed, never followed.
	outside := filepath.Join(w.root, "outside")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	if err := b.Delete(context.Background(), "a"); err != nil || exists(dir) || !exists(outside) {
		t.Fatalf("got %v; folder left %v; outside kept %v", err, exists(dir), exists(outside))
	}
}

func TestDeleteRefusals(t *testing.T) {
	w := newWorld(t)
	w.runtime(t)
	w.session(t, "default", "core.json")
	w.session(t, "live", "core.json")
	listen(t, w.paths.Socket("live"))
	w.session(t, "odd", "core.json")
	if err := os.Mkdir(w.paths.Socket("odd"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := w.session(t, "target", "core.json")
	if err := os.Symlink(target, w.paths.SessionDir("linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.paths.SessionDir("file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	for name, code := range map[string]string{
		"default": "default_session",
		"a/b":     "invalid_name",
		"..":      "invalid_name",
		"linked":  "unsafe_path",
		"file":    "unsafe_path",
		"gone":    "not_found",
		"live":    "running",
		"odd":     "unavailable",
	} {
		requireError(t, b.Delete(context.Background(), name), code, false)
	}
	for _, path := range []string{w.paths.SessionDir("default"), w.paths.SessionDir("live"), w.paths.Socket("live"), w.paths.SessionDir("odd"),
		w.paths.SessionDir("linked"), filepath.Join(target, "core.json"), w.paths.SessionDir("file")} {
		if !exists(path) {
			t.Errorf("%s was removed", path)
		}
	}
	got := requireError(t, b.Delete(context.Background(), "live"), "running", false)
	if got.Message != "Session ‘live’ is running again; it was not deleted." {
		t.Fatalf("message %q", got.Message)
	}
}

func TestDeletePartialFailure(t *testing.T) {
	w := newWorld(t)
	dir := w.session(t, "a", "core.json")
	if err := os.Chmod(w.paths.StateRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	b, _ := w.backend(t, fixture{}, nil, "rodeo")
	got := requireError(t, b.Delete(context.Background(), "a"), "remove", true)
	if !strings.Contains(got.Message, "may be partly deleted") || !exists(dir) || exists(filepath.Join(dir, "core.json")) {
		t.Fatalf("got %q; folder kept %v", got.Message, exists(dir))
	}
	// A runtime folder that cannot be read is also a partial failure.
	w = newWorld(t)
	w.runtime(t)
	w.session(t, "a")
	if err := os.Chmod(w.paths.RuntimeDir, 0o300); err != nil {
		t.Fatal(err)
	}
	b, _ = w.backend(t, fixture{}, nil, "rodeo")
	requireError(t, b.Delete(context.Background(), "a"), "remove", true)
}

func TestLaunchCommand(t *testing.T) {
	w := newWorld(t)
	b, _ := w.backend(t, fixture{}, nil, "bin/rodeo")
	cmd := b.LaunchCommand("my work")
	client := filepath.Join(w.root, "bin", "rodeo")
	if cmd.Path != client || !reflect.DeepEqual(cmd.Args, []string{client}) || cmd.Dir != w.root {
		t.Fatalf("bad command: %+v", cmd)
	}
	var sessions []string
	for _, entry := range cmd.Env {
		if value, ok := strings.CutPrefix(entry, "HC_SESSION="); ok {
			sessions = append(sessions, value)
		}
	}
	if !reflect.DeepEqual(sessions, []string{"my work"}) || !slices.Contains(cmd.Env, "MARKER=kept") {
		t.Fatalf("bad environment: %v", cmd.Env)
	}
	// The inherited environment is not changed for later commands.
	if !slices.Contains(b.environ, "HC_SESSION=other") || slices.Contains(b.environ, "HC_SESSION=my work") {
		t.Fatalf("environment was mutated: %v", b.environ)
	}
	if b.ClientName() != "rodeo" || b.NameHint() != NameHint || b.hc.Executable == "" || !filepath.IsAbs(b.hc.Executable) {
		t.Fatalf("bad backend: %+v", b)
	}
	if b.ValidateNewName("default", nil) == nil || b.ValidateNewName(strings.Repeat("x", 100), nil) == nil || b.ValidateNewName("ok name", nil) != nil {
		t.Fatal("ValidateNewName does not use the v2 rules")
	}
}

func TestNewProductionDeadlines(t *testing.T) {
	b := New(Paths{}, nil, nil, "/bin/hc", "/bin/rodeo", "/")
	if b.listTimeout != 5*time.Second || b.stopTimeout != 20*time.Second || b.hc.WaitDelay != time.Second || b.hc.Program != "hc" || b.hc.Executable != "/bin/hc" {
		t.Fatalf("bad backend: %+v", b)
	}
}

func TestCheckLaunch(t *testing.T) {
	w := newWorld(t)
	appHome := filepath.Join(w.root, "app")
	defaultHome := filepath.Join(w.paths.Home, ".config", "herdr-app")
	for _, dir := range []string{appHome, defaultHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const open = "herdr-app is already open. Close it, then open ‘my work’ again."
	check := func(env Env, client string) string {
		b, _ := w.backend(t, fixture{}, env, client)
		return errText(b.CheckLaunch(context.Background(), "my work"))
	}
	if got := check(Env{"HERDR_APP_HOME": appHome}, "herdr-app"); got != "" {
		t.Fatalf("no socket: %q", got)
	}
	stale(t, filepath.Join(appHome, "herdr-app.sock"))
	if got := check(Env{"HERDR_APP_HOME": appHome}, "herdr-app"); got != "" {
		t.Fatalf("stale socket: %q", got)
	}
	if err := os.Remove(filepath.Join(appHome, "herdr-app.sock")); err != nil {
		t.Fatal(err)
	}
	listen(t, filepath.Join(appHome, "herdr-app.sock"))
	if got := check(Env{"HERDR_APP_HOME": appHome}, "/opt/bin/herdr-app"); got != open {
		t.Fatalf("live socket: %q", got)
	}
	if got := check(Env{"HERDR_APP_HOME": appHome}, "rodeo"); got != "" {
		t.Fatalf("rodeo: %q", got)
	}
	// Without HERDR_APP_HOME, the app home is HOME/.config/herdr-app.
	if got := check(Env{"HERDR_APP_HOME": ""}, "herdr-app"); got != "" {
		t.Fatalf("default home without socket: %q", got)
	}
	listen(t, filepath.Join(defaultHome, "herdr-app.sock"))
	if got := check(nil, "herdr-app"); got != open {
		t.Fatalf("default home: %q", got)
	}
}
