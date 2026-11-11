package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	stdoutLimit = 4 << 20
	stderrLimit = 64 << 10
)

type Backend interface {
	List(context.Context) ([]Session, error)
	Stop(context.Context, string) error
	Delete(context.Context, string) error
}

// CommandError describes a failed CLI operation. Uncertain means the child ran
// and may have changed session state; callers must refresh, never retry blindly.
type CommandError struct {
	Operation string
	Code      string
	Message   string
	Uncertain bool
	Cause     error
}

func (e *CommandError) Error() string {
	message := e.Operation + ": " + e.Message
	if e.Uncertain {
		message += " The session state may have changed; refresh before continuing."
	}
	return message
}
func (e *CommandError) Unwrap() error { return e.Cause }

type Client struct {
	executable                                         string
	env                                                []string
	dir                                                string
	listTimeout, stopTimeout, deleteTimeout, waitDelay time.Duration
}

// NewClient resolves Herdr once, retaining the same environment and directory as
// a direct CLI invocation. It does not inspect or alter personal session data.
func NewClient() (*Client, error) {
	path, err := exec.LookPath("herdr")
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Client{executable: path, env: os.Environ(), dir: dir,
		listTimeout: 5 * time.Second, stopTimeout: 20 * time.Second,
		deleteTimeout: 10 * time.Second, waitDelay: time.Second}, nil
}

func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.executable, args...)
	cmd.Env = c.env
	cmd.Dir = c.dir
	return cmd
}

// LaunchCommand only constructs the command. The app owns terminal handoff,
// Start/Wait, streams, and signals; interactive clients have no deadline.
func (c *Client) LaunchCommand(name string) *exec.Cmd {
	// Herdr treats `session attach help` specially; --session= also safely handles
	// option-like valid names without restricting the upstream naming grammar.
	return c.command(context.Background(), "--session="+name)
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

func (c *Client) run(parent context.Context, operation string, timeout time.Duration, mutation bool, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := c.command(ctx, args...)
	cmd.WaitDelay = c.waitDelay
	out := &limitedBuffer{limit: stdoutLimit}
	diagnostic := &limitedBuffer{limit: stderrLimit}
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
		failure.Code, failure.Message = "output_limit", "Herdr output exceeded the allowed size."
	case err != nil:
		failure.Code, failure.Message = errorDiagnostic(diagnostic.Bytes(), err)
	default:
		return out.Bytes(), nil
	}
	return nil, failure
}

func errorDiagnostic(data []byte, fallback error) (string, string) {
	var response struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if decodeOne(data, &response) == nil && response.Error != nil && response.Error.Message != "" {
		return response.Error.Code, response.Error.Message
	}
	if message := strings.TrimSpace(string(data)); message != "" {
		return "process", message
	}
	return "process", fallback.Error()
}

func decodeOne(data []byte, target any) error {
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

type wireSession struct {
	Name            *string `json:"name"`
	Default         *bool   `json:"default"`
	Running         *bool   `json:"running"`
	Dir             *string `json:"session_dir"`
	SocketPath      *string `json:"socket_path"`
	ConnectionError *string `json:"connection_error"`
}

func invalidResponse(operation string, mutation bool, err error) error {
	return &CommandError{Operation: operation, Code: "invalid_response", Message: "Invalid Herdr response: " + err.Error(), Uncertain: mutation, Cause: err}
}

func (c *Client) List(ctx context.Context) ([]Session, error) {
	data, err := c.run(ctx, "list", c.listTimeout, false, "session", "list", "--json")
	if err != nil {
		return nil, err
	}
	sessions, err := decodeSessions(data)
	if err != nil {
		return nil, invalidResponse("list", false, err)
	}
	for i := range sessions {
		s := &sessions[i]
		path := filepath.Join(s.Dir, "session.json")
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.dir, path)
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			s.ModifiedAt, s.HasModifiedAt = info.ModTime(), true
		}
	}
	SortSessions(sessions)
	return sessions, nil
}

func decodeSessions(data []byte) ([]Session, error) {
	var response struct {
		Sessions *[]wireSession `json:"sessions"`
	}
	if err := decodeOne(data, &response); err != nil {
		return nil, err
	}
	if response.Sessions == nil {
		return nil, fmt.Errorf("sessions must be an array")
	}
	sessions := make([]Session, 0, len(*response.Sessions))
	seen := make(map[string]bool)
	for _, wire := range *response.Sessions {
		if wire.Name == nil || wire.Default == nil || wire.Running == nil || wire.Dir == nil || wire.SocketPath == nil {
			return nil, fmt.Errorf("session record is missing a required field")
		}
		name := *wire.Name
		if err := validateName(name); err != nil {
			return nil, fmt.Errorf("invalid session name: %w", err)
		}
		if (*wire.Default) != (name == "default") {
			return nil, fmt.Errorf("inconsistent default session identity")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate session identity %q", name)
		}
		seen[name] = true
		session := Session{Name: name, Default: *wire.Default, Running: *wire.Running, Dir: *wire.Dir, SocketPath: *wire.SocketPath}
		if wire.ConnectionError != nil {
			session.ConnectionError = *wire.ConnectionError
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (c *Client) Stop(ctx context.Context, name string) error {
	return c.mutate(ctx, "stop", name, c.stopTimeout)
}
func (c *Client) Delete(ctx context.Context, name string) error {
	if name == "default" {
		return &CommandError{Operation: "delete", Code: "default_session", Message: "The default session cannot be deleted."}
	}
	return c.mutate(ctx, "delete", name, c.deleteTimeout)
}
func (c *Client) mutate(ctx context.Context, operation, name string, timeout time.Duration) error {
	if err := validateName(name); err != nil {
		return &CommandError{Operation: operation, Code: "invalid_name", Message: err.Error(), Cause: err}
	}
	data, err := c.run(ctx, operation, timeout, true, "session", operation, "--json", "--", name)
	if err != nil {
		return err
	}
	var response struct {
		Stopped *bool   `json:"stopped"`
		Deleted *bool   `json:"deleted"`
		Session *string `json:"session"`
	}
	if err := decodeOne(data, &response); err != nil {
		return invalidResponse(operation, true, err)
	}
	ack := response.Stopped
	if operation == "delete" {
		ack = response.Deleted
	}
	if ack == nil || !*ack || response.Session == nil || *response.Session != name {
		return invalidResponse(operation, true, fmt.Errorf("missing or mismatched %s acknowledgement", operation))
	}
	return nil
}
