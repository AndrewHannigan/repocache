package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneArgs(t *testing.T) {
	t.Run("no git config", func(t *testing.T) {
		got := cloneArgs("/cache", "https://x/y", "main", "/dest", nil)
		want := []string{"clone", "--reference", "/cache", "--branch", "main", "--", "https://x/y", "/dest"}
		assertArgs(t, got, want)
	})

	t.Run("git config seeded as sorted --config flags before --", func(t *testing.T) {
		got := cloneArgs("/cache", "https://x/y", "main", "/dest", map[string]string{
			"user.email":     "me@work.com",
			"core.hooksPath": ".githooks",
		})
		want := []string{
			"clone", "--reference", "/cache", "--branch", "main",
			"--config", "core.hooksPath=.githooks", // sorted: core before user
			"--config", "user.email=me@work.com",
			"--", "https://x/y", "/dest",
		}
		assertArgs(t, got, want)
	})
}

func TestRename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	t.Setenv("HOME", t.TempDir())

	name := "github.com/foo/bar"
	const oldBranch = "feature/old"
	const newBranch = "renamed"
	makeWorkspace(t, name, oldBranch)

	if !Exists(name, oldBranch) {
		t.Fatalf("setup: workspace %s/%s should exist", name, oldBranch)
	}

	newPath, err := Rename(name, oldBranch, newBranch)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Directory moved to the new branch path, old path gone.
	if newPath != PathFor(name, newBranch) {
		t.Errorf("Rename returned %q, want %q", newPath, PathFor(name, newBranch))
	}
	if !Exists(name, newBranch) {
		t.Errorf("workspace should exist at new branch %s", newBranch)
	}
	if Exists(name, oldBranch) {
		t.Errorf("workspace should no longer exist at old branch %s", oldBranch)
	}
	// Empty intermediate dir from the slash branch was pruned.
	if _, err := os.Stat(filepath.Dir(PathFor(name, oldBranch))); !os.IsNotExist(err) {
		t.Errorf("old branch's empty parent dir should be pruned, stat err = %v", err)
	}
	// The checked-out branch was renamed too.
	if got := gitCurrentBranch(t, newPath); got != newBranch {
		t.Errorf("checked-out branch = %q, want %q", got, newBranch)
	}
}

func TestRenameErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	t.Setenv("HOME", t.TempDir())

	name := "github.com/foo/bar"
	makeWorkspace(t, name, "main")
	makeWorkspace(t, name, "taken")

	t.Run("missing source", func(t *testing.T) {
		if _, err := Rename(name, "nope", "whatever"); err == nil {
			t.Fatal("want error renaming a missing workspace")
		}
	})
	t.Run("same name", func(t *testing.T) {
		if _, err := Rename(name, "main", "main"); err == nil {
			t.Fatal("want error renaming to the same branch")
		}
	})
	t.Run("destination exists", func(t *testing.T) {
		if _, err := Rename(name, "main", "taken"); err == nil {
			t.Fatal("want error when destination workspace exists")
		}
		// Source is left intact when the rename is refused.
		if !Exists(name, "main") {
			t.Fatal("source workspace should be untouched after a refused rename")
		}
	})
	t.Run("unsafe new branch", func(t *testing.T) {
		if _, err := Rename(name, "main", "../escape"); err == nil {
			t.Fatal("want error for a traversing branch name")
		}
	})
	t.Run("target name taken by a local branch", func(t *testing.T) {
		// A second local branch in the "main" workspace, not a separate workspace.
		runOrFail(t, PathFor(name, "main"), "branch", "other")
		if _, err := Rename(name, "main", "other"); err == nil {
			t.Fatal("want error when a local branch of the new name already exists")
		}
		if !Exists(name, "main") {
			t.Fatal("source workspace should be untouched after a refused rename")
		}
	})
	t.Run("directory branch missing", func(t *testing.T) {
		// Directory named "drifted" but its checked-out branch renamed away, so no
		// local branch matches the directory name: refuse rather than rename HEAD.
		makeWorkspace(t, name, "drifted")
		runOrFail(t, PathFor(name, "drifted"), "branch", "-m", "drifted", "elsewhere")
		if _, err := Rename(name, "drifted", "whatever"); err == nil {
			t.Fatal("want error when no local branch matches the directory name")
		}
	})
}

// makeWorkspace creates a real git workspace on disk at the (name, branch)
// path, with branch as the checked-out branch and one commit, so Rename has
// something to operate on.
func makeWorkspace(t *testing.T, name, branch string) {
	t.Helper()
	p := PathFor(name, branch)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
	runOrFail(t, p, "init", "-q")
	runOrFail(t, p, "config", "user.email", "t@example.com")
	runOrFail(t, p, "config", "user.name", "t")
	runOrFail(t, p, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	runOrFail(t, p, "add", "f")
	runOrFail(t, p, "commit", "-q", "-m", "init")
}

func runOrFail(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func gitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cloneArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}
