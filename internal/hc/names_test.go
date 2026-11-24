package hc

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkieltyka/herdr-select/internal/session"
)

func TestValidExisting(t *testing.T) {
	for _, name := range []string{"a", "my work", "a.b", "[x]", "café", "-x", "default"} {
		if !validExisting(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	bad := []string{"", ".", "..", " lead", "trail ", "  ", "a\tb", "a\x7fb", "a\xffb"}
	for _, c := range forbidden {
		bad = append(bad, "a"+string(c)+"b")
	}
	for _, name := range bad {
		if validExisting(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestValidateNewName(t *testing.T) {
	paths := Paths{StateRoot: "/s/herdr-core", RuntimeDir: "/r", MaxSocket: 107}
	existing := []session.Session{{Name: "taken"}}
	const nbsp, nel = "\xc2\xa0", "\xc2\x85"
	cases := map[string]string{
		"my work":       "",
		"a[1]":          "",
		"":              "Enter a session name.",
		" a":            "Remove spaces at the start or end.",
		"a ":            "Remove spaces at the start or end.",
		"a" + nbsp:      "Remove spaces at the start or end.",
		"a\x00b":        "Control characters are not allowed.",
		"a" + nel + "b": "Control characters are not allowed.",
		".":             "The names “.” and “..” are not allowed.",
		"..":            "The names “.” and “..” are not allowed.",
		"default":       "The default session already exists; choose another name.",
		"taken":         "A session named ‘taken’ already exists.",
		"Taken":         "",
		" /":            "Remove spaces at the start or end.", // checks run in order
		"\x01/":         "Control characters are not allowed.",
	}
	for _, c := range forbidden {
		cases["a"+string(c)+"b"] = `These characters are not allowed: / \ : * ? " < > |`
	}
	for name, want := range cases {
		err := validateNewName(paths, name, existing)
		if got := errText(err); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestValidateNewNameSocketLength(t *testing.T) {
	const tooLong = "This name is too long for herdr-core's socket path."
	for _, paths := range []Paths{
		{StateRoot: "/s/herdr-core", RuntimeDir: "/r", MaxSocket: 107},
		{StateRoot: "/s/herdr-core", RuntimeDir: "/r", MaxSocket: 103},
		// Without a runtime folder the name appears twice: StateRoot/name/name.holder.sock.
		{StateRoot: "/s/herdr-core", MaxSocket: 107},
	} {
		// Bytes besides the name: folder + "/" + ".holder.sock" + "." + an 8-byte id.
		fixed, perName := len("/r/")+len(".holder.sock")+9, 1
		if paths.RuntimeDir == "" {
			fixed, perName = len("/s/herdr-core/")+1+len(".holder.sock")+9, 2
		}
		fits := strings.Repeat("x", (paths.MaxSocket-fixed)/perName)
		over := fits + "x"
		if holder := len(filepath.Join(paths.SocketFolder(fits), fits+".holder.sock")) + 9; holder > paths.MaxSocket || holder+perName <= paths.MaxSocket {
			t.Fatalf("bad boundary arithmetic: %d of %d", holder, paths.MaxSocket)
		}
		if err := validateNewName(paths, fits, nil); err != nil {
			t.Errorf("%+v: %d bytes should fit: %v", paths, len(fits), err)
		}
		if got := errText(validateNewName(paths, over, nil)); got != tooLong {
			t.Errorf("%+v: %d bytes: got %q", paths, len(over), got)
		}
	}
	// Bytes, not runes, are counted.
	paths := Paths{RuntimeDir: "/r", MaxSocket: 107}
	if got := errText(validateNewName(paths, strings.Repeat("é", 42), nil)); got != tooLong {
		t.Errorf("multi-byte name: got %q", got)
	}
}
