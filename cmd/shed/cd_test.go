package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
)

// `shed cd <workspace> --path` prints the writable workspace path, located by
// the globally-unique workspace name alone.
func TestRunCdPathWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})
	want := makeWorkspaceDir(t, "github.com/AndrewHannigan/shed", "fix-thing")

	out := captureStdout(t, func() {
		if err := runCd("fix-thing", true); err != nil {
			t.Fatalf("runCd(fix-thing, --path) = %v, want nil", err)
		}
	})
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("runCd printed %q, want workspace path %q", got, want)
	}
}

// `shed cd <repo> --path` resolves a repo by the same shorthand the rest of shed
// uses (a trailing path segment) and prints the read-only store path.
func TestRunCdPathRepoByShorthand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	const repo = "github.com/AndrewHannigan/projects"
	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/projects"}},
	})
	// repostore.Exists only checks the store dir is present.
	if err := os.MkdirAll(paths.RepoStorePath(repo), 0755); err != nil {
		t.Fatalf("make store dir: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runCd("projects", true); err != nil {
			t.Fatalf("runCd(projects, --path) = %v, want nil", err)
		}
	})
	if got, want := strings.TrimSpace(out), paths.RepoStorePath(repo); got != want {
		t.Errorf("runCd printed %q, want repo store path %q", got, want)
	}
}

// A repo that is in the config but not yet synced (no store on disk) reports a
// NotFound with the `shed sync` fix, rather than cd-ing into a missing dir.
func TestRunCdRepoNotSynced(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/projects"}},
	})

	err := runCd("projects", true)
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.NotFound {
		t.Fatalf("runCd(projects) = %v, want errs.NotFound", err)
	}
	if !strings.Contains(err.Error(), "sync") {
		t.Errorf("error should point at `shed sync`, got: %v", err)
	}
}

func TestRunCdNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})

	err := runCd("nope", true)
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.NotFound {
		t.Fatalf("runCd(nope) = %v, want errs.NotFound", err)
	}
}

// The namespace guards keep a name from being both a repo and a workspace, but a
// library that predates them could. cd refuses such a name rather than guessing.
func TestRunCdAmbiguousBoth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/AndrewHannigan/projects"},
			{URL: "https://github.com/AndrewHannigan/shed"},
		},
	})
	// A workspace literally named "projects" living under a different repo, plus
	// the repo "github.com/AndrewHannigan/projects" that "projects" also resolves
	// to — the degenerate collision the guards normally prevent.
	makeWorkspaceDir(t, "github.com/AndrewHannigan/shed", "projects")

	err := runCd("projects", true)
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.Exists {
		t.Fatalf("runCd(projects) with a collision = %v, want errs.Exists", err)
	}
}

// repoNamesMatching mirrors config.Resolve: an exact name or an unambiguous
// trailing "/"-segment selects a repo; an unknown name selects none; a shared
// leaf across hosts selects several (an ambiguous reference).
func TestRepoNamesMatching(t *testing.T) {
	c := &config.Config{Repos: []config.Repo{
		{URL: "https://github.com/AndrewHannigan/projects"},
		{URL: "https://github.com/AndrewHannigan/shed"},
		{URL: "https://gitlab.com/someone/projects"},
	}}

	if got := repoNamesMatching(c, "shed"); len(got) != 1 || got[0] != "github.com/AndrewHannigan/shed" {
		t.Errorf(`repoNamesMatching("shed") = %v, want [github.com/AndrewHannigan/shed]`, got)
	}
	if got := repoNamesMatching(c, "github.com/AndrewHannigan/shed"); len(got) != 1 {
		t.Errorf(`repoNamesMatching(full name) = %v, want one match`, got)
	}
	if got := repoNamesMatching(c, "nope"); len(got) != 0 {
		t.Errorf(`repoNamesMatching("nope") = %v, want none`, got)
	}
	if got := repoNamesMatching(c, "projects"); len(got) != 2 {
		t.Errorf(`repoNamesMatching("projects") = %v, want two (ambiguous)`, got)
	}
}

// workspaceNamesShadowedBy is the add-side mirror: it reports existing
// workspaces a newly-added repo name would shadow under `shed cd`.
func TestWorkspaceNamesShadowedBy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})
	makeWorkspaceDir(t, "github.com/AndrewHannigan/shed", "projects")
	c := loadConfig(t)

	if got := workspaceNamesShadowedBy(c, "github.com/AndrewHannigan/projects"); len(got) != 1 || got[0] != "projects" {
		t.Errorf(`workspaceNamesShadowedBy(.../projects) = %v, want [projects]`, got)
	}
	if got := workspaceNamesShadowedBy(c, "github.com/AndrewHannigan/other"); len(got) != 0 {
		t.Errorf(`workspaceNamesShadowedBy(.../other) = %v, want none`, got)
	}
}

// `workspace new` refuses a workspace name that would collide with a repo name,
// failing fast (before the network sync) so `shed cd <name>` stays unambiguous.
func TestRunWorkspaceNewRejectsRepoNameCollision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/AndrewHannigan/projects"},
			{URL: "https://github.com/AndrewHannigan/shed"},
		},
	})

	// Make a workspace named "projects" under the shed repo; "projects" also
	// resolves to the projects repo, so the guard must reject it.
	err := runWorkspaceNew("shed", "projects", "")
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.Exists {
		t.Fatalf("runWorkspaceNew(shed, projects) = %v, want errs.Exists collision", err)
	}
	if !strings.Contains(err.Error(), "collides with repo") {
		t.Errorf("error should explain the repo-name collision, got: %v", err)
	}
}
