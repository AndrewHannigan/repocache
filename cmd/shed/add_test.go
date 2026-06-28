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

// An owner that resolved to zero repos is the signature of a mistyped name
// (e.g. "langchain" for the org "langchain-ai"); the hint names the owner and
// points at `shed rm` so the dead entry is easy to undo.
func TestOwnerEmptyHintNoRepos(t *testing.T) {
	c := &config.Config{
		Owners: []config.Owner{{URL: "https://github.com/klnaselfhuasef"}},
	}
	hint := ownerEmptyHint(c, "github.com/klnaselfhuasef")
	if hint == "" {
		t.Fatal("owner with no repos should produce a hint")
	}
	if !strings.Contains(hint, "github.com/klnaselfhuasef") {
		t.Fatalf("hint should name the owner, got %q", hint)
	}
	if !strings.Contains(hint, "shed rm github.com/klnaselfhuasef") {
		t.Fatalf("hint should suggest `shed rm <owner>`, got %q", hint)
	}
}

// Only the named owner's repos count: an owner with no repos still warns even
// when other owners have plenty, so one populated owner can't mask a typo'd one.
func TestOwnerEmptyHintScopedToOwner(t *testing.T) {
	c := &config.Config{
		Owners: []config.Owner{
			{URL: "https://github.com/acme"},
			{URL: "https://github.com/typo"},
		},
		Repos: []config.Repo{
			{URL: "https://github.com/acme/widget", Source: "github.com/acme"},
		},
	}
	if hint := ownerEmptyHint(c, "github.com/typo"); hint == "" {
		t.Fatal("an empty owner should warn even when another owner has repos")
	}
}
