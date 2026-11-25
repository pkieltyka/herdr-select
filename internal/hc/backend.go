package hc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pkieltyka/herdr-select/internal/session"
)

var _ session.Backend = (*Backend)(nil)

type Backend struct {
	paths                    Paths
	env                      Env
	environ                  []string
	hc                       session.Runner
	clientPath, dir          string
	listTimeout, stopTimeout time.Duration
}

// New builds the v2 backend from the startup layer's resolved paths and
// programs. environ is passed to hc and the client, with HC_SESSION set per
// session; relative program paths are taken from dir.
func New(paths Paths, env Env, environ []string, hcPath, clientPath, dir string) *Backend {
	absolute := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(dir, path)
	}
	return &Backend{paths: paths, env: env, environ: environ, clientPath: absolute(clientPath), dir: dir,
		hc:          session.Runner{Executable: absolute(hcPath), Dir: dir, WaitDelay: time.Second, Program: "hc"},
		listTimeout: 5 * time.Second, stopTimeout: 20 * time.Second}
}

func (b *Backend) ValidateNewName(name string, existing []session.Session) error {
	return validateNewName(b.paths, name, existing)
}
func (b *Backend) NameHint() string   { return NameHint }
func (b *Backend) ClientName() string { return filepath.Base(b.clientPath) }

// Consequence explains the stop and delete confirmations in herdr-core's
// terms: delete removes the state folder, and kept terminals return as shells.
func (b *Backend) Consequence(delete, running bool) string {
	switch {
	case !delete:
		return "Its programs end. Terminals with keep on come back as shells next time."
	case running:
		return "Its programs end. Its saved layout and history are removed. This can't be undone."
	default:
		return "Its saved layout and history are removed. This can't be undone."
	}
}

// LaunchCommand only constructs the command; the client starts the session's
// core when needed. Interactive clients have no deadline.
func (b *Backend) LaunchCommand(name string) *exec.Cmd {
	cmd := exec.Command(b.clientPath)
	cmd.Dir = b.dir
	cmd.Env = withSession(b.environ, name)
	return cmd
}

// CheckLaunch refuses to launch herdr-app while its window is open: herdr-app
// is single-instance per app home, so a second launch would only raise the open
// window on whatever session it already shows.
func (b *Backend) CheckLaunch(ctx context.Context, name string) error {
	if b.ClientName() != "herdr-app" {
		return nil
	}
	home := b.env["HERDR_APP_HOME"]
	if home == "" {
		home = filepath.Join(b.paths.Home, ".config", "herdr-app")
	}
	if state, _ := Probe(ctx, filepath.Join(home, "herdr-app.sock")); state == Running {
		return fmt.Errorf("herdr-app is already open. Close it, then open ‘%s’ again.", name)
	}
	return nil
}

// withSession replaces any inherited HC_SESSION with name.
func withSession(environ []string, name string) []string {
	env := slices.DeleteFunc(slices.Clone(environ), func(entry string) bool { return strings.HasPrefix(entry, "HC_SESSION=") })
	return append(env, "HC_SESSION="+name)
}

// List reads the state root and probes each session's socket. It never starts,
// stops or changes a core, and never writes.
func (b *Backend) List(parent context.Context) ([]session.Session, error) {
	ctx, cancel := context.WithTimeout(parent, b.listTimeout)
	defer cancel()
	entries, err := os.ReadDir(b.paths.StateRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return []session.Session{}, nil
	}
	if err != nil {
		return nil, &session.CommandError{Operation: "list", Code: "state_root", Message: err.Error(), Cause: err}
	}
	var names []string
	for _, entry := range entries {
		if entry.Type().IsDir() && validExisting(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sessions := make([]session.Session, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { sessions[i] = b.read(ctx, name) })
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, &session.CommandError{Operation: "list", Code: "timeout", Message: "The command was canceled or timed out.", Cause: err}
	}
	session.SortSessions(sessions)
	return sessions, nil
}

func (b *Backend) read(ctx context.Context, name string) session.Session {
	s := session.Session{Name: name, Dir: b.paths.SessionDir(name), SocketPath: b.paths.Socket(name), Default: name == "default"}
	state, diagnostic := Probe(ctx, s.SocketPath)
	s.Running = state == Running
	if state == Unavailable {
		s.ConnectionError = diagnostic
	}
	s.ModifiedAt, s.HasModifiedAt = newestFile(s.Dir)
	return s
}

// newestFile is the newest modification time among regular files directly in dir.
func newestFile(dir string) (newest time.Time, found bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if info, err := entry.Info(); err == nil && (!found || info.ModTime().After(newest)) {
			newest, found = info.ModTime(), true
		}
	}
	return newest, found
}

// Stop runs hc for this session only. The UI stops only sessions it saw
// Running, so hc does not start a core here.
func (b *Backend) Stop(ctx context.Context, name string) error {
	if !validExisting(name) {
		return &session.CommandError{Operation: "stop", Code: "invalid_name", Message: fmt.Sprintf("‘%s’ is not a valid herdr-core session name.", name)}
	}
	runner := b.hc
	runner.Env = withSession(b.environ, name)
	data, err := runner.Run(ctx, "stop", b.stopTimeout, true, diagnose, "server", "stop", "--json")
	if err != nil {
		return err
	}
	var response struct {
		Stopped *bool `json:"stopped"`
	}
	if err := session.DecodeOne(data, &response); err != nil || response.Stopped == nil || !*response.Stopped {
		if err == nil {
			err = errors.New("missing stop acknowledgement")
		}
		return &session.CommandError{Operation: "stop", Code: "invalid_response", Message: "Invalid hc response: " + err.Error(), Uncertain: true, Cause: err}
	}
	return nil
}

// diagnose reads hc's {"error":{"kind","message","hint"}}; an empty message
// lets the runner fall back to stderr.
func diagnose(stderr []byte, _ error) (string, string) {
	var response struct {
		Error *struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if session.DecodeOne(stderr, &response) != nil || response.Error == nil || response.Error.Message == "" {
		return "", ""
	}
	message := response.Error.Message
	if response.Error.Hint != "" {
		message += " " + response.Error.Hint
	}
	return response.Error.Kind, message
}

// Delete removes a stopped session's state folder and leftover sockets. hc has
// no delete command, so this is a deliberate exception to deferring to the
// upstream CLI. The probe right before removal narrows, but cannot close, the
// race with a core that starts at that instant, as with v1's create race.
func (b *Backend) Delete(ctx context.Context, name string) error {
	refuse := func(code, format string) error {
		return &session.CommandError{Operation: "delete", Code: code, Message: fmt.Sprintf(format, name)}
	}
	if !validExisting(name) {
		return refuse("invalid_name", "‘%s’ is not a valid herdr-core session name.")
	}
	if name == "default" {
		return refuse("default_session", "The default session cannot be deleted.")
	}
	dir := b.paths.SessionDir(name)
	if filepath.Dir(dir) != b.paths.StateRoot {
		return refuse("unsafe_path", "Session ‘%s’ is not a folder in herdr-core's state folder; it was not deleted.")
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return refuse("not_found", "Session ‘%s’ no longer exists.")
	}
	if err != nil || !info.IsDir() {
		return refuse("unsafe_path", "Session ‘%s’ is not a plain folder; it was not deleted.")
	}
	switch state, _ := Probe(ctx, b.paths.Socket(name)); state {
	case Running:
		return refuse("running", "Session ‘%s’ is running again; it was not deleted.")
	case Unavailable:
		return refuse("unavailable", "Session ‘%s’ is unavailable; it was not deleted.")
	}
	others := b.sessionsNamedLike(name + ".holder")
	removed := os.RemoveAll(dir) // Removes symlinks inside without following them.
	if removed == nil && b.paths.RuntimeDir != "" {
		removed = b.removeSockets(name, others)
	}
	if removed != nil {
		return &session.CommandError{Operation: "delete", Code: "remove", Uncertain: true, Cause: removed,
			Message: fmt.Sprintf("Could not remove session ‘%s’: %v. The session may be partly deleted.", name, removed)}
	}
	return nil
}

// sessionsNamedLike lists other session folders whose names start with prefix.
// Their sockets match the holder-socket pattern of the session being deleted.
func (b *Backend) sessionsNamedLike(prefix string) []string {
	entries, _ := os.ReadDir(b.paths.StateRoot)
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// removeSockets compares names as strings, never with a glob, because hc
// names may contain glob characters.
func (b *Backend) removeSockets(name string, others []string) error {
	paths := []string{b.paths.Socket(name)}
	entries, err := os.ReadDir(b.paths.RuntimeDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		file := entry.Name()
		if entry.Type() == fs.ModeSocket && strings.HasPrefix(file, name+".holder.") && strings.HasSuffix(file, ".sock") &&
			!slices.ContainsFunc(others, func(other string) bool { return ownsSocket(other, file) }) {
			paths = append(paths, filepath.Join(b.paths.RuntimeDir, file))
		}
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func ownsSocket(session, file string) bool {
	return file == session+".sock" || strings.HasPrefix(file, session+".holder.")
}
