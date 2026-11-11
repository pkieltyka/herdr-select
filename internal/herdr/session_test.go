package herdr

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateNewName(t *testing.T) {
	existing := []Session{{Name: "Project"}}
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

func TestSortSessions(t *testing.T) {
	now := time.Unix(100, 0)
	sessions := []Session{
		{Name: "unknown-z"},
		{Name: "older", HasModifiedAt: true, ModifiedAt: now.Add(-time.Second)},
		{Name: "default", Default: true, HasModifiedAt: true, ModifiedAt: now},
		{Name: "A", HasModifiedAt: true, ModifiedAt: now},
		{Name: "future", HasModifiedAt: true, ModifiedAt: now.Add(time.Hour)},
		{Name: "unknown-a"},
	}
	SortSessions(sessions)
	var names []string
	for _, s := range sessions {
		names = append(names, s.Name)
	}
	if want := []string{"future", "A", "default", "older", "unknown-a", "unknown-z"}; !slices.Equal(names, want) {
		t.Fatalf("got %v want %v", names, want)
	}
	if !sessions[2].Default {
		t.Fatal("sorting changed session identity")
	}
}

func TestRelativeAge(t *testing.T) {
	now := time.Unix(1000000, 0)
	for _, tt := range []struct {
		age  time.Duration
		want string
	}{
		{-time.Hour, "0s ago"}, {0, "0s ago"}, {999 * time.Millisecond, "0s ago"},
		{time.Second, "1s ago"}, {59 * time.Second, "59s ago"}, {time.Minute, "1min ago"},
		{59*time.Minute + 59*time.Second, "59min ago"}, {time.Hour, "1h ago"},
		{24*time.Hour - time.Second, "23h ago"}, {24 * time.Hour, "1d ago"}, {6 * 24 * time.Hour, "6d ago"},
	} {
		if got := RelativeAge(now.Add(-tt.age), now); got != tt.want {
			t.Errorf("age %s: got %q want %q", tt.age, got, tt.want)
		}
	}
}
