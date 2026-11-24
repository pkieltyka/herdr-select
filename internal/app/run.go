package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/pkieltyka/herdr-select/internal/hc"
	"github.com/pkieltyka/herdr-select/internal/herdr"
	"github.com/pkieltyka/herdr-select/internal/session"
	"github.com/pkieltyka/herdr-select/internal/ui"
)

func Run(args []string, version string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, Usage)
		return 2
	}
	if opts.Version {
		fmt.Println(version)
		return 0
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "herdr-select requires an interactive terminal.")
		return 1
	}
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LC_CTYPE")
	}
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	compact := !strings.Contains(strings.ToLower(locale), "utf-8") && !strings.Contains(strings.ToLower(locale), "utf8")
	backend, warning := newBackend(opts.App, os.Environ(), exec.LookPath)
	var model *ui.Model
	if warning != "" {
		model = ui.Warning(warning, time.Now(), compact)
	} else {
		model = ui.New(backend, time.Now(), compact)
	}
	for {
		final, err := tea.NewProgram(model).Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "herdr-select:", err)
			return 1
		}
		model = final.(*ui.Model)
		if model.Launch == nil {
			return model.ExitCode
		}
		cmd := backend.LaunchCommand(model.Launch.Name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		started, code, err := runChild(cmd, 5*time.Second)
		if started {
			return code
		}
		model.LaunchFailed(err)
	}
}

// newBackend chooses the client, checks what must hold before any session is
// listed, and builds the chosen protocol's backend. A non-empty warning
// replaces the picker.
func newBackend(app string, environ []string, lookPath func(string) (string, error)) (session.Backend, string) {
	choice, missing := Choose(app, lookPath)
	if missing != "" {
		return nil, missing
	}
	env := hc.EnvFrom(environ)
	if env["HERDR_PANE_ID"] != "" || env["HC_TERMINAL_ID"] != "" {
		return nil, "Detach from your current herdr or hc session before opening another session."
	}
	if choice.Protocol == V1 {
		client, err := herdr.NewClient(choice.ClientPath)
		if err != nil {
			return nil, err.Error()
		}
		return client, ""
	}
	paths, err := hc.ResolvePaths(env, os.Getuid(), runtime.GOOS, isDir)
	if errors.Is(err, hc.ErrFixedSocket) {
		return nil, "herdr-select does not support HC_SOCKET or a fixed socket in herdr-core's config."
	}
	if err != nil {
		return nil, err.Error()
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err.Error()
	}
	return hc.New(paths, env, environ, choice.HcPath, choice.ClientPath, dir), ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
