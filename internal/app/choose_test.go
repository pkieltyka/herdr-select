package app

import (
	"errors"
	"slices"
	"testing"
)

// installed resolves names to /bin/<name> and paths to themselves when listed.
func installed(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if !slices.Contains(names, name) {
			return "", errors.New("not found")
		}
		if name[0] == '/' {
			return name, nil
		}
		return "/bin/" + name, nil
	}
}

func TestChoose(t *testing.T) {
	const install = "Install herdr or herdr-app, or pass --app=<client>."
	v1 := Choice{Protocol: V1, Client: "herdr", ClientPath: "/bin/herdr"}
	for _, tc := range []struct {
		name, app string
		path      []string
		want      Choice
		missing   string
	}{
		{"herdr and herdr-app", "", []string{"herdr", "herdr-app", "hc"}, v1, ""},
		{"herdr only", "", []string{"herdr"}, v1, ""},
		{"herdr-app only", "", []string{"herdr-app", "hc"}, Choice{V2, "herdr-app", "/bin/herdr-app", "/bin/hc"}, ""},
		{"neither", "", nil, Choice{}, install},
		{"hc alone", "", []string{"hc", "rodeo"}, Choice{}, install},
		{"herdr-app without hc", "", []string{"herdr-app"}, Choice{}, "hc (herdr-core) is required for herdr-app. Install herdr-core, then run herdr-select again."},
		{"--app=herdr", "herdr", []string{"herdr", "herdr-app", "hc"}, v1, ""},
		{"--app=herdr path", "/x/bin/herdr", []string{"/x/bin/herdr"}, Choice{Protocol: V1, Client: "herdr", ClientPath: "/x/bin/herdr"}, ""},
		{"--app=herdr missing", "herdr", []string{"herdr-app", "hc"}, Choice{}, herdrRequired},
		{"--app=herdr path missing", "/x/bin/herdr", []string{"herdr"}, Choice{}, herdrRequired},
		{"--app=rodeo", "rodeo", []string{"herdr", "rodeo", "hc"}, Choice{V2, "rodeo", "/bin/rodeo", "/bin/hc"}, ""},
		{"--app=rodeo path", "/opt/rodeo", []string{"/opt/rodeo", "hc"}, Choice{V2, "rodeo", "/opt/rodeo", "/bin/hc"}, ""},
		{"--app=herdr-app", "herdr-app", []string{"herdr", "herdr-app", "hc"}, Choice{V2, "herdr-app", "/bin/herdr-app", "/bin/hc"}, ""},
		{"--app=herdrx", "herdrx", []string{"herdrx", "hc"}, Choice{V2, "herdrx", "/bin/herdrx", "/bin/hc"}, ""},
		{"--app=rodeo missing", "rodeo", []string{"herdr", "hc"}, Choice{}, "rodeo was not found on PATH."},
		{"--app=path missing", "/opt/rodeo", []string{"hc"}, Choice{}, "/opt/rodeo was not found on PATH."},
		{"--app=rodeo without hc", "rodeo", []string{"rodeo", "herdr"}, Choice{}, "hc (herdr-core) is required for rodeo. Install herdr-core, then run herdr-select again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, missing := Choose(tc.app, installed(tc.path...))
			if got != tc.want || missing != tc.missing {
				t.Fatalf("Choose(%q) = %+v, %q; want %+v, %q", tc.app, got, missing, tc.want, tc.missing)
			}
		})
	}
}
