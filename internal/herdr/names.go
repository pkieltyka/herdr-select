package herdr

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/pkieltyka/herdr-select/internal/session"
)

const NameHint = "1-64 characters, no spaces: A-Z a-z 0-9 . _ -"

// ValidateNewName preserves the exact name; it never trims or normalizes input.
func ValidateNewName(name string, existing []session.Session) error {
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
