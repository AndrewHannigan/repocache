package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

// resolveRepoName powers the shorthand acceptance in `workspace path`/`rm`,
// matching what `workspace new` already does via config.Resolve.
func TestResolveRepoName(t *testing.T) {
	c := &config.Config{
		Repos: []config.Repo{
			{URL: "https://github.com/AndrewHannigan/shed"},
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
		{"shorthand leaf", "shed", "github.com/AndrewHannigan/shed", true},
		{"full name", "github.com/AndrewHannigan/shed", "github.com/AndrewHannigan/shed", true},
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

// makeWorkspaceDir creates a minimal workspace dir (with a .git subdir) so
// workspace.Exists / LocateByName treat it as a real workspace.
func makeWorkspaceDir(t *testing.T, repo, name string) string {
	t.Helper()
	p := paths.WorkspacePath(repo, name)
	if err := os.MkdirAll(filepath.Join(p, ".git"), 0755); err != nil {
		t.Fatalf("make workspace dir: %v", err)
	}
	return p
}

// `workspace rm` now takes just the globally-unique workspace name and resolves
// it to the one repo it lives under, the same lookup `shed resume` uses.
func TestRunWorkspaceRmByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})
	const repo = "github.com/AndrewHannigan/shed"
	p := makeWorkspaceDir(t, repo, "fix-thing")

	// --force skips the clean check (the bare dir isn't a real git repo).
	captureStdout(t, func() {
		if err := runWorkspaceRm("fix-thing", true); err != nil {
			t.Fatalf("runWorkspaceRm(fix-thing) = %v, want nil", err)
		}
	})
	if workspace.Exists(repo, "fix-thing") {
		t.Errorf("workspace dir %s still exists after rm", p)
	}
}

func TestRunWorkspaceRmNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})

	err := runWorkspaceRm("nope", false)
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.NotFound {
		t.Fatalf("runWorkspaceRm(nope) = %v, want errs.NotFound", err)
	}
}
