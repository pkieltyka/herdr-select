package hc

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pkieltyka/herdr-select/internal/session"
)

const NameHint = `No / \ : * ? " < > |, and no leading or trailing spaces`

const (
	forbidden = `/\:*?"<>|`
	// holderID is the length of the id hc puts in a holder socket's name.
	holderID = 8
)

// validExisting is hc's folder-name rule for a session. The spec accepts
// leading and trailing spaces, but hc trims HC_SESSION (and an all-space value
// becomes "default"), so such a folder would open, stop or delete a different
// session; hc never creates one, so it is skipped.
func validExisting(name string) bool {
	return name != "" && utf8.ValidString(name) && strings.TrimSpace(name) == name && name != "." && name != ".." &&
		!strings.ContainsFunc(name, unicode.IsControl) && !strings.ContainsAny(name, forbidden)
}

func validateNewName(paths Paths, name string, existing []session.Session) error {
	first, _ := utf8.DecodeRuneInString(name)
	last, _ := utf8.DecodeLastRuneInString(name)
	switch {
	case name == "":
		return fmt.Errorf("Enter a session name.")
	case unicode.IsSpace(first) || unicode.IsSpace(last):
		return fmt.Errorf("Remove spaces at the start or end.")
	case strings.ContainsFunc(name, unicode.IsControl):
		return fmt.Errorf("Control characters are not allowed.")
	case strings.ContainsAny(name, forbidden):
		return fmt.Errorf(`These characters are not allowed: / \ : * ? " < > |`)
	case name == "." || name == "..":
		return fmt.Errorf("The names “.” and “..” are not allowed.")
	case socketTooLong(paths, name):
		return fmt.Errorf("This name is too long for herdr-core's socket path.")
	case name == "default":
		return fmt.Errorf("The default session already exists; choose another name.")
	}
	for _, s := range existing {
		if s.Name == name {
			return fmt.Errorf("A session named ‘%s’ already exists.", name)
		}
	}
	return nil
}

// socketTooLong is hc's own check: the longest holder socket path, and the
// session socket, must fit in sun_path.
func socketTooLong(paths Paths, name string) bool {
	holder := len(filepath.Join(paths.SocketFolder(name), name+".holder.sock")) + 1 + holderID
	return max(holder, len(paths.Socket(name))) > paths.MaxSocket
}
