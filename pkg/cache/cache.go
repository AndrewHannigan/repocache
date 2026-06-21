// Package cache inspects on-disk state of cache repos: existence, size,
// and the .git/repocache.meta sidecar.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// ErrLocked is returned when a cache repo's lock cannot be acquired in time.
var ErrLocked = errors.New("cache repo locked by another process")

// Meta is the JSON sidecar written at <cache>/.git/repocache.meta after a
// sync. LastSyncAt records the last *successful* sync; LastError/LastErrorAt
// record the most recent failed attempt (cleared on the next success).
type Meta struct {
	LastSyncAt  time.Time `json:"last_sync_at"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitempty"`
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

// Lock acquires a flock on the cache repo's lockfile. exclusive=true for
// sync, false (shared) for workspace creation. Caller must Unlock on
// release. Returns ErrLocked on timeout.
type Lock struct{ inner *flock.Flock }

func AcquireLock(name string, exclusive bool, timeout time.Duration) (*Lock, error) {
	p := paths.CacheRepoLockFile(name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}
	l := flock.New(p)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var (
		ok  bool
		err error
	)
	if exclusive {
		ok, err = l.TryLockContext(ctx, 100*time.Millisecond)
	} else {
		ok, err = l.TryRLockContext(ctx, 100*time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLocked
	}
	return &Lock{inner: l}, nil
}

func (l *Lock) Unlock() error { return l.inner.Unlock() }

// LockTree applies chmod -R a-w to the cache working tree, excluding .git/.
// The owner can always re-chmod to restore write later.
func LockTree(name string) error { return chmodTree(paths.CacheRepoPath(name), false) }

// UnlockTree applies chmod -R u+w to the cache working tree, excluding
// .git/, so a subsequent git checkout can write tracked files.
func UnlockTree(name string) error { return chmodTree(paths.CacheRepoPath(name), true) }

func chmodTree(root string, writable bool) error {
	gitDir := filepath.Join(root, ".git")
	gitDirPrefix := gitDir + string(filepath.Separator)
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == gitDir {
			return filepath.SkipDir
		}
		if strings.HasPrefix(p, gitDirPrefix) {
			return nil
		}
		mode := info.Mode().Perm()
		if writable {
			mode |= 0200
		} else {
			mode &^= 0222
		}
		// Skip if no change needed.
		if mode == info.Mode().Perm() {
			return nil
		}
		return os.Chmod(p, mode)
	})
}

// Remove deletes the cache repo directory from disk. It first restores
// write permissions on the working tree (sync leaves it chmod a-w, which
// would block os.RemoveAll from unlinking entries in read-only dirs),
// then takes the exclusive lock so a concurrent sync can't race the
// delete. Returns nil if the cache is already absent.
func Remove(name string, timeout time.Duration) error {
	if !Exists(name) {
		return nil
	}
	lock, err := AcquireLock(name, true, timeout)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	if err := UnlockTree(name); err != nil {
		return err
	}
	p := paths.CacheRepoPath(name)
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	paths.PruneEmptyDirs(filepath.Dir(p), paths.ReposDir())
	return nil
}

// Clone runs `git clone --no-checkout --config gc.auto=0 <url> <path>`.
// If the destination exists, treats it as success (race with another sync).
func Clone(url, name string) error {
	dest := paths.CacheRepoPath(name)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	cmd := exec.Command("git", "clone", "--no-checkout", "--config", "gc.auto=0", url, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Race: another process created the dir between our stat and clone.
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("git clone: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Fetch runs `git fetch --all --prune --tags` in the cache repo.
func Fetch(name string) error {
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name), "fetch", "--all", "--prune", "--tags")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CheckoutDetachedHEAD runs `git checkout --detach --force origin/HEAD` so
// the cache tree always reflects the default branch's tip without owning a
// local branch.
//
// --force makes the checkout self-healing: the cache is a read-only mirror
// the user never edits, so there are no local changes worth preserving.
// Without it, a tree left in a dirty state by an interrupted prior sync
// (stale index entries, untracked files in the way) would wedge every
// subsequent sync; --force discards that and resets to origin/HEAD.
//
// GIT_LFS_SKIP_SMUDGE=1 keeps the read-only mirror from invoking the LFS
// smudge filter: a cache only needs the committed pointer files, not the
// resolved blobs. Without it, checkout would fetch every LFS object on
// each sync, and a single missing object (e.g. pruned from the server)
// would fail the whole sync.
func CheckoutDetachedHEAD(name string) error {
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name), "checkout", "--detach", "--force", "origin/HEAD")
	cmd.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoteHEADResolves reports whether refs/remotes/origin/HEAD points at a
// real commit. It is false for an empty remote (no commits pushed), where
// `git checkout --detach origin/HEAD` would fail because origin/HEAD does
// not resolve to a commit-ish.
func RemoteHEADResolves(name string) (bool, error) {
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name),
		"rev-parse", "--verify", "--quiet", "origin/HEAD")
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return false, nil // ref doesn't resolve → empty remote, not a failure
	}
	return false, err // real exec error (git missing, etc.)
}
