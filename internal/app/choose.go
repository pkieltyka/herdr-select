package app

import "path/filepath"

type Protocol int

const (
	V1 Protocol = iota // herdr CLI
	V2                 // herdr-core
)

type Choice struct {
	Protocol           Protocol
	Client, ClientPath string // display name and resolved path
	HcPath             string // V2 only
}

const herdrRequired = "Herdr is required. Install Herdr first, then run herdr-select again. https://github.com/herdrdev/herdr"

// Choose picks the protocol and client from --app, or else from PATH: herdr
// stays the default for backward compatibility. A non-empty message names the
// missing program.
func Choose(app string, lookPath func(string) (string, error)) (Choice, string) {
	var choice Choice
	switch {
	case app != "":
		path, err := lookPath(app)
		if filepath.Base(app) == "herdr" {
			if err != nil {
				return Choice{}, herdrRequired
			}
			return Choice{Protocol: V1, Client: "herdr", ClientPath: path}, ""
		}
		if err != nil {
			return Choice{}, app + " was not found on PATH."
		}
		choice = Choice{Protocol: V2, Client: filepath.Base(app), ClientPath: path}
	default:
		if path, err := lookPath("herdr"); err == nil {
			return Choice{Protocol: V1, Client: "herdr", ClientPath: path}, ""
		}
		path, err := lookPath("herdr-app")
		if err != nil {
			return Choice{}, "Install herdr or herdr-app, or pass --app=<client>."
		}
		choice = Choice{Protocol: V2, Client: "herdr-app", ClientPath: path}
	}
	hcPath, err := lookPath("hc")
	if err != nil {
		return Choice{}, "hc (herdr-core) is required for " + choice.Client + ". Install herdr-core, then run herdr-select again."
	}
	choice.HcPath = hcPath
	return choice, ""
}
