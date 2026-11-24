// Package hc is the herdr-core (v2) adapter. It reads herdr-core's state and
// runtime folders directly, because every hc command for a session starts that
// session's core, and runs hc only to stop a session.
package hc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Env is the environment the paths are resolved from.
type Env map[string]string

// EnvFrom reads "KEY=value" entries. The first of a repeated key wins, as with
// getenv.
func EnvFrom(environ []string) Env {
	env := make(Env, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if _, seen := env[key]; ok && key != "" && !seen {
			env[key] = value
		}
	}
	return env
}

// Paths mirrors herdr-core 0.1.20's path rules (rodeo's crates/hc/src/paths.rs).
type Paths struct {
	Home       string
	StateRoot  string // $XDG_STATE_HOME/herdr-core or ~/.local/state/herdr-core
	RuntimeDir string // "" means: each session's own state folder
	MaxSocket  int    // 107 on Linux, 103 on macOS
}

// ErrFixedSocket means every session shares one socket, so sessions cannot be
// told apart.
var ErrFixedSocket = errors.New("fixed socket")

func ResolvePaths(env Env, uid int, goos string, isDir func(string) bool) (Paths, error) {
	home := env["HOME"]
	if home == "" {
		return Paths{}, errors.New("herdr-select needs HOME to find herdr-core's sessions.")
	}
	xdg := func(key, fallback string) string {
		if value := env[key]; value != "" {
			return value
		}
		return fallback
	}
	if env["HC_SOCKET"] != "" || configuredSocket(filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "herdr-core", "config.toml")) {
		return Paths{}, ErrFixedSocket
	}
	p := Paths{Home: home, StateRoot: filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "herdr-core"), MaxSocket: 107}
	if goos == "darwin" {
		p.MaxSocket = 103
	}
	if runtime := env["XDG_RUNTIME_DIR"]; runtime != "" {
		p.RuntimeDir = filepath.Join(runtime, "herdr-core")
	} else if runUser := fmt.Sprintf("/run/user/%d", uid); isDir(runUser) {
		p.RuntimeDir = filepath.Join(runUser, "herdr-core")
	}
	return p, nil
}

// configuredSocket reports whether the config sets socket. A missing or
// unparsable config counts as no fixed socket, as in hc.
func configuredSocket(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var config struct {
		Socket *string `toml:"socket"`
	}
	return toml.Unmarshal(data, &config) == nil && config.Socket != nil
}

func (p Paths) SessionDir(name string) string { return filepath.Join(p.StateRoot, name) }

func (p Paths) SocketFolder(name string) string {
	if p.RuntimeDir == "" {
		return p.SessionDir(name)
	}
	return p.RuntimeDir
}

func (p Paths) Socket(name string) string { return filepath.Join(p.SocketFolder(name), name+".sock") }
