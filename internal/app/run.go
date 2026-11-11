package app

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
)

func Run() int {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "herdr-select requires an interactive terminal.")
		return 1
	}
	// Phase 1 packages the tested backend; phase 2 replaces this scaffold with
	// the approved selector and startup warning views.
	fmt.Fprintln(os.Stderr, "herdr-select: the interactive selector is not implemented yet.")
	return 1
}
