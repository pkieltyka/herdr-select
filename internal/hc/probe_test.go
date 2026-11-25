package hc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shortDir keeps socket paths under the 107-byte sun_path limit. Symlinked
// temporary parents (e.g. /var on macOS) are resolved.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, _ os.DirEntry, _ error) error { return os.Chmod(path, 0o700) })
		_ = os.RemoveAll(dir)
	})
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func listen(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
}

// stale leaves a socket file nothing listens on, as a crashed core does.
func stale(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProbe(t *testing.T) {
	dir := shortDir(t)
	live, old, plain := filepath.Join(dir, "live.sock"), filepath.Join(dir, "old.sock"), filepath.Join(dir, "plain.sock")
	listen(t, live)
	stale(t, old)
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(dir, "hidden")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	listen(t, filepath.Join(hidden, "s.sock"))
	if err := os.Chmod(hidden, 0); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path       string
		want       State
		diagnostic string
	}{
		{filepath.Join(dir, "none.sock"), Stopped, ""},
		{live, Running, ""},
		{old, Stopped, ""},
		{plain, Unavailable, "not a socket"},
		{filepath.Join(hidden, "s.sock"), Unavailable, "permission denied"},
	}
	for _, c := range cases {
		state, diagnostic := Probe(context.Background(), c.path)
		if state != c.want || !strings.Contains(diagnostic, c.diagnostic) || (c.diagnostic == "") != (diagnostic == "") {
			t.Errorf("%s: got %v %q; want %v %q", c.path, state, diagnostic, c.want, c.diagnostic)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state, diagnostic := Probe(ctx, live); state != Unavailable || diagnostic == "" {
		t.Errorf("canceled probe: got %v %q", state, diagnostic)
	}
}
