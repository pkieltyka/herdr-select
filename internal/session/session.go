// Package session holds the protocol-neutral session domain shared by the
// protocol adapters and the UI.
package session

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type Session struct {
	Name, Dir, SocketPath string
	Default, Running      bool
	ConnectionError       string // non-empty means Unavailable
	ModifiedAt            time.Time
	HasModifiedAt         bool
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
