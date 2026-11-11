package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionWithoutTerminalOrHerdr(t *testing.T) {
	for _, tc := range []struct {
		name, ldflags, want string
	}{
		{"direct", "", "dev+unknown\n"},
		{"injected", "-X main.version=v1.2.3+abcdef.dirty", "v1.2.3+abcdef.dirty\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "herdr-select")
			build := exec.Command("go", "build", "-ldflags="+tc.ldflags, "-o", binary, ".")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, output)
			}
			cmd := exec.Command(binary, "--version")
			cmd.Env = []string{"PATH=" + t.TempDir(), "HERDR_PANE_ID=nested"}
			output, err := cmd.CombinedOutput()
			if err != nil || string(output) != tc.want {
				t.Fatalf("--version = %q, %v; want %q", output, err, tc.want)
			}
			// The ordinary launcher still rejects nonterminal input.
			cmd = exec.Command(binary)
			cmd.Env = os.Environ()
			output, err = cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "requires an interactive terminal") {
				t.Fatalf("normal launch = %q, %v", output, err)
			}
		})
	}
}
