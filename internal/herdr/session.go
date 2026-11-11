// Package herdr contains the session domain and the installed Herdr CLI adapter.
package herdr

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

type Session struct {
	Name, Dir, SocketPath string
	Default, Running      bool
	ConnectionError       string
	ModifiedAt            time.Time
	HasModifiedAt         bool
}

// ValidateNewName preserves the exact name; it never trims or normalizes input.
func ValidateNewName(name string, existing []Session) error {
	if err := validateName(name); err != nil {
		return err
	}
	if name == "default" {
		return fmt.Errorf("The default session already exists; choose another name.")
	}
	for _, s := range existing {
		if s.Name == name {
			return fmt.Errorf("A session named ‘%s’ already exists.", name)
		}
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("Enter a session name.")
	}
	if len(name) > 64 {
		return fmt.Errorf("Use at most 64 characters.")
	}
	if strings.ContainsFunc(name, unicode.IsSpace) {
		return fmt.Errorf("Spaces are not allowed.")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return fmt.Errorf("Use only letters, numbers, dots, underscores, and hyphens.")
		}
	}
	if name == "." || name == ".." {
		return fmt.Errorf("The names “.” and “..” are not allowed.")
	}
	return nil
}

func SortSessions(sessions []Session) {
	slices.SortFunc(sessions, func(a, b Session) int {
		if a.HasModifiedAt != b.HasModifiedAt {
			if a.HasModifiedAt {
				return -1
			}
			return 1
		}
		if a.HasModifiedAt {
			if c := b.ModifiedAt.Compare(a.ModifiedAt); c != 0 {
				return c
			}
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func RelativeAge(modified, now time.Time) string {
	age := max(now.Sub(modified), 0)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds ago", int64(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dmin ago", int64(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int64(age/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int64(age/(24*time.Hour)))
	}
}
