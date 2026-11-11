//go:build linux

package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// The same test executable acts as the selector process and disposable Herdr.
// No test can discover or mutate the user's installed Herdr sessions.
func TestSubprocess(t *testing.T) {
	mode := os.Getenv("SELECTOR_TEST_PROCESS")
	if mode == "" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			mode = "herdr"
			break
		}
	}
	switch mode {
	case "app":
		os.Exit(Run())
	case "wait":
		cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess")
		cmd.Env = append(os.Environ(), "SELECTOR_TEST_PROCESS=signal-child")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		_, code, _ := runChild(cmd, 100*time.Millisecond)
		os.Exit(code)
	case "signal-child":
		sig := make(chan os.Signal, 2)
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
		fmt.Println("CHILD_READY")
		for received := range sig {
			if received == syscall.SIGTERM && os.Getenv("IGNORE_TERM") == "1" {
				continue
			}
			fmt.Println("CHILD_SIGNAL", received)
			time.Sleep(30 * time.Millisecond)
			fmt.Println("CHILD_CLEANED")
			os.Exit(23)
		}
	case "herdr":
		if len(args) > 0 && args[0] == "session" {
			fmt.Println(`{"sessions":[{"name":"default","default":true,"running":false,"session_dir":"/nonexistent/selector-test","socket_path":"/nonexistent/selector-test/socket"}]}`)
			if os.Getenv("REMOVE_HELPER") == "1" {
				os.Remove(os.Getenv("HELPER_PATH"))
			}
			os.Exit(0)
		}
		state, _ := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
		restored := state != nil && state.Lflag&unix.ICANON != 0 && state.Lflag&unix.ECHO != 0
		fmt.Printf("CHILD_TTY=%t RESTORED=%t ARGS=%s\n", term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()), restored, strings.Join(args, " "))
		fmt.Fprintln(os.Stderr, "DISPOSABLE_CHILD_DIAGNOSTIC")
		os.Exit(7)
	}
	os.Exit(99)
}

type capture struct {
	sync.Mutex
	buffer bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) { c.Lock(); defer c.Unlock(); return c.buffer.Write(p) }
func (c *capture) text() string                { c.Lock(); defer c.Unlock(); return c.buffer.String() }
func waitFor(t *testing.T, c *capture, want string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.text(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("missing %q in %q", want, c.text())
}
func helperEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "herdr")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestSubprocess -- \"$@\"\n"
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PATH=") && !strings.HasPrefix(entry, "HERDR_PANE_ID=") && !strings.HasPrefix(entry, "SELECTOR_TEST_PROCESS=") {
			env = append(env, entry)
		}
	}
	return append(env, "PATH="+dir, "TERM=xterm-256color", "LANG=C.UTF-8", "SELECTOR_TEST_PROCESS=app", "HELPER_PATH="+path)
}
func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "ptmx")
	t.Cleanup(func() { master.Close() })
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 36, Col: 100}); err != nil {
		t.Fatal(err)
	}
	return master, slave
}
func launchPTY(t *testing.T, env []string) (*exec.Cmd, *os.File, *os.File, *capture, chan error) {
	t.Helper()
	master, slave := openPTY(t)
	cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess")
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	output := &capture{}
	go func() { _, _ = io.Copy(output, master) }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return cmd, master, slave, output, done
}
func finish(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(8 * time.Second):
		t.Fatal("process did not finish")
		return nil
	}
}
func TestPTYHandoffRestoresTerminalAndPreservesFailure(t *testing.T) {
	_, master, slave, out, done := launchPTY(t, helperEnv(t))
	waitFor(t, out, "Resume")
	if _, err := master.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	err := finish(t, done)
	if exitCode(err) != 7 {
		t.Fatalf("exit %v", err)
	}
	waitFor(t, out, "DISPOSABLE_CHILD_DIAGNOSTIC")
	output := out.text()
	if !strings.Contains(output, "CHILD_TTY=true RESTORED=true ARGS=--session=default") {
		t.Fatal(output)
	}
	cleanup := strings.LastIndex(output, "\x1b[?1049l")
	child := strings.Index(output, "CHILD_TTY=")
	if cleanup < 0 || cleanup > child {
		t.Fatal("child launched before alternate-screen cleanup")
	}
	if strings.Contains(output[child:], "\x1b[?1049h") {
		t.Fatal("picker reopened after child failure")
	}
	state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || state.Lflag&unix.ICANON == 0 {
		t.Fatal("terminal not restored")
	}
}
func TestPTYStartFailureRecoversAndQuits(t *testing.T) {
	// Remove the helper during the second list: the command path remains captured,
	// so failure occurs in Start after the selector's preflight has succeeded.
	env := helperEnv(t)
	for _, entry := range env {
		if strings.HasPrefix(entry, "HELPER_PATH=") {
			path := strings.TrimPrefix(entry, "HELPER_PATH=")
			script, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			counter := filepath.Join(filepath.Dir(path), "called")
			script = []byte(strings.Replace(string(script), "exec ", "if [ -e '"+counter+"' ]; then export REMOVE_HELPER=1; else touch '"+counter+"'; fi\nexec ", 1)) // Use shell builtin creation because PATH contains only the helper.
			script = []byte(strings.Replace(string(script), "touch '"+counter+"'", ": > '"+counter+"'", 1))
			if err = os.WriteFile(path, script, 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, master, _, out, done := launchPTY(t, env)
	waitFor(t, out, "Resume")
	master.Write([]byte("\r"))
	waitFor(t, out, "Could not start Herdr")
	master.Write([]byte("q"))
	if err := finish(t, done); err != nil {
		t.Fatal(err)
	}
}
func TestPTYWarningsAndQuit(t *testing.T) {
	for _, kind := range []string{"missing", "nested"} {
		t.Run(kind, func(t *testing.T) {
			env := helperEnv(t)
			want := "Detach from your current Herdr"
			if kind == "missing" {
				env = append(env, "PATH="+t.TempDir())
				want = "Herdr is required"
			} else {
				env = append(env, "HERDR_PANE_ID=disposable")
			}
			_, master, _, out, done := launchPTY(t, env)
			waitFor(t, out, want)
			master.Write([]byte("q"))
			if code := exitCode(finish(t, done)); code != 1 {
				t.Fatal(code)
			}
		})
	}
}
func TestRejectNonTerminal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess")
	cmd.Env = helperEnv(t)
	output, err := cmd.CombinedOutput()
	if exitCode(err) != 1 || !strings.Contains(string(output), "requires an interactive terminal") {
		t.Fatalf("%v: %s", err, output)
	}
}
func TestChildSignalOwnership(t *testing.T) {
	for _, mode := range []string{"term", "interrupt", "kill"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess")
			cmd.Env = append(os.Environ(), "SELECTOR_TEST_PROCESS=wait")
			if mode == "kill" {
				cmd.Env = append(cmd.Env, "IGNORE_TERM=1")
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			out := &capture{}
			cmd.Stdout, cmd.Stderr = out, out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cmd.Process.Kill() })
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waitFor(t, out, "CHILD_READY")
			if mode == "interrupt" {
				if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
			} else {
				cmd.Process.Signal(syscall.SIGTERM)
			}
			code := exitCode(finish(t, done))
			want := 23
			if mode == "kill" {
				want = 137
			} else if !strings.Contains(out.text(), "CHILD_CLEANED") {
				t.Fatal("parent exited before child cleanup")
			}
			if code != want {
				t.Fatalf("exit %d want %d: %s", code, want, out.text())
			}
		})
	}
}
func TestChildStartFailure(t *testing.T) {
	started, code, err := runChild(exec.Command("/nonexistent/herdr-select-child"), time.Second)
	if started || code != 1 || err == nil {
		t.Fatal(started, code, err)
	}
}
