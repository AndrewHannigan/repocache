package main

import "testing"

// decidePrune gates `prune`: remove only merged branches, and never discard
// local work unless --force.
func TestDecidePrune(t *testing.T) {
	tests := []struct {
		name     string
		prNumber int
		dirty    bool
		unpushed int
		force    bool
		want     pruneAction
	}{
		{"no merged PR", 0, false, 0, false, pruneKeep},
		{"no merged PR even if dirty", 0, true, 3, false, pruneKeep},
		{"merged and clean", 12, false, 0, false, pruneRemove},
		{"merged, no upstream", 12, false, -1, false, pruneRemove},
		{"merged but dirty", 12, true, 0, false, pruneSkip},
		{"merged but unpushed", 12, false, 2, false, pruneSkip},
		{"merged, dirty, forced", 12, true, 2, true, pruneRemove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decidePrune(tt.prNumber, tt.dirty, tt.unpushed, tt.force); got != tt.want {
				t.Errorf("decidePrune(%d, %v, %d, %v) = %d, want %d",
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
