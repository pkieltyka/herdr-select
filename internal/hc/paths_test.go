package hc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvFrom(t *testing.T) {
	env := EnvFrom([]string{"A=1", "B=x=y", "A=2", "malformed", "=nokey", "E="})
	want := Env{"A": "1", "B": "x=y", "E": ""}
	if len(env) != len(want) {
		t.Fatalf("got %v", env)
	}
	for k, v := range want {
		if got, ok := env[k]; !ok || got != v {
			t.Fatalf("%s: got %q, want %q", k, got, v)
		}
	}
}

func noRunUser(string) bool { return false }

func TestResolvePaths(t *testing.T) {
	config := t.TempDir() // an empty config folder, so the user's own config is never read
	cases := []struct {
		name  string
		env   Env
		isDir func(string) bool
		want  Paths
	}{
		{"xdg set", Env{"HOME": "/h", "XDG_STATE_HOME": "/s", "XDG_RUNTIME_DIR": "/r", "XDG_CONFIG_HOME": config}, noRunUser,
			Paths{Home: "/h", StateRoot: "/s/herdr-core", RuntimeDir: "/r/herdr-core", MaxSocket: 107}},
		{"xdg empty", Env{"HOME": "/h", "XDG_STATE_HOME": "", "XDG_RUNTIME_DIR": "", "XDG_CONFIG_HOME": config}, noRunUser,
			Paths{Home: "/h", StateRoot: "/h/.local/state/herdr-core", MaxSocket: 107}},
		{"run user present", Env{"HOME": "/h", "XDG_CONFIG_HOME": config}, func(p string) bool { return p == "/run/user/42" },
			Paths{Home: "/h", StateRoot: "/h/.local/state/herdr-core", RuntimeDir: "/run/user/42/herdr-core", MaxSocket: 107}},
		{"runtime wins over run user", Env{"HOME": "/h", "XDG_RUNTIME_DIR": "/r", "XDG_CONFIG_HOME": config}, func(string) bool { return true },
			Paths{Home: "/h", StateRoot: "/h/.local/state/herdr-core", RuntimeDir: "/r/herdr-core", MaxSocket: 107}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolvePaths(c.env, 42, "linux", c.isDir)
			if err != nil || got != c.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
	// With XDG unset, the config is read from HOME/.config (empty here).
	home := t.TempDir()
	if got, err := ResolvePaths(Env{"HOME": home}, 42, "darwin", noRunUser); err != nil || got.MaxSocket != 103 || got.RuntimeDir != "" ||
		got.StateRoot != filepath.Join(home, ".local/state/herdr-core") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPathsPerSession(t *testing.T) {
	shared := Paths{StateRoot: "/s/herdr-core", RuntimeDir: "/r/herdr-core"}
	if shared.SessionDir("a b") != "/s/herdr-core/a b" || shared.SocketFolder("a b") != "/r/herdr-core" || shared.Socket("a b") != "/r/herdr-core/a b.sock" {
		t.Fatalf("bad shared paths: %+v", shared)
	}
	own := Paths{StateRoot: "/s/herdr-core"}
	if own.SocketFolder("x") != "/s/herdr-core/x" || own.Socket("x") != "/s/herdr-core/x/x.sock" {
		t.Fatalf("bad per-session paths")
	}
}

func writeConfig(t *testing.T, root, text string) {
	t.Helper()
	dir := filepath.Join(root, "herdr-core")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePathsFixedSocket(t *testing.T) {
	config := t.TempDir()
	home := t.TempDir()
	resolve := func(env Env) error {
		_, err := ResolvePaths(env, 1, "linux", noRunUser)
		return err
	}
	if err := resolve(Env{"HOME": home, "XDG_CONFIG_HOME": config, "HC_SOCKET": "/e/x.sock"}); !errors.Is(err, ErrFixedSocket) {
		t.Fatalf("HC_SOCKET: %v", err)
	}
	if err := resolve(Env{"HOME": home, "XDG_CONFIG_HOME": config, "HC_SOCKET": ""}); err != nil {
		t.Fatalf("empty HC_SOCKET: %v", err)
	}
	for text, fixed := range map[string]bool{
		"headless = \"80x24\"\nsocket = \"/c/core.sock\"\n": true,
		"socket = \"\"\n":        true,
		"headless = \"80x24\"\n": false,
		"socket = [":             false,
		"socket = 7\n":           false,
	} {
		writeConfig(t, config, text)
		if err := resolve(Env{"HOME": home, "XDG_CONFIG_HOME": config}); errors.Is(err, ErrFixedSocket) != fixed || (!fixed && err != nil) {
			t.Fatalf("config %q: %v", text, err)
		}
	}
	// Without XDG_CONFIG_HOME, the config lives under HOME/.config.
	writeConfig(t, filepath.Join(home, ".config"), "socket = \"/c/core.sock\"\n")
	if err := resolve(Env{"HOME": home, "XDG_CONFIG_HOME": ""}); !errors.Is(err, ErrFixedSocket) {
		t.Fatalf("HOME config: %v", err)
	}
}

func TestResolvePathsNeedsHome(t *testing.T) {
	for _, env := range []Env{{}, {"HOME": ""}} {
		if _, err := ResolvePaths(env, 1, "linux", noRunUser); err == nil || errors.Is(err, ErrFixedSocket) {
			t.Fatalf("env %v: %v", env, err)
		}
	}
}
