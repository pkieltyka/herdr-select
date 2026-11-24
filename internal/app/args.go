package app

import (
	"errors"
	"fmt"
	"strings"
)

const Usage = "usage: herdr-select [--app=<client>] | --version"

type Options struct {
	App     string
	Version bool
}

// ParseArgs accepts no arguments, --version alone, --app=<client> or
// --app <client>. Any other form is a usage error.
func ParseArgs(args []string) (Options, error) {
	var opts Options
	if len(args) == 1 && args[0] == "--version" {
		return Options{Version: true}, nil
	}
	seen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == "--app":
			if i+1 == len(args) {
				return Options{}, errors.New("--app needs a client")
			}
			i++
			value = args[i]
		case strings.HasPrefix(arg, "--app="):
			value = strings.TrimPrefix(arg, "--app=")
		case arg == "--version":
			return Options{}, errors.New("--version takes no other arguments")
		case strings.HasPrefix(arg, "-"):
			return Options{}, fmt.Errorf("unknown option %q", arg)
		default:
			return Options{}, fmt.Errorf("unexpected argument %q", arg)
		}
		if seen {
			return Options{}, errors.New("--app given more than once")
		}
		if value == "" {
			return Options{}, errors.New("--app needs a client")
		}
		seen, opts.App = true, value
	}
	return opts, nil
}
