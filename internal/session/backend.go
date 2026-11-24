package session

import (
	"context"
	"os/exec"
)

// Backend is everything the UI needs from a session protocol. The UI never
// learns which protocol is behind it.
type Backend interface {
	List(ctx context.Context) ([]Session, error)
	Stop(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error

	// ValidateNewName applies the protocol's rules, "default" and duplicates.
	ValidateNewName(name string, existing []Session) error
	// NameHint is the one-line hint under the New Session field.
	NameHint() string
	// CheckLaunch runs right before a launch request is emitted. nil means go ahead.
	CheckLaunch(ctx context.Context, name string) error
	// ClientName is used in messages such as "Could not start rodeo: ...".
	ClientName() string
	// LaunchCommand builds, but does not start, the interactive client.
	LaunchCommand(name string) *exec.Cmd
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
