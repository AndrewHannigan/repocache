// Package cache inspects on-disk state of cache repos: existence, size,
// branch count, and the .git/repocache.meta sidecar.
package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// Meta is the JSON sidecar written at <cache>/.git/repocache.meta after
// each successful sync.
type Meta struct {
	LastSyncAt time.Time `json:"last_sync_at"`
}

// Exists returns true if the cache repo dir is present on disk.
func Exists(name string) bool {
	s, err := os.Stat(paths.CacheRepoPath(name))
	return err == nil && s.IsDir()
}

// LoadMeta reads the meta sidecar. Returns (nil, nil) if absent (the
// repo hasn't been synced yet).
func LoadMeta(name string) (*Meta, error) {
	data, err := os.ReadFile(paths.CacheRepoMetaFile(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveMeta writes the meta sidecar.
func SaveMeta(name string, m *Meta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p := paths.CacheRepoMetaFile(name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// Size returns the total on-disk size of the cache repo in bytes. Walks
// the tree and sums file sizes; errors on individual files are ignored.
// Returns 0 if the cache does not exist.
func Size(name string) (int64, error) {
	if !Exists(name) {
		return 0, nil
	}
	var total int64
	err := filepath.Walk(paths.CacheRepoPath(name), func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// BranchCount returns the number of remote-tracking branches under
// refs/remotes/origin (excluding the symbolic HEAD). Returns 0 if the
// cache does not exist.
func BranchCount(name string) (int, error) {
	if !Exists(name) {
		return 0, nil
	}
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name),
		"for-each-ref", "--format=%(refname)", "refs/remotes/origin")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte{'\n'}) {
		if len(line) == 0 || bytes.HasSuffix(line, []byte("/HEAD")) {
			continue
		}
		count++
	}
	return count, nil
}
