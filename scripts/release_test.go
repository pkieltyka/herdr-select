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

func (r *releaseRepo) release(args ...string) (string, error) {
	cmd := exec.Command("bash", append([]string{r.script}, args...)...)
	cmd.Dir, cmd.Env = r.dir, r.env
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
	for _, version := range []string{"0.1.0", "1.2.3"} {
		t.Run(version, func(t *testing.T) {
			r := newReleaseRepo(t)
			before := r.git("rev-parse", "HEAD")
			if out, err := r.release(version); err != nil {
				t.Fatalf("release: %v\n%s", err, out)
			}
			head := r.git("rev-parse", "HEAD")
			if head == before || r.git("rev-parse", "v"+version+"^{commit}") != head {
				t.Fatal("release must create a commit and tag that commit")
			}
			if got := r.git("show", "v"+version+":VERSION"); got != version {
				t.Fatalf("tagged VERSION = %q", got)
			}
			if r.git("status", "--porcelain") != "" || r.git("show", "HEAD:other") != "original" {
				t.Fatal("release left changes or modified unrelated content")
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
		{"unstaged", []string{"1.2.3"}, func(r *releaseRepo) { r.write("other", "changed") }},
		{"staged", []string{"1.2.3"}, func(r *releaseRepo) { r.write("other", "changed"); r.git("add", "other") }},
		{"untracked", []string{"1.2.3"}, func(r *releaseRepo) { r.write("untracked", "keep") }},
		{"detached", []string{"1.2.3"}, func(r *releaseRepo) { r.git("checkout", "--detach") }},
	}
	for _, version := range []string{"v1.2.3", "01.2.3", "1.02.3", "1.2.03", "1.2", "1.2.3-rc.1", "1.2.3+abc", "-1.2.3", "1.2.3\n", ""} {
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
			snapshot := func() string {
				return r.git("rev-parse", "HEAD") + r.git("show-ref", "--tags", "--heads") +
					r.git("status", "--porcelain") + r.git("diff") + r.git("diff", "--cached")
			}
			before := snapshot()
			if out, err := r.release(tc.args...); err == nil {
				t.Fatalf("release unexpectedly succeeded: %s", out)
			}
			if snapshot() != before {
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
	if out, err := r.release("1.2.3"); err == nil || !strings.Contains(out, "commit failed") {
		t.Fatalf("release = %q, %v", out, err)
	}
	if r.git("rev-parse", "HEAD") != before || r.git("tag", "--list") != "" {
		t.Fatal("failed commit must not create a commit or tag")
	}
}
