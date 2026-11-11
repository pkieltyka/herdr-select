package app

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// runChild is called only after Bubble Tea restores the terminal. A successfully
// started client owns the terminal until it exits; its diagnostics stay intact.
func runChild(cmd *exec.Cmd, grace time.Duration) (started bool, code int, err error) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err = cmd.Start(); err != nil {
		return false, 1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timeout <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case err = <-done:
			return true, exitCode(err), err
		case received := <-signals:
			// Terminal SIGINT already reaches both foreground processes. Do not forward
			// another interrupt or abandon the client before it restores its terminal.
			if received == syscall.SIGTERM && timer == nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				timer = time.NewTimer(grace)
				timeout = timer.C
			}
		case <-timeout:
			_ = cmd.Process.Kill() // Only the interactive CLI child, never its persistent server.
			timeout = nil
		}
	}
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		if exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
	}
	return 1
}
