package main

import (
	"os"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
)

// rmTestEnv points config + data dirs at temp dirs so rm can mutate them, and
// redirects stdin to /dev/null so the confirmation prompts deterministically
// take their non-interactive branch (untie for owners, refuse for repos)
// regardless of how the test runner's stdin is wired.
func rmTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	// A regular (empty) file is not a character device, so stdinIsTTY() reports
	// false and the prompts take their non-interactive branch. /dev/null would
	// not work here: it *is* a char device and reads as a TTY.
	f, err := os.Create(t.TempDir() + "/stdin")
	if err != nil {
		t.Fatalf("create fake stdin: %v", err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		f.Close()
	})
}

func saveConfig(t *testing.T, c *config.Config) {
	t.Helper()
	if err := config.Save(c); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return c
}

// sourceOf returns the Source of the repo with the given resolved name, plus
// whether the repo is present at all.
func sourceOf(t *testing.T, c *config.Config, name string) (string, bool) {
	t.Helper()
	r := c.FindByName(name)
	if r == nil {
		return "", false
	}
	return r.Source, true
}

// Owner removal without --force and without a TTY keeps the repos, clearing
// their Source so they become standalone, and drops only the owner entry.
func TestOwnerRmUntiesWhenNotConfirmed(t *testing.T) {
	rmTestEnv(t)
	saveConfig(t, &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/acme/a", Source: "github.com/acme"},
			{URL: "https://github.com/acme/b", Source: "github.com/acme"},
			{URL: "https://github.com/acme/z"}, // user-added, not owned
		},
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	})

	if err := runRepoRm("acme", false); err != nil {
		t.Fatalf("runRepoRm: %v", err)
	}

	c := loadConfig(t)
	if len(c.Owners) != 0 {
		t.Fatalf("owner should be removed, got %+v", c.Owners)
	}
	for _, name := range []string{"github.com/acme/a", "github.com/acme/b", "github.com/acme/z"} {
		src, ok := sourceOf(t, c, name)
		if !ok {
			t.Fatalf("repo %s should be kept", name)
		}
		if src != "" {
			t.Fatalf("repo %s should be untied (Source cleared), got %q", name, src)
		}
	}
}

// With --force, owner removal deletes the owner and every repo it managed,
// leaving user-added repos in place.
func TestOwnerRmForceRemovesManagedRepos(t *testing.T) {
	rmTestEnv(t)
	saveConfig(t, &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/acme/a", Source: "github.com/acme"},
			{URL: "https://github.com/acme/b", Source: "github.com/acme"},
			{URL: "https://github.com/acme/z"}, // user-added, survives
		},
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	})

	if err := runRepoRm("acme", true); err != nil {
		t.Fatalf("runRepoRm: %v", err)
	}

	c := loadConfig(t)
	if len(c.Owners) != 0 {
		t.Fatalf("owner should be removed, got %+v", c.Owners)
	}
	if len(c.Repos) != 1 {
		t.Fatalf("only the user-added repo should remain, got %+v", c.Repos)
	}
	if _, ok := sourceOf(t, c, "github.com/acme/z"); !ok {
		t.Fatalf("user-added repo should survive owner --force removal")
	}
}

// An owner with no managed repos is dropped outright, with no prompt.
func TestOwnerRmEmptyDropsEntry(t *testing.T) {
	rmTestEnv(t)
	saveConfig(t, &config.Config{
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	})

	if err := runRepoRm("acme", false); err != nil {
		t.Fatalf("runRepoRm: %v", err)
	}

	c := loadConfig(t)
	if len(c.Owners) != 0 {
		t.Fatalf("empty owner should be removed, got %+v", c.Owners)
	}
}

// A repo with no workspaces removes cleanly without a prompt, even without a
// TTY and without --force (there is nothing to confirm).
func TestRepoRmNoWorkspacesRemovesWithoutConfirm(t *testing.T) {
	rmTestEnv(t)
	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/z"}},
	})

	if err := runRepoRm("github.com/acme/z", false); err != nil {
		t.Fatalf("runRepoRm: %v", err)
	}

	c := loadConfig(t)
	if len(c.Repos) != 0 {
		t.Fatalf("repo should be removed, got %+v", c.Repos)
	}
}
