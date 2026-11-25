package session

import (
	"errors"
	"testing"
)

func TestCommandError(t *testing.T) {
	cause := errors.New("cause")
	certain := &CommandError{Operation: "stop", Code: "process", Message: "Denied.", Cause: cause}
	if got := certain.Error(); got != "stop: Denied." {
		t.Fatalf("certain error = %q", got)
	}
	if !errors.Is(certain, cause) {
		t.Fatal("cause not unwrapped")
	}
	uncertain := &CommandError{Operation: "delete", Message: "Failed.", Uncertain: true}
	if got := uncertain.Error(); got != "delete: Failed. The session state may have changed; refresh before continuing." {
		t.Fatalf("uncertain error = %q", got)
	}
}
