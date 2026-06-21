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
