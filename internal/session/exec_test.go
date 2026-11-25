package session

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

// The test binary doubles as the disposable child CLI, steered by RUNNER_TEST_MODE.
func TestMain(m *testing.M) {
	if mode := os.Getenv("RUNNER_TEST_MODE"); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

type report struct {
	Args        []string
	Dir, Marker string
}

func helper(mode string) int {
	switch mode {
	case "hold":
		time.Sleep(300 * time.Millisecond)
		return 0
	case "report":
		dir, _ := os.Getwd()
		data, _ := json.Marshal(report{os.Args[1:], dir, os.Getenv("RUNNER_TEST_MARKER")})
		os.Stdout.Write(data)
		return 0
	case "timeout":
		time.Sleep(10 * time.Second)
	case "stdout_overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", StdoutLimit+1))
	case "stderr_overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("x", StderrLimit+1))
	case "inherited_pipe":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "RUNNER_TEST_MODE=hold")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			return 92
		}
	case "fail":
		fmt.Fprint(os.Stderr, os.Getenv("RUNNER_TEST_STDERR"))
		return 3
	}
	return 0
}

func testRunner(t *testing.T, mode string, env ...string) Runner {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Subprocess cwd reporting resolves symlinked temporary parents (e.g. /var on macOS).
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env = append([]string{"RUNNER_TEST_MODE=" + mode, "GORACE=atexit_sleep_ms=0"}, env...)
	if dir, ok := os.LookupEnv("GOCOVERDIR"); ok {
		// Without it a coverage-instrumented child warns on stderr.
		env = append(env, "GOCOVERDIR="+dir)
	}
	return Runner{Executable: executable, Dir: dir, Env: env, WaitDelay: time.Second, Program: "Fixture"}
}

func noDiagnosis([]byte, error) (string, string) { return "", "" }

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

func TestRunnerPassesArgsDirAndEnv(t *testing.T) {
	r := testRunner(t, "report", "RUNNER_TEST_MARKER=kept")
	out, err := r.Run(context.Background(), "list", time.Second*5, false, noDiagnosis, "a", "--b", "c d")
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err = DecodeOne(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, report{[]string{"a", "--b", "c d"}, r.Dir, "kept"}) {
		t.Fatalf("child saw %+v", got)
	}
	cmd := r.Command(context.Background(), "x")
	if cmd.Path != r.Executable || cmd.Dir != r.Dir || !reflect.DeepEqual(cmd.Env, r.Env) || !reflect.DeepEqual(cmd.Args, []string{r.Executable, "x"}) {
		t.Fatalf("bad command %+v", cmd)
	}
}

func TestRunnerDiagnostics(t *testing.T) {
	var seen []byte
	structured := func(stderr []byte, _ error) (string, string) {
		seen = stderr
		return "denied", "Access denied"
	}
	r := testRunner(t, "fail", "RUNNER_TEST_STDERR=raw text")
	err := requireError(t, run(r, true, structured), "denied", true)
	if err.Message != "Access denied" || string(seen) != "raw text" || !strings.Contains(err.Error(), "may have changed") {
		t.Fatalf("diagnose not used: %+v (stderr %q)", err, seen)
	}
	err = requireError(t, run(testRunner(t, "fail", "RUNNER_TEST_STDERR=  plain diagnostic\n"), false, noDiagnosis), "process", false)
	if err.Message != "plain diagnostic" {
		t.Fatalf("stderr fallback = %q", err.Message)
	}
	err = requireError(t, run(testRunner(t, "fail"), true, noDiagnosis), "process", true)
	if err.Message != "exit status 3" {
		t.Fatalf("Go error fallback = %q", err.Message)
	}
}

func run(r Runner, mutation bool, diagnose func([]byte, error) (string, string)) error {
	_, err := r.Run(context.Background(), "op", 5*time.Second, mutation, diagnose)
	return err
}

func TestRunnerBounds(t *testing.T) {
	for _, mode := range []string{"stdout_overflow", "stderr_overflow", "timeout", "inherited_pipe"} {
		t.Run(mode, func(t *testing.T) {
			r := testRunner(t, mode)
			timeout, code := 2*time.Second, "output_limit"
			switch mode {
			case "timeout":
				timeout, code = 50*time.Millisecond, "timeout"
			case "inherited_pipe":
				r.WaitDelay, code = 20*time.Millisecond, "process"
			}
			start := time.Now()
			_, runErr := r.Run(context.Background(), "stop", timeout, true, noDiagnosis)
			err := requireError(t, runErr, code, true)
			if time.Since(start) > 3*time.Second {
				t.Fatal("command was not bounded")
			}
			switch mode {
			case "stdout_overflow", "stderr_overflow":
				if err.Message != "Fixture output exceeded the allowed size." {
					t.Fatalf("overflow message = %q", err.Message)
				}
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("lost deadline cause: %v", err)
				}
			case "inherited_pipe":
				if !errors.Is(err, exec.ErrWaitDelay) {
					t.Fatalf("lost WaitDelay cause: %v", err)
				}
			}
		})
	}
	t.Run("not_a_mutation", func(t *testing.T) {
		_, err := testRunner(t, "timeout").Run(context.Background(), "list", 50*time.Millisecond, false, noDiagnosis)
		requireError(t, err, "timeout", false)
	})
}

func TestRunnerStartFailure(t *testing.T) {
	r := testRunner(t, "report")
	r.Executable = filepath.Join(t.TempDir(), "missing")
	_, err := r.Run(context.Background(), "stop", time.Second, true, noDiagnosis)
	requireError(t, err, "start", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = testRunner(t, "report").Run(ctx, "stop", time.Second, true, noDiagnosis)
	if !errors.Is(requireError(t, err, "start", false), context.Canceled) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
}

func TestDecodeOne(t *testing.T) {
	var v map[string]int
	if err := DecodeOne([]byte(` {"a":1} `), &v); err != nil || v["a"] != 1 {
		t.Fatalf("got %v, %v", v, err)
	}
	for _, data := range []string{"", "{", `{"a":1} {}`, `{"a":1} x`} {
		if err := DecodeOne([]byte(data), &v); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := &limitedBuffer{limit: 3}
	for _, part := range []string{"ab", "cd", "ef"} {
		if n, err := b.Write([]byte(part)); n != len(part) || err != nil {
			t.Fatalf("write got %d %v", n, err)
		}
	}
	if b.String() != "abc" || string(b.Bytes()) != "abc" || !b.overflow {
		t.Fatalf("buffer = %q overflow %v", b.String(), b.overflow)
	}
}
