// Package repostore inspects on-disk state of stored repos: existence, size,
// and the .git/shed.meta sidecar.
package repostore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// ErrLocked is returned when a stored repo's lock cannot be acquired in time.
var ErrLocked = errors.New("stored repo locked by another process")

// ErrGitMissing is returned by RequireGit when the git binary is not on PATH.
// git is shed's one hard dependency — every store and workspace operation
// shells out to it (clone, fetch, checkout) — so it is the one thing we cannot
// degrade around the way we do for gh (see pkg/forge).
var ErrGitMissing = errors.New("git not found on PATH — shed requires git (install from https://git-scm.com/downloads)")

// RequireGit reports whether the git binary is available on PATH, returning
// ErrGitMissing if not. Commands that shell out to git call this first so the
// user gets one clear, actionable message instead of a cryptic per-invocation
// "exec: \"git\": executable file not found in $PATH".
func RequireGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitMissing
	}
	return nil
}

// Meta is the JSON sidecar written at <store>/.git/shed.meta after a
// sync. LastSyncAt records the last *successful* sync; LastError/LastErrorAt
// record the most recent failed attempt (cleared on the next success).
// FirstSyncAt records the *first* successful sync — effectively when the repo
// landed in the library — and is set once and never overwritten, so callers
// can tell how recently a repo was added. Repos that predate this field have
// it zero; treat that as "added time unknown", not "added just now".
type Meta struct {
	LastSyncAt  time.Time `json:"last_sync_at"`
	FirstSyncAt time.Time `json:"first_sync_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitempty"`
}

// Exists returns true if the stored repo dir is present on disk.
func Exists(name string) bool {
	s, err := os.Stat(paths.RepoStorePath(name))
	return err == nil && s.IsDir()
}

// LoadMeta reads the meta sidecar. Returns (nil, nil) if absent (the
// repo hasn't been synced yet).
func LoadMeta(name string) (*Meta, error) {
	data, err := os.ReadFile(paths.RepoStoreMetaFile(name))
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

// RecordFirstSyncError persists errText for a repo that failed before its
// store dir (and meta sidecar) ever existed — i.e. a failed first clone.
// Without it the failure would vanish: LoadMeta has nothing to read, so
// `shed status` would report the repo healthy and the session-context banner
// would stay silent. The record lives in a standalone file outside ReposDir
// so it never makes Exists() or Clone() mistake it for a populated store.
func RecordFirstSyncError(name, errText string) error {
	m := &Meta{LastError: errText, LastErrorAt: time.Now().UTC()}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p := paths.SyncErrorFile(name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// LoadFirstSyncError reads a standalone first-sync failure record written by
// RecordFirstSyncError, or (nil, nil) when none exists.
func LoadFirstSyncError(name string) (*Meta, error) {
	data, err := os.ReadFile(paths.SyncErrorFile(name))
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

// ClearFirstSyncError removes any standalone first-sync failure record for
// name (best effort), pruning now-empty parent dirs. Called after a
// successful sync and on Remove so a stale record can't outlive the
// condition it described.
func ClearFirstSyncError(name string) {
	p := paths.SyncErrorFile(name)
	if err := os.Remove(p); err != nil {
		return
	}
	paths.PruneEmptyDirs(filepath.Dir(p), paths.SyncErrorDir())
}

// Reachable probes whether git can authenticate to and read from url without
// any interactive prompt. It runs `git ls-remote` with terminal and SSH
// prompts disabled, so a missing or wrong credential fails fast instead of
// blocking on stdin (which would hang an `add` or a session-start hook). A nil
// return means the remote is reachable with the credentials currently
// configured for whatever transport url names. A non-nil error wraps git's
// output so callers can classify it (auth vs. network vs. not-found).
func Reachable(url string) error {
	cmd := exec.Command("git", "ls-remote", "--heads", "--", url)
	// GIT_TERMINAL_PROMPT=0 stops HTTPS from prompting for username/password.
	// BatchMode=yes does the same for SSH (no passphrase/password prompt);
	// accept-new avoids hanging on an unknown host key the first time.
	// Respect a user's own GIT_SSH_COMMAND if they've set one.
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=accept-new")
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git ls-remote: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SaveMeta writes the meta sidecar.
func SaveMeta(name string, m *Meta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p := paths.RepoStoreMetaFile(name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// Size returns the total on-disk size of the stored repo in bytes. Walks
// the tree and sums file sizes; errors on individual files are ignored.
// Returns 0 if the store does not exist.
func Size(name string) (int64, error) {
	if !Exists(name) {
		return 0, nil
	}
	var total int64
	err := filepath.Walk(paths.RepoStorePath(name), func(_ string, info os.FileInfo, err error) error {
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

// Lock acquires a flock on the stored repo's lockfile. exclusive=true for
// sync, false (shared) for workspace creation. Caller must Unlock on
// release. Returns ErrLocked on timeout.
type Lock struct{ inner *flock.Flock }

func AcquireLock(name string, exclusive bool, timeout time.Duration) (*Lock, error) {
	p := paths.RepoStoreLockFile(name)
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

// LockTree applies chmod -R a-w to the store working tree, excluding .git/.
// The owner can always re-chmod to restore write later.
func LockTree(name string) error { return chmodTree(paths.RepoStorePath(name), false) }

// UnlockTree applies chmod -R u+w to the store working tree, excluding
// .git/, so a subsequent git checkout can write tracked files.
func UnlockTree(name string) error { return chmodTree(paths.RepoStorePath(name), true) }

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

// Remove deletes the stored repo directory from disk. It first restores
// write permissions on the working tree (sync leaves it chmod a-w, which
// would block os.RemoveAll from unlinking entries in read-only dirs),
// then takes the exclusive lock so a concurrent sync can't race the
// delete. Returns nil if the store is already absent.
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
	p := paths.RepoStorePath(name)
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	paths.PruneEmptyDirs(filepath.Dir(p), paths.ReposDir())
	// Drop any standalone first-sync failure record so a re-add starts clean.
	ClearFirstSyncError(name)
	return nil
}

// Clone runs `git clone --no-checkout --config gc.auto=0 <url> <path>`.
// If the destination exists, treats it as success (race with another sync).
func Clone(url, name string) error {
	dest := paths.RepoStorePath(name)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	// "--" terminates options so a url beginning with "-" can't be parsed as a
	// git flag (argument injection); url and dest are strictly positional.
	cmd := exec.Command("git", "clone", "--no-checkout", "--config", "gc.auto=0", "--", url, dest)
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

// SetConfig writes the given git config key/value pairs into the stored repo's
// local .git/config via `git config`. It is idempotent and set/update only:
// it overwrites an existing value but never unsets a key dropped from the map,
// so removing a key from config does not retroactively clear it (re-add the
// repo to fully reset). Keys are forwarded verbatim; callers validate them up
// front (config.ValidateGitConfigKey) so a key can't be parsed as a git flag.
// Keys are applied in sorted order for deterministic behavior.
func SetConfig(name string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	path := paths.RepoStorePath(name)
	for _, k := range keys {
		cmd := exec.Command("git", "-C", path, "config", k, kv[k])
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git config %s: %w (output: %s)", k, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// Fetch runs `git fetch --all --prune --tags` in the stored repo.
func Fetch(name string) error {
	cmd := exec.Command("git", "-C", paths.RepoStorePath(name), "fetch", "--all", "--prune", "--tags")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CheckoutDetachedHEAD runs `git checkout --detach --force origin/HEAD` so
// the store tree always reflects the default branch's tip without owning a
// local branch.
//
// --force makes the checkout self-healing: the store is a read-only store
// the user never edits, so there are no local changes worth preserving.
// Without it, a tree left in a dirty state by an interrupted prior sync
// (stale index entries, untracked files in the way) would wedge every
// subsequent sync; --force discards that and resets to origin/HEAD.
//
// GIT_LFS_SKIP_SMUDGE=1 keeps the read-only store from invoking the LFS
// smudge filter: a store only needs the committed pointer files, not the
// resolved blobs. Without it, checkout would fetch every LFS object on
// each sync, and a single missing object (e.g. pruned from the server)
// would fail the whole sync.
func CheckoutDetachedHEAD(name string) error {
	cmd := exec.Command("git", "-C", paths.RepoStorePath(name), "checkout", "--detach", "--force", "origin/HEAD")
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
	cmd := exec.Command("git", "-C", paths.RepoStorePath(name),
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
