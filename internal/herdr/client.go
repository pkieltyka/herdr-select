// Package herdr is the herdr v1 adapter: it drives the installed herdr CLI.
package herdr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/pkieltyka/herdr-select/internal/session"
)

var _ session.Backend = (*Client)(nil)

type Client struct {
	runner                                  session.Runner
	listTimeout, stopTimeout, deleteTimeout time.Duration
}

// NewClient uses the herdr executable the startup layer resolved, retaining the
// same environment and directory as a direct CLI invocation. It does not
// inspect or alter personal session data.
func NewClient(path string) (*Client, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Client{runner: session.Runner{Executable: path, Env: os.Environ(), Dir: dir, WaitDelay: time.Second, Program: "Herdr"},
		listTimeout: 5 * time.Second, stopTimeout: 20 * time.Second, deleteTimeout: 10 * time.Second}, nil
}

func (c *Client) ValidateNewName(name string, existing []session.Session) error {
	return ValidateNewName(name, existing)
}
func (c *Client) NameHint() string                          { return NameHint }
func (c *Client) CheckLaunch(context.Context, string) error { return nil }
func (c *Client) ClientName() string                        { return "Herdr" }

// LaunchCommand only constructs the command. The app owns terminal handoff,
// Start/Wait, streams, and signals; interactive clients have no deadline.
func (c *Client) LaunchCommand(name string) *exec.Cmd {
	// Herdr treats `session attach help` specially; --session= also safely handles
	// option-like valid names without restricting the upstream naming grammar.
	return c.runner.Command(context.Background(), "--session="+name)
}

func (c *Client) run(ctx context.Context, operation string, timeout time.Duration, mutation bool, args ...string) ([]byte, error) {
	return c.runner.Run(ctx, operation, timeout, mutation, diagnose, args...)
}

// diagnose reads herdr's {"error":{"code","message"}}; an empty message lets
// the runner fall back to stderr.
func diagnose(stderr []byte, _ error) (string, string) {
	var response struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if session.DecodeOne(stderr, &response) == nil && response.Error != nil {
		return response.Error.Code, response.Error.Message
	}
	return "", ""
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
	return &session.CommandError{Operation: operation, Code: "invalid_response", Message: "Invalid Herdr response: " + err.Error(), Uncertain: mutation, Cause: err}
}

func (c *Client) List(ctx context.Context) ([]session.Session, error) {
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
			path = filepath.Join(c.runner.Dir, path)
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			s.ModifiedAt, s.HasModifiedAt = info.ModTime(), true
		}
	}
	session.SortSessions(sessions)
	return sessions, nil
}

func decodeSessions(data []byte) ([]session.Session, error) {
	var response struct {
		Sessions *[]wireSession `json:"sessions"`
	}
	if err := session.DecodeOne(data, &response); err != nil {
		return nil, err
	}
	if response.Sessions == nil {
		return nil, fmt.Errorf("sessions must be an array")
	}
	sessions := make([]session.Session, 0, len(*response.Sessions))
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
		s := session.Session{Name: name, Default: *wire.Default, Running: *wire.Running, Dir: *wire.Dir, SocketPath: *wire.SocketPath}
		if wire.ConnectionError != nil {
			s.ConnectionError = *wire.ConnectionError
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (c *Client) Stop(ctx context.Context, name string) error {
	return c.mutate(ctx, "stop", name, c.stopTimeout)
}
func (c *Client) Delete(ctx context.Context, name string) error {
	if name == "default" {
		return &session.CommandError{Operation: "delete", Code: "default_session", Message: "The default session cannot be deleted."}
	}
	return c.mutate(ctx, "delete", name, c.deleteTimeout)
}
func (c *Client) mutate(ctx context.Context, operation, name string, timeout time.Duration) error {
	if err := validateName(name); err != nil {
		return &session.CommandError{Operation: operation, Code: "invalid_name", Message: err.Error(), Cause: err}
	}
	data, err := c.run(ctx, operation, timeout, true, "session", operation, "--json", "--", name)
	if err != nil {
		return err
	}
	var response struct {
		Stopped *bool `json:"stopped"`
		Deleted *bool `json:"deleted"`
		// Herdr 0.9.5 returns a SessionInfo object, not a session-name string.
		Session *struct {
			Name *string `json:"name"`
		} `json:"session"`
	}
	if err := session.DecodeOne(data, &response); err != nil {
		return invalidResponse(operation, true, err)
	}
	ack := response.Stopped
	if operation == "delete" {
		ack = response.Deleted
	}
	if ack == nil || !*ack || response.Session == nil || response.Session.Name == nil || *response.Session.Name != name {
		return invalidResponse(operation, true, fmt.Errorf("missing or mismatched %s acknowledgement", operation))
	}
	return nil
}
