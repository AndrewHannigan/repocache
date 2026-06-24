package main

import (
	"testing"
	"time"
)

// decidePrune gates `prune`: remove only reclaimable branches, and never
// discard local work unless --force.
func TestDecidePrune(t *testing.T) {
	tests := []struct {
		name     string
		prunable bool
		dirty    bool
		unpushed int
		force    bool
		want     pruneAction
	}{
		{"not reclaimable", false, false, 0, false, pruneKeep},
		{"not reclaimable even if dirty", false, true, 3, false, pruneKeep},
		{"reclaimable and clean", true, false, 0, false, pruneRemove},
		{"reclaimable, no upstream", true, false, -1, false, pruneRemove},
		{"reclaimable but dirty", true, true, 0, false, pruneSkip},
		{"reclaimable but unpushed", true, false, 2, false, pruneSkip},
		{"reclaimable, dirty, forced", true, true, 2, true, pruneRemove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decidePrune(tt.prunable, tt.dirty, tt.unpushed, tt.force); got != tt.want {
				t.Errorf("decidePrune(%v, %v, %d, %v) = %d, want %d",
					tt.prunable, tt.dirty, tt.unpushed, tt.force, got, tt.want)
			}
		})
	}
}

// pruneReason reports the highest-priority reason a workspace is reclaimable.
func TestPruneReason(t *testing.T) {
	tests := []struct {
		name          string
		prNumber      int
		landed        bool
		defaultBranch string
		expired       bool
		inactive      time.Duration
		want          string
	}{
		{"merged PR wins", 12, true, "main", true, 100 * 24 * time.Hour, "PR #12 merged"},
		{"landed into named default", 0, true, "main", false, 0, "merged into main"},
		{"landed, default unknown", 0, true, "", false, 0, "merged into default branch"},
		{"expired only", 0, false, "main", true, 100 * 24 * time.Hour, "inactive for 100 days"},
		{"nothing", 0, false, "main", false, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pruneReason(tt.prNumber, tt.landed, tt.defaultBranch, tt.expired, tt.inactive); got != tt.want {
				t.Errorf("pruneReason(%d, %v, %q, %v, %v) = %q, want %q",
					tt.prNumber, tt.landed, tt.defaultBranch, tt.expired, tt.inactive, got, tt.want)
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
