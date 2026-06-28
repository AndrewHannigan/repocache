package main

import (
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
)

// A just-added owner that manages at least one repo is working as intended, so
// no advisory is emitted.
func TestOwnerEmptyHintWithRepos(t *testing.T) {
	c := &config.Config{
		Owners: []config.Owner{{URL: "https://github.com/acme"}},
		Repos: []config.Repo{
			{URL: "https://github.com/acme/widget", Source: "github.com/acme"},
		},
	}
	if hint := ownerEmptyHint(c, "github.com/acme"); hint != "" {
		t.Fatalf("owner with a repo should produce no hint, got %q", hint)
	}
}

// An owner that exists (add validates that) but resolved to zero repos is the
// benign "empty owner" case; the hint names the owner and points at `shed rm`
// so an unexpected empty entry is easy to undo.
func TestOwnerEmptyHintNoRepos(t *testing.T) {
	c := &config.Config{
		Owners: []config.Owner{{URL: "https://github.com/emptyorg"}},
	}
	hint := ownerEmptyHint(c, "github.com/emptyorg")
	if hint == "" {
		t.Fatal("owner with no repos should produce a hint")
	}
	if !strings.Contains(hint, "github.com/emptyorg") {
		t.Fatalf("hint should name the owner, got %q", hint)
	}
	if !strings.Contains(hint, "shed rm github.com/emptyorg") {
		t.Fatalf("hint should suggest `shed rm <owner>`, got %q", hint)
	}
}

// Only the named owner's repos count: an owner with no repos still warns even
// when other owners have plenty, so one populated owner can't mask an empty one.
func TestOwnerEmptyHintScopedToOwner(t *testing.T) {
	c := &config.Config{
		Owners: []config.Owner{
			{URL: "https://github.com/acme"},
			{URL: "https://github.com/empty"},
		},
		Repos: []config.Repo{
			{URL: "https://github.com/acme/widget", Source: "github.com/acme"},
		},
	}
	if hint := ownerEmptyHint(c, "github.com/empty"); hint == "" {
		t.Fatal("an empty owner should warn even when another owner has repos")
	}
}
