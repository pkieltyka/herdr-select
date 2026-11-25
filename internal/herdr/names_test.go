package herdr

import (
	"strings"
	"testing"

	"github.com/pkieltyka/herdr-select/internal/session"
)

func TestValidateNewName(t *testing.T) {
	existing := []session.Session{{Name: "Project"}}
	for _, name := range []string{"a", "A_z-9.0", "project", "DEFAULT", "help", "-h", "--json", "--", strings.Repeat("a", 64)} {
		t.Run("valid_"+name, func(t *testing.T) {
			if err := ValidateNewName(name, existing); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, name := range []string{"", ".", "..", "default", "Project", " a", "a ", "a b", "a\tb", "a\nb", "a\rb", "a\u00a0b", "a\x00b", "a/b", "é", "🐏", strings.Repeat("a", 65)} {
		t.Run("invalid_"+name, func(t *testing.T) {
			if err := ValidateNewName(name, existing); err == nil {
				t.Fatalf("accepted %q", name)
			}
		})
	}
}
