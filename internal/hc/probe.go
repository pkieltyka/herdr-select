package hc

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"syscall"
	"time"
)

type State int

const (
	Stopped State = iota
	Running
	Unavailable
)

const probeTimeout = time.Second

// Probe connects to a Unix socket and closes at once, without speaking the hc
// protocol and without writing anything. A refused connection is a stale
// socket left by a crashed core, so it counts as Stopped.
func Probe(ctx context.Context, path string) (State, string) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Stopped, ""
	case err != nil:
		return Unavailable, err.Error()
	case info.Mode().Type() != fs.ModeSocket:
		return Unavailable, path + " is not a socket"
	}
	conn, err := (&net.Dialer{Timeout: probeTimeout}).DialContext(ctx, "unix", path)
	switch {
	case err == nil:
		_ = conn.Close()
		return Running, ""
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ENOENT):
		return Stopped, ""
	default:
		return Unavailable, err.Error()
	}
}
