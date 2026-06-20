// Package paths centralizes every on-disk location repocache touches.
// All functions return absolute paths.
package paths

import (
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

// DataDir returns ~/.local/share/repocache (honoring XDG_DATA_HOME).
func DataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, appName)
	}
	return filepath.Join(home(), ".local", "share", appName)
}

func ConfigFile() string     { return filepath.Join(ConfigDir(), "config.toml") }
func ConfigLockFile() string { return filepath.Join(ConfigDir(), ".lock") }

func ReposDir() string      { return filepath.Join(DataDir(), "repos") }
func WorkspacesDir() string { return filepath.Join(DataDir(), "workspaces") }
func LogsDir() string       { return filepath.Join(DataDir(), "logs") }

func BgSyncLockFile() string { return filepath.Join(DataDir(), ".bg-sync.lock") }
func BgSyncLogFile() string  { return filepath.Join(LogsDir(), "bg-sync.log") }

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
