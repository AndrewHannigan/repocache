package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
)

// repoTestEnv points config + data dirs at temp dirs so the list reads an
// isolated library.
func repoTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// `shed repo ls` lists the library (owners + repos) but never the Workspaces
// section that top-level `shed ls` adds — that split is the whole reason the
// command exists.
func TestRepoOnlyListOmitsWorkspaces(t *testing.T) {
	repoTestEnv(t)
	saveConfig(t, &config.Config{
		Repos:  []config.Repo{{URL: "https://github.com/acme/widget"}},
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	})

	out := captureStdout(t, func() {
		if err := runRepoOnlyList(false); err != nil {
			t.Fatalf("runRepoOnlyList: %v", err)
		}
	})

	for _, want := range []string{"Owners", "acme", "Repos", "github.com/acme/widget"} {
		if !strings.Contains(out, want) {
			t.Errorf("repo ls output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Workspaces") {
		t.Errorf("repo ls must not render the Workspaces section:\n%s", out)
	}
}

// An empty library shows the same actionable hint top-level `ls` shows, not
// empty headers.
func TestRepoOnlyListEmpty(t *testing.T) {
	repoTestEnv(t)
	saveConfig(t, &config.Config{})

	out := captureStdout(t, func() {
		if err := runRepoOnlyList(false); err != nil {
			t.Fatalf("runRepoOnlyList: %v", err)
		}
	})
	if !strings.Contains(out, "nothing tracked yet") {
		t.Errorf("empty library should show the hint:\n%s", out)
	}
}

// `shed repo ls --json` emits owners + repos and, unlike top-level `ls --json`,
// no workspaces key.
func TestRepoOnlyListJSONHasNoWorkspaces(t *testing.T) {
	repoTestEnv(t)
	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget"}},
	})

	out := captureStdout(t, func() {
		if err := runRepoOnlyList(true); err != nil {
			t.Fatalf("runRepoOnlyList json: %v", err)
		}
	})
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("repo ls --json is not valid JSON: %v\n%s", err, out)
	}
	for _, key := range []string{"owners", "repos"} {
		if _, ok := got[key]; !ok {
			t.Errorf("repo ls --json missing %q key:\n%s", key, out)
		}
	}
	if _, ok := got["workspaces"]; ok {
		t.Errorf("repo ls --json must not include a workspaces key:\n%s", out)
	}
}

// repo add / repo rm change the library, so they are recorded in history just
// like the top-level add / rm they reuse; repo ls is read-only and is not.
func TestRepoSubcommandsRecorded(t *testing.T) {
	root := newRootCmd()
	cases := map[string]bool{
		"repo add": true,
		"repo rm":  true,
		"repo ls":  false,
	}
	for path, want := range cases {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatalf("find %q: %v", path, err)
		}
		if got := shouldRecord(cmd); got != want {
			t.Errorf("shouldRecord(%q) = %v, want %v", path, got, want)
		}
	}
}

// The `repos` alias resolves to the same command as `repo`.
func TestRepoAliasResolves(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"repos", "ls"})
	if err != nil {
		t.Fatalf("find repos ls: %v", err)
	}
	if cmd.CommandPath() != "shed repo ls" {
		t.Errorf("`repos` should alias `repo`, got command path %q", cmd.CommandPath())
	}
}
