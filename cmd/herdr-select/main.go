package main

import (
	"fmt"
	"os"

	"github.com/pkieltyka/herdr-select/internal/app"
)

// Nix sets version at link time; direct Go builds are explicitly development builds.
var version = "dev+unknown"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	os.Exit(app.Run())
}
