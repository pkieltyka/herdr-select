package app

import "testing"

func TestParseArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Options
	}{
		{nil, Options{}},
		{[]string{}, Options{}},
		{[]string{"--version"}, Options{Version: true}},
		{[]string{"--app=rodeo"}, Options{App: "rodeo"}},
		{[]string{"--app", "rodeo"}, Options{App: "rodeo"}},
		{[]string{"--app=/opt/bin/herdr-app"}, Options{App: "/opt/bin/herdr-app"}},
		{[]string{"--app=a=b"}, Options{App: "a=b"}},
		{[]string{"--app", "./my client"}, Options{App: "./my client"}},
	} {
		got, err := ParseArgs(tc.args)
		if err != nil || got != tc.want {
			t.Errorf("ParseArgs(%q) = %+v, %v; want %+v", tc.args, got, err, tc.want)
		}
	}
	for _, args := range [][]string{
		{"--app="},
		{"--app", ""},
		{"--app"},
		{"--app=a", "--app=b"},
		{"--app", "a", "--app", "b"},
		{"--app=a", "--app", "a"},
		{"--version", "--app=x"},
		{"--app=x", "--version"},
		{"--version", "--version"},
		{"--version", "extra"},
		{"-h"},
		{"--help"},
		{"--ap=x"},
		{"--app-x"},
		{"rodeo"},
		{"--app=rodeo", "extra"},
	} {
		if got, err := ParseArgs(args); err == nil {
			t.Errorf("ParseArgs(%q) = %+v; want a usage error", args, got)
		}
	}
}
