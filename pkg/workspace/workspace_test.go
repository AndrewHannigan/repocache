package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

func TestLoadRepoUpdatedAt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	name := "github.com/acme/widgets"
	synced := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(paths.CacheRepoPath(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveMeta(name, &cache.Meta{LastSyncAt: synced}); err != nil {
		t.Fatal(err)
	}

	got := loadRepoUpdatedAt([]string{name, "github.com/acme/missing"})
	if !got[name].Equal(synced) {
		t.Fatalf("loadRepoUpdatedAt[%q] = %v, want %v", name, got[name], synced)
	}
	if _, ok := got["github.com/acme/missing"]; ok {
		t.Fatalf("missing repo should not appear in map, got %v", got)
	}
}

func TestListIncludesRepoUpdatedAt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	name := "github.com/acme/widgets"
	branch := "main"
	synced := time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC)

	if err := os.MkdirAll(paths.CacheRepoPath(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveMeta(name, &cache.Meta{LastSyncAt: synced}); err != nil {
		t.Fatal(err)
	}

	wsPath := paths.WorkspacePath(name, branch)
	if err := os.MkdirAll(filepath.Join(wsPath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	infos, err := List([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("List() returned %d infos, want 1", len(infos))
	}
	if !infos[0].RepoUpdatedAt.Equal(synced) {
		t.Fatalf("RepoUpdatedAt = %v, want %v", infos[0].RepoUpdatedAt, synced)
	}
}
