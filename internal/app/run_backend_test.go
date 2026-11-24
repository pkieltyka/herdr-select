package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkieltyka/herdr-select/internal/hc"
	"github.com/pkieltyka/herdr-select/internal/herdr"
)

const (
	nestedWarning      = "Detach from your current herdr or hc session before opening another session."
	fixedSocketWarning = "herdr-select does not support HC_SOCKET or a fixed socket in herdr-core's config."
)

// isolatedEnviron points every folder herdr-core reads at a temp folder.
func isolatedEnviron(t *testing.T) (environ []string, home string) {
	t.Helper()
	home = t.TempDir()
	return []string{
		"HOME=" + home,
		"XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"XDG_RUNTIME_DIR=" + filepath.Join(home, "run"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"HERDR_APP_HOME=" + filepath.Join(home, "app"),
	}, home
}

func TestNewBackend(t *testing.T) {
	everything := installed("herdr", "herdr-app", "rodeo", "hc")

	t.Run("v1", func(t *testing.T) {
		environ, _ := isolatedEnviron(t)
		backend, warning := newBackend("", environ, everything)
		if _, ok := backend.(*herdr.Client); !ok || warning != "" {
			t.Fatalf("%T, %q", backend, warning)
		}
	})

	t.Run("v2", func(t *testing.T) {
		environ, home := isolatedEnviron(t)
		if err := os.MkdirAll(filepath.Join(home, "state", "herdr-core", "work"), 0o700); err != nil {
			t.Fatal(err)
		}
		backend, warning := newBackend("rodeo", environ, everything)
		v2, ok := backend.(*hc.Backend)
		if !ok || warning != "" {
			t.Fatalf("%T, %q", backend, warning)
		}
		if v2.ClientName() != "rodeo" {
			t.Fatal(v2.ClientName())
		}
		sessions, err := v2.List(context.Background())
		if err != nil || len(sessions) != 1 || sessions[0].Name != "work" {
			t.Fatalf("listing is not from the isolated state root: %+v, %v", sessions, err)
		}
	})

	for _, tc := range []struct {
		name, app string
		extra     []string
		lookPath  func(string) (string, error)
		want      string
	}{
		{"missing program", "rodeo", nil, installed("hc"), "rodeo was not found on PATH."},
		{"missing before nesting", "", []string{"HC_TERMINAL_ID=t1"}, installed(), "Install herdr or herdr-app, or pass --app=<client>."},
		{"v1 in herdr pane", "", []string{"HERDR_PANE_ID=w1:p1"}, everything, nestedWarning},
		{"v1 in hc terminal", "", []string{"HC_TERMINAL_ID=t1"}, everything, nestedWarning},
		{"v2 in herdr pane", "rodeo", []string{"HERDR_PANE_ID=w1:p1"}, everything, nestedWarning},
		{"v2 in hc terminal", "rodeo", []string{"HC_TERMINAL_ID=t1"}, everything, nestedWarning},
		{"nesting before fixed socket", "rodeo", []string{"HC_TERMINAL_ID=t1", "HC_SOCKET=/tmp/hc.sock"}, everything, nestedWarning},
		{"HC_SOCKET", "rodeo", []string{"HC_SOCKET=/tmp/hc.sock"}, everything, fixedSocketWarning},
		{"v1 ignores HC_SOCKET", "", []string{"HC_SOCKET=/tmp/hc.sock"}, everything, ""},
		{"empty nesting values", "rodeo", []string{"HERDR_PANE_ID=", "HC_TERMINAL_ID="}, everything, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			environ, _ := isolatedEnviron(t)
			backend, warning := newBackend(tc.app, append(environ, tc.extra...), tc.lookPath)
			if warning != tc.want || (warning == "") == (backend == nil) {
				t.Fatalf("%T, %q; want %q", backend, warning, tc.want)
			}
		})
	}

	t.Run("config socket", func(t *testing.T) {
		environ, home := isolatedEnviron(t)
		config := filepath.Join(home, "config", "herdr-core", "config.toml")
		if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config, []byte("socket = \"/tmp/hc.sock\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, warning := newBackend("rodeo", environ, everything); warning != fixedSocketWarning {
			t.Fatal(warning)
		}
	})

	t.Run("empty HOME", func(t *testing.T) {
		environ, _ := isolatedEnviron(t)
		backend, warning := newBackend("rodeo", append([]string{"HOME="}, environ[1:]...), everything)
		if backend != nil || warning != "herdr-select needs HOME to find herdr-core's sessions." {
			t.Fatalf("%T, %q", backend, warning)
		}
	})
}

func TestIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !isDir(dir) || isDir(file) || isDir(filepath.Join(dir, "missing")) {
		t.Fatal("isDir must report only existing folders")
	}
}
