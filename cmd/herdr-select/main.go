package main

import (
	"os"

	"github.com/pkieltyka/herdr-select/internal/app"
)

// Nix sets version at link time; direct Go builds are explicitly development builds.
var version = "dev+unknown"

func main() {
	os.Exit(app.Run(os.Args[1:], version))
}
