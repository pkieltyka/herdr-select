package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	StdoutLimit = 4 << 20
	StderrLimit = 64 << 10
)

// Runner runs short, bounded CLI operations with a fixed executable,
// environment and working directory.
type Runner struct {
	Executable, Dir string
	Env             []string
	WaitDelay       time.Duration
	// Program names the CLI in the output-limit message, such as "Herdr".
	Program string
}

func (r Runner) Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Executable, args...)
	cmd.Env = r.Env
	cmd.Dir = r.Dir
	return cmd
}

// Run executes one operation under timeout. diagnose turns a failed child's
// stderr into a code and message; an empty message falls back to the trimmed
// stderr, then the Go error. Once the child started, a mutation's failure is
// Uncertain.
func (r Runner) Run(parent context.Context, operation string, timeout time.Duration, mutation bool,
	diagnose func(stderr []byte, err error) (code, message string), args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := r.Command(ctx, args...)
	cmd.WaitDelay = r.WaitDelay
	out := &limitedBuffer{limit: StdoutLimit}
	diagnostic := &limitedBuffer{limit: StderrLimit}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	if err := cmd.Start(); err != nil {
		return nil, &CommandError{Operation: operation, Code: "start", Message: err.Error(), Cause: err}
	}
	err := cmd.Wait()
	failure := &CommandError{Operation: operation, Uncertain: mutation, Cause: err}
	switch {
	case ctx.Err() != nil:
		failure.Code, failure.Message, failure.Cause = "timeout", "The command was canceled or timed out.", ctx.Err()
	case out.overflow || diagnostic.overflow:
		failure.Code, failure.Message = "output_limit", r.Program+" output exceeded the allowed size."
	case err != nil:
		failure.Code, failure.Message = diagnose(diagnostic.Bytes(), err)
		if failure.Message == "" {
			failure.Code, failure.Message = fallbackDiagnostic(diagnostic.Bytes(), err)
		}
	default:
		return out.Bytes(), nil
	}
	return nil, failure
}

func fallbackDiagnostic(stderr []byte, err error) (string, string) {
	if message := strings.TrimSpace(string(stderr)); message != "" {
		return "process", message
	}
	return "process", err.Error()
}

// DecodeOne decodes exactly one JSON value and rejects anything after it.
func DecodeOne(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected data after JSON response")
	}
	return nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, b.limit-b.buffer.Len())
	_, _ = b.buffer.Write(p[:keep])
	b.overflow = b.overflow || keep < n
	return n, nil // Drain excess output so the child cannot block on a full pipe.
}
