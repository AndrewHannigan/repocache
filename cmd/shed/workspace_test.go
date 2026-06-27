package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

// `workspace ls` lists workspaces most-recently-active first, so the one a user
// just touched sits at the top — matching the Workspaces section of `shed ls`.
func TestWriteWorkspaceListTableSortsByAge(t *testing.T) {
	now := time.Now()
	infos := []workspace.Info{
		{Name: "github.com/acme/widget", Branch: "old", Path: "/w/old", Age: now.Add(-3 * time.Hour)},
		{Name: "github.com/acme/widget", Branch: "new", Path: "/w/new", Age: now.Add(-1 * time.Minute)},
		{Name: "github.com/octo/hello", Branch: "mid", Path: "/w/mid", Age: now.Add(-1 * time.Hour)},
	}

	var buf bytes.Buffer
	if err := writeWorkspaceListTable(&buf, infos); err != nil {
		t.Fatalf("writeWorkspaceListTable: %v", err)
	}
	out := buf.String()

	iNew := strings.Index(out, "new")
	iMid := strings.Index(out, "mid")
	iOld := strings.Index(out, "old")
	if !(iNew < iMid && iMid < iOld) {
		t.Errorf("expected newest→oldest order (new, mid, old), got:\n%s", out)
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

// `workspace path` now takes just the globally-unique workspace name and
// resolves it to the one repo it lives under, the same lookup `rm`/`resume` use.
func TestRunWorkspacePathByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})
	const repo = "github.com/AndrewHannigan/shed"
	want := makeWorkspaceDir(t, repo, "fix-thing")

	out := captureStdout(t, func() {
		if err := runWorkspacePath("fix-thing"); err != nil {
			t.Fatalf("runWorkspacePath(fix-thing) = %v, want nil", err)
		}
	})
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("runWorkspacePath(fix-thing) printed %q, want %q", got, want)
	}
}

func TestRunWorkspacePathNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	saveConfig(t, &config.Config{
		Repos: []config.Repo{{URL: "https://github.com/AndrewHannigan/shed"}},
	})

	err := runWorkspacePath("nope")
	var c *errs.Coded
	if !errors.As(err, &c) || c.Code != errs.NotFound {
		t.Fatalf("runWorkspacePath(nope) = %v, want errs.NotFound", err)
	}
}
