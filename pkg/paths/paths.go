// Package paths centralizes every on-disk location repocache touches.
// All functions return absolute paths.
package paths

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const appName = "repocache"

// ConfigDir returns ~/.config/repocache (honoring XDG_CONFIG_HOME).
func ConfigDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, appName)
	}
	return filepath.Join(home(), ".config", appName)
}

// DataDir returns ~/.repocache.
func DataDir() string {
	return filepath.Join(home(), "."+appName)
}

func ConfigFile() string     { return filepath.Join(ConfigDir(), "config.toml") }
func ConfigLockFile() string { return filepath.Join(ConfigDir(), ".lock") }

func ReposDir() string      { return filepath.Join(DataDir(), "repos") }
func WorkspacesDir() string { return filepath.Join(DataDir(), "workspaces") }
func LogsDir() string       { return filepath.Join(DataDir(), "logs") }

func BgSyncLockFile() string { return filepath.Join(DataDir(), ".bg-sync.lock") }
func BgSyncLogFile() string  { return filepath.Join(LogsDir(), "bg-sync.log") }

// HistoryFile is the JSON-Lines log of recent repocache commands (one event
// per line). HistoryTrimMarkerFile holds the RFC3339 timestamp of the last
// trim check, used to debounce truncation of the history file.
func HistoryFile() string           { return filepath.Join(DataDir(), "history.jsonl") }
func HistoryTrimMarkerFile() string { return filepath.Join(DataDir(), ".history-trim") }

// CacheRepoPath returns the on-disk path for a named cache repo
// (e.g. "github.com/foo/bar" → "<DataDir>/repos/github.com/foo/bar").
func CacheRepoPath(name string) string {
	return filepath.Join(ReposDir(), filepath.FromSlash(name))
}

func CacheRepoLockFile(name string) string {
	return filepath.Join(CacheRepoPath(name), ".git", "repocache.lock")
}

func CacheRepoMetaFile(name string) string {
	return filepath.Join(CacheRepoPath(name), ".git", "repocache.meta")
}

// WorkspacePath returns the on-disk path for a (repo, branch) workspace.
// Branch with slashes becomes nested dirs.
func WorkspacePath(name, branch string) string {
	return filepath.Join(WorkspacesDir(), filepath.FromSlash(name), filepath.FromSlash(branch))
}

// checkSafeRelPath verifies p is a relative, slash-separated path that cannot
// escape a base directory once joined into one: no absolute prefix, no Windows
// volume/backslash, and no ".." or empty segment. Repo names and branches are
// always "/"-separated regardless of host OS, so we split on "/" rather than
// the OS separator. Without this, a name or branch like "../../etc/x" would
// make filepath.Join resolve outside ReposDir/WorkspacesDir.
func checkSafeRelPath(p string) error {
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return errors.New("must be a relative path")
	}
	if strings.ContainsRune(p, '\\') {
		return errors.New("must not contain backslashes")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "..":
			return errors.New(`must not contain a ".." segment`)
		case "":
			return errors.New("must not contain empty path segments")
		}
	}
	return nil
}

// ValidateName reports an error when name is not a safe relative repo name —
// one that, joined under ReposDir or WorkspacesDir, could escape it. Called
// when names enter config (user `--name` overrides, URL-derived defaults, and
// owner-discovered repos) so a traversing name is rejected before it ever
// reaches a path.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("repo name is empty")
	}
	if err := checkSafeRelPath(name); err != nil {
		return fmt.Errorf("repo name %q is unsafe: %w", name, err)
	}
	return nil
}

// ValidateBranch is ValidateName's analog for branch names, which become
// nested directories under a workspace and must likewise stay contained. It
// additionally rejects a leading "-" so the branch can't be parsed as an
// option when passed to git (e.g. `git clone --branch`, `git checkout -b`);
// git refs cannot begin with "-" anyway.
func ValidateBranch(branch string) error {
	if branch == "" {
		return errors.New("branch is empty")
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("branch %q is unsafe: must not start with %q", branch, "-")
	}
	if err := checkSafeRelPath(branch); err != nil {
		return fmt.Errorf("branch %q is unsafe: %w", branch, err)
	}
	return nil
}

// WriteFileAtomic writes data to path atomically: it writes a sibling temp
// file (same directory, so the rename stays on one filesystem) and renames it
// over path, so a reader or a crash never sees a half-written file. When path
// already exists its permission bits are preserved; otherwise defaultPerm is
// used. The temp file is chmod'd to the chosen mode before the rename, so the
// result does not depend on the process umask, and the temp is cleaned up if a
// later step fails.
func WriteFileAtomic(path string, data []byte, defaultPerm os.FileMode) error {
	perm := defaultPerm
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// NormalizeURL expands a user-supplied repo reference into a full git URL.
// Full URLs (anything with a "://" scheme) and scp-style remotes
// (git@host:path) are returned unchanged. A bare reference with no scheme is
// treated as shorthand and expanded so the common cases just work:
//
//	octocat                         -> https://github.com/octocat
//	octocat/Hello-World             -> https://github.com/octocat/Hello-World
//	github.com/octocat              -> https://github.com/octocat
//	gitlab.com/foo/bar              -> https://gitlab.com/foo/bar
//
// A leading segment that looks like a host (contains "." or ":") is taken as
// the host and only given an https:// scheme; otherwise the reference is
// GitHub shorthand (owner or owner/repo) and is resolved against github.com.
func NormalizeURL(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return s
	}
	// Already a full URL (https://, ssh://, git://) or scp-style git@host:path.
	if strings.Contains(s, "://") || isSCPLike(s) {
		return s
	}
	s = strings.Trim(s, "/")
	// A leading segment that looks like a host (a dot, or a host:port colon)
	// means the user gave host/owner[/repo] without a scheme; just add https://.
	if first, _, _ := strings.Cut(s, "/"); strings.ContainsAny(first, ".:") {
		return "https://" + s
	}
	// Otherwise it's GitHub shorthand: owner or owner/repo.
	return "https://github.com/" + s
}

// isSCPLike reports whether s is a scp-style remote (user@host:path with no
// scheme), the one no-scheme form NormalizeURL must leave untouched. Mirrors
// the detection in ParseURL.
func isSCPLike(s string) bool {
	if at := strings.Index(s, "@"); at > 0 {
		return strings.Contains(s[at+1:], ":")
	}
	return false
}

// ParseURL extracts (host, path) from a git URL. Handles both standard
// URLs (https://, ssh://, git://) and scp-style (git@github.com:foo/bar.git).
// Path has any trailing ".git" stripped.
func ParseURL(rawURL string) (host, path string, err error) {
	// scp-style: user@host:path (no scheme, host is before ":")
	if !strings.Contains(rawURL, "://") {
		if at := strings.Index(rawURL, "@"); at > 0 {
			rest := rawURL[at+1:]
			if colon := strings.Index(rest, ":"); colon > 0 {
				host = rest[:colon]
				path = strings.TrimPrefix(rest[colon+1:], "/")
				path = strings.TrimSuffix(path, ".git")
				if host == "" || path == "" {
					return "", "", fmt.Errorf("could not parse host or path from %q", rawURL)
				}
				return host, path, nil
			}
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse URL %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("missing host in URL %q", rawURL)
	}
	host = u.Host
	path = strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" {
		return "", "", fmt.Errorf("missing path in URL %q", rawURL)
	}
	return host, path, nil
}

// DefaultName returns "host/path" as the default repo name for a URL.
func DefaultName(rawURL string) (string, error) {
	host, path, err := ParseURL(rawURL)
	if err != nil {
		return "", err
	}
	return host + "/" + path, nil
}

// IsOwnerURL reports whether a git URL points at a bare owner (user or org)
// rather than a specific repo — i.e. its path is a single segment
// ("github.com/octocat") with no "<owner>/<repo>" tail. Returns an
// error only if the URL itself cannot be parsed.
func IsOwnerURL(rawURL string) (bool, error) {
	_, path, err := ParseURL(rawURL)
	if err != nil {
		return false, err
	}
	return !strings.Contains(path, "/"), nil
}

// DefaultOwnerName returns "host/owner" as the default name for an owner URL.
// It errors if the URL's path has more than one segment (i.e. it looks like a
// repo URL, not an owner URL).
func DefaultOwnerName(rawURL string) (string, error) {
	host, path, err := ParseURL(rawURL)
	if err != nil {
		return "", err
	}
	if strings.Contains(path, "/") {
		return "", fmt.Errorf("URL %q looks like a repo, not an owner (path %q has multiple segments)", rawURL, path)
	}
	return host + "/" + path, nil
}

// PruneEmptyDirs removes dir and each now-empty ancestor, walking up
// until (but not including) stop. It stops at the first directory that
// is non-empty or cannot be removed. Used to clean up the intermediate
// host/owner dirs left behind when a repo's leaf dir is deleted.
func PruneEmptyDirs(dir, stop string) {
	stopPrefix := stop + string(filepath.Separator)
	for dir != stop && strings.HasPrefix(dir, stopPrefix) {
		if err := os.Remove(dir); err != nil {
			return // non-empty or gone; nothing more to prune
		}
		dir = filepath.Dir(dir)
	}
}

// Display returns a path with $HOME collapsed to "~" for human-readable output.
func Display(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return p
	}
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(os.PathSeparator)) {
		return "~" + p[len(h):]
	}
	return p
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		// UserHomeDir can fail in pathological envs; fall back to $HOME
		// or "/" as a last resort. Don't panic — callers will fail later
		// with a clearer error when paths don't resolve.
		if h := os.Getenv("HOME"); h != "" {
			return h
		}
		return "/"
	}
	return h
}
