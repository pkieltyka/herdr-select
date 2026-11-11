package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	Stdout, Stderr, Mode, Capture string
	Exit                          int
}
type invocation struct {
	Args                    []string
	Dir, Marker, SessionEnv string
}

func TestMain(m *testing.M) {
	if path := os.Getenv("HERDR_SELECT_TEST_FIXTURE"); path != "" {
		os.Exit(helper(path))
	}
	os.Exit(m.Run())
}

func helper(path string) int {
	if os.Getenv("HERDR_SELECT_TEST_HOLD") == "1" {
		time.Sleep(300 * time.Millisecond)
		return 0
	}
	var f fixture
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &f) != nil {
		return 90
	}
	if f.Capture != "" {
		dir, _ := os.Getwd()
		data, _ := json.Marshal(invocation{os.Args[1:], dir, os.Getenv("HERDR_SELECT_TEST_MARKER"), os.Getenv("HERDR_SESSION")})
		if os.WriteFile(f.Capture, data, 0600) != nil {
			return 91
		}
	}
	switch f.Mode {
	case "timeout":
		time.Sleep(10 * time.Second)
	case "stdout_overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", stdoutLimit+1))
	case "stderr_overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("x", stderrLimit+1))
	case "inherited_pipe":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "HERDR_SELECT_TEST_HOLD=1")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 92
		}
	}
	fmt.Fprint(os.Stdout, f.Stdout)
	fmt.Fprint(os.Stderr, f.Stderr)
	return f.Exit
}

func fixtureClient(t *testing.T, f fixture) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	f.Capture = filepath.Join(dir, "invocation.json")
	path := filepath.Join(dir, "fixture.json")
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{executable: executable, dir: dir, env: append(os.Environ(), "HERDR_SELECT_TEST_FIXTURE="+path, "HERDR_SELECT_TEST_MARKER=preserved", "HERDR_SESSION=other", "GORACE=atexit_sleep_ms=0"), listTimeout: 2 * time.Second, stopTimeout: 2 * time.Second, deleteTimeout: 2 * time.Second, waitDelay: time.Second}
	return c, f.Capture
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
func requireError(t *testing.T, err error, code string, uncertain bool) *CommandError {
	t.Helper()
	var got *CommandError
	if !errors.As(err, &got) {
		t.Fatalf("expected CommandError, got %v", err)
	}
	if got.Code != code || got.Uncertain != uncertain {
		t.Fatalf("got %+v; want code %q uncertain %v", got, code, uncertain)
	}
	return got
}
func record(name, dir string) map[string]any {
	return map[string]any{"name": name, "default": name == "default", "running": false, "session_dir": dir, "socket_path": "socket"}
}
func response(records ...map[string]any) string {
	if records == nil {
		records = []map[string]any{}
	}
	data, _ := json.Marshal(map[string]any{"sessions": records})
	return string(data)
}

func TestNewClient(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Symlink(executable, filepath.Join(dir, "herdr")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HERDR_SELECT_TEST_MARKER", "original")
	t.Chdir(dir)
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_SELECT_TEST_MARKER", "changed")
	cmd := c.LaunchCommand("help")
	if cmd.Path != filepath.Join(dir, "herdr") || cmd.Dir != dir || !reflect.DeepEqual(cmd.Args, []string{cmd.Path, "--session=help"}) {
		t.Fatalf("bad command: %+v", cmd)
	}
	if !strings.Contains(strings.Join(cmd.Env, "\n"), "HERDR_SELECT_TEST_MARKER=original") {
		t.Fatal("environment was not captured")
	}
	if c.listTimeout != 5*time.Second || c.stopTimeout != 20*time.Second || c.deleteTimeout != 10*time.Second || c.waitDelay != time.Second {
		t.Fatal("incorrect production deadlines")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := NewClient(); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing executable: %v", err)
	}
}

func TestClientList(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old")
	future := filepath.Join(root, "future")
	directory := filepath.Join(root, "directory")
	now := time.Now().Truncate(time.Second)
	for dir, mtime := range map[string]time.Time{old: now.Add(-time.Hour), future: now.Add(time.Hour)} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "session.json")
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(directory, "session.json"), 0700); err != nil {
		t.Fatal(err)
	}
	unavailable := record("unavailable", filepath.Join(root, "missing"))
	unavailable["connection_error"] = "Permission denied"
	unavailable["extra_field"] = "allowed"
	running := record("newest", future)
	running["running"] = true
	c, capture := fixtureClient(t, fixture{Stdout: response(unavailable, record("default", old), record("directory", directory), running)})
	sessions, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 4 || sessions[0].Name != "newest" || !sessions[0].Running || !sessions[1].Default || !sessions[1].HasModifiedAt || !sessions[1].ModifiedAt.Equal(now.Add(-time.Hour)) || sessions[2].HasModifiedAt || sessions[3].HasModifiedAt || sessions[3].ConnectionError != "Permission denied" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	got := readInvocation(t, capture)
	if !reflect.DeepEqual(got.Args, []string{"session", "list", "--json"}) || got.Dir != c.dir || got.Marker != "preserved" || got.SessionEnv != "other" {
		t.Fatalf("unexpected invocation %+v", got)
	}
}

func TestClientEmptyList(t *testing.T) {
	c, _ := fixtureClient(t, fixture{Stdout: `{"sessions":[],"future":true}`})
	sessions, err := c.List(context.Background())
	if err != nil || sessions == nil || len(sessions) != 0 {
		t.Fatalf("got %#v, %v", sessions, err)
	}
}

func TestClientListInvalid(t *testing.T) {
	cases := map[string]string{
		"empty": "", "malformed": "{", "null": "null", "array": "[]", "missing": "{}", "null_sessions": `{"sessions":null}`, "nonarray": `{"sessions":{}}`, "null_record": `{"sessions":[null]}`,
		"trailing": response() + ` {}`, "trailing_garbage": response() + ` x`,
		"duplicate":    response(record("a", "/missing"), record("a", "/missing")),
		"invalid_name": response(record("a b", "/missing")),
	}
	for _, field := range []string{"name", "default", "running", "session_dir", "socket_path"} {
		r := record("a", "/missing")
		delete(r, field)
		cases["missing_"+field] = response(r)
		r = record("a", "/missing")
		r[field] = nil
		cases["null_"+field] = response(r)
	}
	for _, name := range []string{"default", "a"} {
		r := record(name, "/missing")
		r["default"] = name != "default"
		cases["inconsistent_"+name] = response(r)
	}
	r := record("a", "/missing")
	r["running"] = "false"
	cases["wrong_type"] = response(r)
	r = record("a", "/missing")
	r["connection_error"] = false
	cases["bad_connection_error"] = response(r)
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := fixtureClient(t, fixture{Stdout: body})
			sessions, err := c.List(context.Background())
			requireError(t, err, "invalid_response", false)
			if sessions != nil {
				t.Fatal("failure appeared as successful data")
			}
		})
	}
}

func TestClientMutations(t *testing.T) {
	for _, op := range []string{"stop", "delete"} {
		for _, name := range []string{"help", "-h", "--json", "--", "Case_1"} {
			t.Run(op+"_"+name, func(t *testing.T) {
				ack := "stopped"
				if op == "delete" {
					ack = "deleted"
				}
				body := fmt.Sprintf(`{"%s":true,"session":%q,"future":123}`, ack, name)
				c, capture := fixtureClient(t, fixture{Stdout: body})
				var err error
				if op == "stop" {
					err = c.Stop(context.Background(), name)
				} else {
					err = c.Delete(context.Background(), name)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got := readInvocation(t, capture); !reflect.DeepEqual(got.Args, []string{"session", op, "--json", "--", name}) {
					t.Fatalf("bad argv: %v", got.Args)
				}
				if got := c.LaunchCommand(name).Args; !reflect.DeepEqual(got, []string{c.executable, "--session=" + name}) {
					t.Fatalf("bad launch argv: %v", got)
				}
			})
		}
	}
}

func TestClientMutationFailures(t *testing.T) {
	for _, tt := range []struct {
		name string
		f    fixture
		code string
	}{
		{"missing_ack", fixture{Stdout: `{"session":"a"}`}, "invalid_response"},
		{"null_ack", fixture{Stdout: `{"stopped":null,"session":"a"}`}, "invalid_response"},
		{"false_ack", fixture{Stdout: `{"stopped":false,"session":"a"}`}, "invalid_response"},
		{"wrong_target", fixture{Stdout: `{"stopped":true,"session":"b"}`}, "invalid_response"},
		{"missing_target", fixture{Stdout: `{"stopped":true}`}, "invalid_response"},
		{"null", fixture{Stdout: `null`}, "invalid_response"},
		{"malformed", fixture{Stdout: `{`}, "invalid_response"},
		{"wrong_type", fixture{Stdout: `{"stopped":"true","session":"a"}`}, "invalid_response"},
		{"trailing", fixture{Stdout: `{"stopped":true,"session":"a"} true`}, "invalid_response"},
		{"wrong_operation", fixture{Stdout: `{"deleted":true,"session":"a"}`}, "invalid_response"},
		{"nonzero", fixture{Stdout: `{"stopped":true,"session":"a"}`, Stderr: "plain diagnostic", Exit: 2}, "process"},
		{"structured_error", fixture{Stderr: `{"error":{"code":"denied","message":"Access denied"}}`, Exit: 1}, "denied"},
		{"empty_diagnostic", fixture{Exit: 3}, "process"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := fixtureClient(t, tt.f)
			err := requireError(t, c.Stop(context.Background(), "a"), tt.code, true)
			if tt.name == "nonzero" && err.Message != "plain diagnostic" {
				t.Fatal(err)
			}
			if tt.name == "structured_error" && err.Message != "Access denied" {
				t.Fatal(err)
			}
			if !strings.Contains(err.Error(), "may have changed") {
				t.Fatal("missing uncertainty diagnostic")
			}
		})
	}
	c, capture := fixtureClient(t, fixture{})
	requireError(t, c.Delete(context.Background(), "default"), "default_session", false)
	requireError(t, c.Stop(context.Background(), "a b"), "invalid_name", false)
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid mutation ran child")
	}
}

func TestClientBounds(t *testing.T) {
	for _, mode := range []string{"stdout_overflow", "stderr_overflow", "timeout", "inherited_pipe"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := fixtureClient(t, fixture{Mode: mode, Stdout: `{"stopped":true,"session":"a"}`})
			code := "output_limit"
			switch mode {
			case "timeout":
				c.stopTimeout = 50 * time.Millisecond
				code = "timeout"
			case "inherited_pipe":
				c.waitDelay = 20 * time.Millisecond
				code = "process"
			}
			start := time.Now()
			err := requireError(t, c.Stop(context.Background(), "a"), code, true)
			if time.Since(start) > 3*time.Second {
				t.Fatal("command was not bounded")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost deadline cause: %v", err)
			}
			if mode == "inherited_pipe" && !errors.Is(err, exec.ErrWaitDelay) {
				t.Fatalf("lost WaitDelay cause: %v", err)
			}
		})
	}
	c, _ := fixtureClient(t, fixture{})
	c.executable = filepath.Join(t.TempDir(), "missing")
	requireError(t, c.Stop(context.Background(), "a"), "start", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ = fixtureClient(t, fixture{})
	err := requireError(t, c.Stop(ctx, "a"), "start", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
	c, _ = fixtureClient(t, fixture{Mode: "timeout"})
	c.listTimeout = 50 * time.Millisecond
	_, listErr := c.List(context.Background())
	requireError(t, listErr, "timeout", false)
}

func TestLimitedBuffer(t *testing.T) {
	b := &limitedBuffer{limit: 3}
	for _, part := range []string{"ab", "cd", "ef"} {
		if n, err := b.Write([]byte(part)); n != len(part) || err != nil {
			t.Fatalf("write got %d %v", n, err)
		}
	}
	if b.String() != "abc" || !b.overflow {
		t.Fatalf("buffer = %q overflow %v", b.String(), b.overflow)
	}
}
