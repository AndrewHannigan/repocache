// Package workspace handles creation, inspection, and removal of
// writable workspaces derived from cache repos.
package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

const cacheLockTimeout = 2 * time.Second

// Info is a single workspace's state for listing.
type Info struct {
	Name     string    `json:"name"`     // repo name e.g. "github.com/foo/bar"
	Branch   string    `json:"branch"`
	Path     string    `json:"path"`
	Dirty    bool      `json:"dirty"`
	Unpushed int       `json:"unpushed"` // -1 if no upstream set
	Age      time.Time `json:"age_mtime"`
}

// PathFor returns the absolute workspace path. Does not check existence.
func PathFor(name, branch string) string {
	return paths.WorkspacePath(name, branch)
}

// Exists returns true if the workspace dir contains a .git directory.
func Exists(name, branch string) bool {
	p := PathFor(name, branch)
	s, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil && s.IsDir()
}

// New creates a new workspace via `git clone --reference`. Returns the
// absolute workspace path on success.
//
// If branch exists on the cache's origin refs, checks it out. Otherwise
// clones starting from base (or origin/HEAD if base is empty) and
// creates a new local branch named branch.
func New(name, branch, base, url string) (string, error) {
	if !cache.Exists(name) {
		return "", fmt.Errorf("cache repo not present; run `repocache sync %s` first", name)
	}
	wsPath := PathFor(name, branch)
	if _, err := os.Stat(wsPath); err == nil {
		return "", fmt.Errorf("workspace already exists at %s", wsPath)
	}

	lock, err := cache.AcquireLock(name, false, cacheLockTimeout)
	if err != nil {
		return "", err
	}
	defer lock.Unlock()

	branchExists, err := refExists(name, "refs/remotes/origin/"+branch)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(wsPath), 0755); err != nil {
		return "", err
	}

	cachePath := paths.CacheRepoPath(name)
	if branchExists {
		if err := runGitClone(cachePath, url, branch, wsPath); err != nil {
			return "", err
		}
	} else {
		baseBranch := base
		if baseBranch == "" {
			baseBranch, err = defaultBranch(name)
			if err != nil {
				return "", err
			}
		}
		if err := runGitClone(cachePath, url, baseBranch, wsPath); err != nil {
			return "", err
		}
		if err := runGit(wsPath, "checkout", "-b", branch); err != nil {
			return "", err
		}
	}
	return wsPath, nil
}

func runGitClone(referencePath, url, branch, dest string) error {
	cmd := exec.Command("git", "clone",
		"--reference", referencePath,
		"--branch", branch,
		url, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w (output: %s)", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

func refExists(name, ref string) (bool, error) {
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name),
		"show-ref", "--verify", "--quiet", ref)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func defaultBranch(name string) (string, error) {
	cmd := exec.Command("git", "-C", paths.CacheRepoPath(name),
		"symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not resolve origin/HEAD: %w", err)
	}
	full := strings.TrimSpace(string(out))
	return strings.TrimPrefix(full, "refs/remotes/origin/"), nil
}

// List returns all workspaces present on disk, scoped to the given repo
// names (so the repo/branch split is unambiguous).
func List(repoNames []string) ([]Info, error) {
	var out []Info
	for _, name := range repoNames {
		repoDir := filepath.Join(paths.WorkspacesDir(), filepath.FromSlash(name))
		if _, err := os.Stat(repoDir); err != nil {
			continue
		}
		walkErr := filepath.Walk(repoDir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() {
				return nil
			}
			// A dir containing .git is a workspace root. Don't recurse further.
			if s, err := os.Stat(filepath.Join(p, ".git")); err == nil && s.IsDir() {
				rel, err := filepath.Rel(repoDir, p)
				if err != nil || rel == "." {
					return nil
				}
				out = append(out, infoFor(name, filepath.ToSlash(rel), p))
				return filepath.SkipDir
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return out, nil
}

func infoFor(name, branch, path string) Info {
	i := Info{Name: name, Branch: branch, Path: path, Unpushed: -1}
	i.Dirty, _ = isDirty(path)
	if n, ok := unpushedCount(path); ok {
		i.Unpushed = n
	}
	i.Age = newestMtime(path)
	return i
}

func isDirty(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// unpushedCount returns (count, true) if the branch has an upstream;
// (0, false) if no upstream is configured.
func unpushedCount(path string) (int, bool) {
	cmd := exec.Command("git", "-C", path, "rev-list", "--count", "@{u}..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return n, true
}

func newestMtime(path string) time.Time {
	var newest time.Time
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

// CheckClean returns (dirty, unpushed, error). If the workspace is
// clean, returns (false, 0, nil).
func CheckClean(path string) (bool, int, error) {
	dirty, err := isDirty(path)
	if err != nil {
		return false, 0, err
	}
	unpushed, ok := unpushedCount(path)
	if !ok {
		unpushed = 0 // no upstream → no unpushed commits to report
	}
	return dirty, unpushed, nil
}

// Remove deletes the workspace dir.
func Remove(name, branch string) error {
	p := PathFor(name, branch)
	if !Exists(name, branch) {
		return fmt.Errorf("workspace not found at %s", p)
	}
	return os.RemoveAll(p)
}
