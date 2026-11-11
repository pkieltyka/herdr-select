package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type releaseRepo struct {
	t      *testing.T
	dir    string
	script string
	env    []string
}

func newReleaseRepo(t *testing.T) *releaseRepo {
	t.Helper()
	script, err := filepath.Abs("release")
	if err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	r := &releaseRepo{t, t.TempDir(), script, env}
	r.git("init", "-q")
	r.git("config", "user.email", "test@example.invalid")
	r.git("config", "user.name", "Release Test")
	r.write("VERSION", "0.1.0\n")
	r.write("other", "original\n")
	r.git("add", ".")
	r.git("commit", "-qm", "Initial")
	return r
}

func (r *releaseRepo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *releaseRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = r.dir, r.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *releaseRepo) snapshot() string {
	r.t.Helper()
	return r.git("rev-parse", "HEAD") + r.git("show-ref", "--tags", "--heads") +
		r.git("status", "--porcelain") + r.git("diff") + r.git("diff", "--cached")
}

func (r *releaseRepo) release(input string, args ...string) (string, error) {
	cmd := exec.Command("bash", append([]string{r.script}, args...)...)
	cmd.Dir, cmd.Env = r.dir, r.env
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestReleaseRepoIgnoresInheritedGitEnvironment(t *testing.T) {
	outer := newReleaseRepo(t)
	outer.git("config", "user.name", "Outer Repository")
	before := outer.git("rev-parse", "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(outer.dir, ".git"))
	t.Setenv("GIT_WORK_TREE", outer.dir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(outer.dir, ".git", "index"))
	inner := newReleaseRepo(t)
	if got := inner.git("rev-parse", "--show-toplevel"); got != inner.dir {
		t.Fatalf("repository directory = %q, want %q", got, inner.dir)
	}
	if outer.git("config", "user.name") != "Outer Repository" ||
		outer.git("rev-parse", "HEAD") != before || outer.git("status", "--porcelain") != "" {
		t.Fatal("inherited Git environment changed outer repository")
	}
}

func TestRelease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		input   string
		prompts int
	}{
		{"bare default", "0.1.0", "\n", 1},
		{"prefixed default", "v0.1.0", "\n", 1},
		{"bare yes", "1.2.3", "y\n", 1},
		{"prefixed yes", "v1.2.3", "yes\n", 1},
		{"uppercase yes", "1.2.3", "Y\n", 1},
		{"mixed case yes", "v1.2.3", "YeS\n", 1},
		{"invalid then default", "1.2.3", "maybe\n\n", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRepo(t)
			before := r.git("rev-parse", "HEAD")
			version := strings.TrimPrefix(tc.version, "v")
			out, err := r.release(tc.input, tc.version)
			if err != nil {
				t.Fatalf("release: %v\n%s", err, out)
			}
			prompt := "Are you sure you want to release v" + version + "? [Y/n] "
			if got := strings.Count(out, prompt); got != tc.prompts {
				t.Fatalf("prompt count = %d, want %d: %s", got, tc.prompts, out)
			}
			head := r.git("rev-parse", "HEAD")
			if head == before || r.git("rev-parse", "v"+version+"^{commit}") != head {
				t.Fatal("release must create a commit and tag that commit")
			}
			if got := r.git("show", "v"+version+":VERSION"); got != version {
				t.Fatalf("tagged VERSION = %q", got)
			}
			if r.git("tag", "--list") != "v"+version || r.git("cat-file", "-t", "v"+version) != "tag" {
				t.Fatal("release must create exactly one annotated tag with a single v prefix")
			}
			if r.git("status", "--porcelain") != "" || r.git("show", "HEAD:other") != "original" {
				t.Fatal("release left changes or modified unrelated content")
			}
		})
	}
}

func TestReleaseCancellationPreservesRepository(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"no", "n\n"},
		{"uppercase no", "N\n"},
		{"word no", "no\n"},
		{"mixed case no", "No\n"},
		{"EOF", ""},
		{"unterminated yes", "y"},
		{"invalid then EOF", "maybe\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRepo(t)
			before := r.snapshot()
			out, err := r.release(tc.input, "v1.2.3")
			if err == nil || !strings.Contains(out, "release cancelled") ||
				!strings.Contains(out, "Are you sure you want to release v1.2.3? [Y/n]") {
				t.Fatalf("release = %q, %v", out, err)
			}
			if r.snapshot() != before {
				t.Fatal("cancelled release changed repository")
			}
		})
	}
}

func TestReleaseRefusalsPreserveRepository(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		prepare func(*releaseRepo)
	}{
		{"missing version", nil, nil},
		{"extra argument", []string{"1.2.3", "extra"}, nil},
		{"existing tag", []string{"1.2.3"}, func(r *releaseRepo) { r.git("tag", "v1.2.3") }},
		{"existing prefixed tag", []string{"v1.2.3"}, func(r *releaseRepo) { r.git("tag", "v1.2.3") }},
		{"unstaged", []string{"1.2.3"}, func(r *releaseRepo) { r.write("other", "changed") }},
		{"staged", []string{"1.2.3"}, func(r *releaseRepo) { r.write("other", "changed"); r.git("add", "other") }},
		{"untracked", []string{"1.2.3"}, func(r *releaseRepo) { r.write("untracked", "keep") }},
		{"detached", []string{"1.2.3"}, func(r *releaseRepo) { r.git("checkout", "--detach") }},
	}
	for _, version := range []string{"vv1.2.3", "V1.2.3", "v", "v01.2.3", "01.2.3", "1.02.3", "1.2.03", "1.2", "1.2.3-rc.1", "1.2.3+abc", "-1.2.3", "1.2.3\n", ""} {
		cases = append(cases, struct {
			name    string
			args    []string
			prepare func(*releaseRepo)
		}{"invalid " + version, []string{version}, nil})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newReleaseRepo(t)
			if tc.prepare != nil {
				tc.prepare(r)
			}
			before := r.snapshot()
			out, err := r.release("y\n", tc.args...)
			if err == nil {
				t.Fatalf("release unexpectedly succeeded: %s", out)
			}
			if strings.Contains(out, "Are you sure") {
				t.Fatalf("release prompted before rejecting invalid input or checkout: %s", out)
			}
			if r.snapshot() != before {
				t.Fatal("refused release changed repository")
			}
		})
	}
}

func TestFailedCommitDoesNotCreateTag(t *testing.T) {
	r := newReleaseRepo(t)
	before := r.git("rev-parse", "HEAD")
	hook := filepath.Join(r.dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := r.release("y\n", "1.2.3"); err == nil || !strings.Contains(out, "commit failed") {
		t.Fatalf("release = %q, %v", out, err)
	}
	if r.git("rev-parse", "HEAD") != before || r.git("tag", "--list") != "" {
		t.Fatal("failed commit must not create a commit or tag")
	}
}
