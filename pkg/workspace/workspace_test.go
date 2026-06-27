package workspace

import (
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

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cloneArgs mismatch\n got: %v\nwant: %v", got, want)
	}
}
