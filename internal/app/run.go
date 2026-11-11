package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/pkieltyka/herdr-select/internal/herdr"
	"github.com/pkieltyka/herdr-select/internal/ui"
)

func Run() int {
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
	client, err := herdr.NewClient()
	var model *ui.Model
	switch {
	case err != nil:
		model = ui.Warning("Herdr is required. Install Herdr first, then run herdr-select again. https://github.com/herdrdev/herdr", time.Now(), compact)
	case os.Getenv("HERDR_PANE_ID") != "":
		model = ui.Warning("Detach from your current Herdr session before opening another session. Nested Herdr clients are not supported.", time.Now(), compact)
	default:
		model = ui.New(client, time.Now(), compact)
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
		cmd := client.LaunchCommand(model.Launch.Name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		started, code, err := runChild(cmd, 5*time.Second)
		if started {
			return code
		}
		model.LaunchFailed(err)
	}
}
