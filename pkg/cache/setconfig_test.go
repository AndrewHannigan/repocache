package cache

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

func TestSetConfig(t *testing.T) {
	if err := RequireGit(); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	// Redirect the data dir (and thus the cache path) to a temp HOME.
	t.Setenv("HOME", t.TempDir())

	name := "github.com/foo/bar"
	repoPath := paths.CacheRepoPath(name)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repoPath, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}

	t.Run("empty map is a no-op", func(t *testing.T) {
		if err := SetConfig(name, nil); err != nil {
			t.Fatalf("SetConfig(nil) = %v", err)
		}
	})

	t.Run("sets and overwrites keys", func(t *testing.T) {
		if err := SetConfig(name, map[string]string{
			"user.email":     "me@work.com",
			"core.hooksPath": ".githooks",
		}); err != nil {
			t.Fatal(err)
		}
		assertGitConfig(t, repoPath, "user.email", "me@work.com")
		assertGitConfig(t, repoPath, "core.hooksPath", ".githooks")

		// Set/update: a second call overwrites the existing value.
		if err := SetConfig(name, map[string]string{"user.email": "me@home.com"}); err != nil {
			t.Fatal(err)
		}
		assertGitConfig(t, repoPath, "user.email", "me@home.com")
	})
}

func assertGitConfig(t *testing.T, repoPath, key, want string) {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", "--get", key).Output()
	if err != nil {
		t.Fatalf("git config --get %s: %v", key, err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
