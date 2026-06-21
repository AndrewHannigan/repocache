package main

import (
	"errors"
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/forge"
)

func TestNewOwnerRepos(t *testing.T) {
	c := &config.Config{
		Repos:  []config.Repo{{URL: "https://github.com/acme/existing"}},
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	}
	discovered := []forge.Repo{
		{Name: "existing", CloneURL: "https://github.com/acme/existing"}, // already tracked
		{Name: "new1", CloneURL: "https://github.com/acme/new1"},
		{Name: "new1again", CloneURL: "https://github.com/acme/new1"}, // dup within batch
		{Name: "new2", CloneURL: "https://github.com/acme/new2"},
		{Name: "empty", CloneURL: ""}, // skipped
	}
	got := newOwnerRepos(c, "github.com/acme", discovered)
	if len(got) != 2 {
		t.Fatalf("want 2 new repos, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Source != "github.com/acme" {
			t.Fatalf("expected Source tag, got %+v", r)
		}
	}
	if got[0].URL != "https://github.com/acme/new1" || got[1].URL != "https://github.com/acme/new2" {
		t.Fatalf("unexpected repos added: %+v", got)
	}
}

func TestOwnersInScope(t *testing.T) {
	c := &config.Config{
		Repos:  []config.Repo{{URL: "https://github.com/acme/a", Source: "github.com/acme"}},
		Owners: []config.Owner{{URL: "https://github.com/acme"}, {URL: "https://github.com/widgets"}},
	}
	if got := ownersInScope(c, nil); len(got) != 2 {
		t.Fatalf("no names should select all owners, got %d", len(got))
	}
	got := ownersInScope(c, []string{"acme"})
	if len(got) != 1 || got[0].URL != "https://github.com/acme" {
		t.Fatalf("owner name should select that owner, got %+v", got)
	}
	if got := ownersInScope(c, []string{"a"}); len(got) != 0 {
		t.Fatalf("a repo name should select no owners, got %+v", got)
	}
}

func TestResolveSyncTargetsExpandsOwners(t *testing.T) {
	c := &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/acme/a", Source: "github.com/acme"},
			{URL: "https://github.com/acme/b", Source: "github.com/acme"},
			{URL: "https://github.com/other/c"},
		},
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
	}

	targets, err := resolveSyncTargets(c, []string{"acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("owner arg should expand to its 2 repos, got %d: %+v", len(targets), targets)
	}

	targets, err = resolveSyncTargets(c, []string{"c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].name != "github.com/other/c" {
		t.Fatalf("repo arg should resolve to that repo, got %+v", targets)
	}

	if _, err := resolveSyncTargets(c, []string{"nope"}); err == nil {
		t.Fatal("unknown name should error")
	}
}

func TestReconcileOwnerAddsAndIsAdditive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	owner := config.Owner{URL: "https://github.com/acme"}
	if err := config.Save(&config.Config{Owners: []config.Owner{owner}}); err != nil {
		t.Fatal(err)
	}

	fake := func(url string, f forge.Filter) ([]forge.Repo, error) {
		return []forge.Repo{
			{Name: "a", CloneURL: "https://github.com/acme/a"},
			{Name: "b", CloneURL: "https://github.com/acme/b"},
		}, nil
	}

	added, err := reconcileOwner(owner, fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("want 2 added, got %v", added)
	}

	// Re-running with the same listing adds nothing (idempotent / additive).
	added2, err := reconcileOwner(owner, fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(added2) != 0 {
		t.Fatalf("second reconcile should add nothing, got %v", added2)
	}

	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ReposForOwner("github.com/acme"); len(got) != 2 {
		t.Fatalf("config should hold 2 managed repos, got %v", got)
	}
}

func TestReconcileOwnerGhErrorLeavesConfigUntouched(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	owner := config.Owner{URL: "https://github.com/acme"}
	if err := config.Save(&config.Config{Owners: []config.Owner{owner}}); err != nil {
		t.Fatal(err)
	}

	failing := func(url string, f forge.Filter) ([]forge.Repo, error) {
		return nil, forge.ErrGhMissing
	}
	_, err := reconcileOwner(owner, failing)
	if !errors.Is(err, forge.ErrGhMissing) {
		t.Fatalf("want ErrGhMissing, got %v", err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Repos) != 0 {
		t.Fatalf("gh failure must not add repos, got %+v", c.Repos)
	}
}
