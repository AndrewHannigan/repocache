package main

import (
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/config"
)

// resolveRepoName powers the shorthand acceptance in `workspace path`/`rm`,
// matching what `workspace new` already does via config.Resolve.
func TestResolveRepoName(t *testing.T) {
	c := &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/AndrewHannigan/repocache"},
			{URL: "https://github.com/acme/widgets"},
			{URL: "https://github.com/other/widgets"}, // shares leaf "widgets"
		},
	}

	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"shorthand leaf", "repocache", "github.com/AndrewHannigan/repocache", true},
		{"full name", "github.com/AndrewHannigan/repocache", "github.com/AndrewHannigan/repocache", true},
		{"unknown", "nope", "", false},
		{"ambiguous leaf", "widgets", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveRepoName(c, tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("resolveRepoName(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// decideGc gates `workspace gc`: prune only merged branches, and never discard
// local work unless --force.
func TestDecideGc(t *testing.T) {
	tests := []struct {
		name     string
		prNumber int
		dirty    bool
		unpushed int
		force    bool
		want     gcAction
	}{
		{"no merged PR", 0, false, 0, false, gcKeep},
		{"no merged PR even if dirty", 0, true, 3, false, gcKeep},
		{"merged and clean", 12, false, 0, false, gcPrune},
		{"merged, no upstream", 12, false, -1, false, gcPrune},
		{"merged but dirty", 12, true, 0, false, gcSkip},
		{"merged but unpushed", 12, false, 2, false, gcSkip},
		{"merged, dirty, forced", 12, true, 2, true, gcPrune},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideGc(tt.prNumber, tt.dirty, tt.unpushed, tt.force); got != tt.want {
				t.Errorf("decideGc(%d, %v, %d, %v) = %d, want %d",
					tt.prNumber, tt.dirty, tt.unpushed, tt.force, got, tt.want)
			}
		})
	}
}

// ghRepoFromName turns a workspace's "host/owner/repo" name into the host and
// "owner/repo" slug gh needs.
func TestGhRepoFromName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantHost string
		wantRepo string
		wantOK   bool
	}{
		{"github", "github.com/AndrewHannigan/repocache", "github.com", "AndrewHannigan/repocache", true},
		{"enterprise host", "ghe.acme.com/team/widgets", "ghe.acme.com", "team/widgets", true},
		{"nested repo path", "github.com/acme/group/widgets", "github.com", "acme/group/widgets", true},
		{"no repo segment", "github.com/owneronly", "", "", false},
		{"host only", "github.com", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, ok := ghRepoFromName(tt.in)
			if host != tt.wantHost || repo != tt.wantRepo || ok != tt.wantOK {
				t.Errorf("ghRepoFromName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.in, host, repo, ok, tt.wantHost, tt.wantRepo, tt.wantOK)
			}
		})
	}
}
