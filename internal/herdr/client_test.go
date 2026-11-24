package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pkieltyka/herdr-select/internal/session"
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
	if f.Mode == "timeout" {
		time.Sleep(10 * time.Second)
	}
	fmt.Fprint(os.Stdout, f.Stdout)
	fmt.Fprint(os.Stderr, f.Stderr)
	return f.Exit
}

func fixtureClient(t *testing.T, f fixture) (*Client, string) {
	t.Helper()
	// Subprocess cwd reporting resolves symlinked temporary parents (e.g. /var on macOS).
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	env := append(os.Environ(), "HERDR_SELECT_TEST_FIXTURE="+path, "HERDR_SELECT_TEST_MARKER=preserved", "HERDR_SESSION=other", "GORACE=atexit_sleep_ms=0")
	c := &Client{runner: session.Runner{Executable: executable, Dir: dir, Env: env, WaitDelay: time.Second, Program: "Herdr"},
		listTimeout: 2 * time.Second, stopTimeout: 2 * time.Second, deleteTimeout: 2 * time.Second}
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
	t.Setenv("HERDR_SELECT_TEST_MARKER", "original")
	t.Chdir(dir)
	// A relative path from the startup layer is made absolute.
	c, err := NewClient("herdr")
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
	if c.listTimeout != 5*time.Second || c.stopTimeout != 20*time.Second || c.deleteTimeout != 10*time.Second || c.runner.WaitDelay != time.Second {
		t.Fatal("incorrect production deadlines")
	}
	if c.runner.Program != "Herdr" {
		t.Fatalf("program = %q", c.runner.Program)
	}
}

func TestClientInterface(t *testing.T) {
	c, capture := fixtureClient(t, fixture{})
	if c.NameHint() != "1-64 characters, no spaces: A-Z a-z 0-9 . _ -" || c.ClientName() != "Herdr" {
		t.Fatalf("hint %q, client %q", c.NameHint(), c.ClientName())
	}
	if err := c.CheckLaunch(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	existing := []session.Session{{Name: "work"}}
	for _, name := range []string{"ok", "a b", "work", "default", ""} {
		if got, want := c.ValidateNewName(name, existing), ValidateNewName(name, existing); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: method %v, rules %v", name, got, want)
		}
	}
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interface methods ran the herdr CLI")
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
	if !reflect.DeepEqual(got.Args, []string{"session", "list", "--json"}) || got.Dir != c.runner.Dir || got.Marker != "preserved" || got.SessionEnv != "other" {
		t.Fatalf("unexpected invocation %+v", got)
	}
}

func TestClientSymlinkTempDir(t *testing.T) {
	root := t.TempDir()
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(root, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", linked)
	// Fresh subtests allocate their temporary directories through the symlink.
	t.Run("List", TestClientList)
	t.Run("NewClient", TestNewClient)
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
				// Herdr v0.9.5 cli.rs session_stop/session_delete serialize SessionInfo:
				// https://github.com/herdrdev/herdr/blob/v0.9.5/src/cli.rs#L490-L524
				body, marshalErr := json.Marshal(map[string]any{ack: true, "session": record(name, "/tmp/herdr-fixture/sessions/"+name), "future": 123})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				c, capture := fixtureClient(t, fixture{Stdout: string(body)})
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
				if got := c.LaunchCommand(name).Args; !reflect.DeepEqual(got, []string{c.runner.Executable, "--session=" + name}) {
					t.Fatalf("bad launch argv: %v", got)
				}
			})
		}
	}
}

func TestClientMutationFailures(t *testing.T) {
	for _, op := range []string{"stop", "delete"} {
		for _, tt := range []struct {
			name string
			f    fixture
			code string
		}{
			{"missing_ack", fixture{Stdout: `{"session":{"name":"a"}}`}, "invalid_response"},
			{"null_ack", fixture{Stdout: `{"stopped":null,"session":{"name":"a"}}`}, "invalid_response"},
			{"false_ack", fixture{Stdout: `{"stopped":false,"session":{"name":"a"}}`}, "invalid_response"},
			{"wrong_target", fixture{Stdout: `{"stopped":true,"session":{"name":"b"}}`}, "invalid_response"},
			{"missing_target", fixture{Stdout: `{"stopped":true}`}, "invalid_response"},
			{"null_target", fixture{Stdout: `{"stopped":true,"session":null}`}, "invalid_response"},
			{"string_target", fixture{Stdout: `{"stopped":true,"session":"a"}`}, "invalid_response"},
			{"missing_name", fixture{Stdout: `{"stopped":true,"session":{}}`}, "invalid_response"},
			{"null_name", fixture{Stdout: `{"stopped":true,"session":{"name":null}}`}, "invalid_response"},
			{"wrong_name_type", fixture{Stdout: `{"stopped":true,"session":{"name":123}}`}, "invalid_response"},
			{"null", fixture{Stdout: `null`}, "invalid_response"},
			{"malformed", fixture{Stdout: `{`}, "invalid_response"},
			{"wrong_type", fixture{Stdout: `{"stopped":"true","session":{"name":"a"}}`}, "invalid_response"},
			{"trailing", fixture{Stdout: `{"stopped":true,"session":{"name":"a"}} true`}, "invalid_response"},
			{"wrong_operation", fixture{Stdout: `{"deleted":true,"session":{"name":"a"}}`}, "invalid_response"},
			{"nonzero", fixture{Stdout: `{"stopped":true,"session":{"name":"a"}}`, Stderr: "plain diagnostic", Exit: 2}, "process"},
			{"structured_error", fixture{Stderr: `{"error":{"code":"denied","message":"Access denied"}}`, Exit: 1}, "denied"},
			{"empty_diagnostic", fixture{Exit: 3}, "process"},
		} {
			t.Run(op+"/"+tt.name, func(t *testing.T) {
				if op == "delete" {
					tt.f.Stdout = strings.NewReplacer(`"stopped"`, `"deleted"`, `"deleted"`, `"stopped"`).Replace(tt.f.Stdout)
				}
				c, _ := fixtureClient(t, tt.f)
				mutation := c.Stop
				if op == "delete" {
					mutation = c.Delete
				}
				err := requireError(t, mutation(context.Background(), "a"), tt.code, true)
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
	}
	c, capture := fixtureClient(t, fixture{})
	requireError(t, c.Delete(context.Background(), "default"), "default_session", false)
	requireError(t, c.Stop(context.Background(), "a b"), "invalid_name", false)
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid mutation ran child")
	}
}

// The runner's bounds are covered in internal/session; this checks the client
// wires each deadline and keeps the runner's error classification.
func TestClientDeadlines(t *testing.T) {
	c, _ := fixtureClient(t, fixture{Mode: "timeout"})
	c.stopTimeout = 50 * time.Millisecond
	start := time.Now()
	err := requireError(t, c.Stop(context.Background(), "a"), "timeout", true)
	if time.Since(start) > 3*time.Second || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop was not bounded: %v", err)
	}
	c, _ = fixtureClient(t, fixture{Mode: "timeout"})
	c.deleteTimeout = 50 * time.Millisecond
	requireError(t, c.Delete(context.Background(), "a"), "timeout", true)
	c, _ = fixtureClient(t, fixture{Mode: "timeout"})
	c.listTimeout = 50 * time.Millisecond
	_, listErr := c.List(context.Background())
	requireError(t, listErr, "timeout", false)
	c, _ = fixtureClient(t, fixture{})
	c.runner.Executable = filepath.Join(t.TempDir(), "missing")
	requireError(t, c.Stop(context.Background(), "a"), "start", false)
}
