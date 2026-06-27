// Package workspace handles creation, inspection, and removal of
// writable workspaces derived from stored repos.
package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
)

const storeLockTimeout = 2 * time.Second

// Info is a single workspace's state for listing.
type Info struct {
	Name     string    `json:"name"` // repo name e.g. "github.com/foo/bar"
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
// If branch exists on the store's origin refs, checks it out. Otherwise
// clones starting from base (or origin/HEAD if base is empty) and
// creates a new local branch named branch.
//
// gitConfig is seeded into the new workspace's .git/config at clone time via
// `git clone --config`, so the repo's configured git options apply to every
// later git command in the workspace — including the user's own. Keys are
// validated by config before reaching here.
func New(name, branch, base, url string, gitConfig map[string]string) (string, error) {
	// Guard the path-forming inputs so a name/branch can't escape WorkspacesDir
	// (filepath.Join would resolve a ".." away). base only ever becomes a git
	// ref, but validating it too keeps option-injection out of `git clone
	// --branch`.
	if err := paths.ValidateName(name); err != nil {
		return "", err
	}
	if err := paths.ValidateBranch(branch); err != nil {
		return "", err
	}
	if base != "" {
		if err := paths.ValidateBranch(base); err != nil {
			return "", err
		}
	}
	if !repostore.Exists(name) {
		return "", fmt.Errorf("stored repo not present; run `shed sync %s` first", name)
	}
	wsPath := PathFor(name, branch)
	if _, err := os.Stat(wsPath); err == nil {
		return "", fmt.Errorf("workspace already exists at %s", wsPath)
	}

	lock, err := repostore.AcquireLock(name, false, storeLockTimeout)
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

	storePath := paths.RepoStorePath(name)
	if branchExists {
		if err := runGitClone(storePath, url, branch, wsPath, gitConfig); err != nil {
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
		if err := runGitClone(storePath, url, baseBranch, wsPath, gitConfig); err != nil {
			return "", err
		}
		if err := runGit(wsPath, "checkout", "-b", branch); err != nil {
			return "", err
		}
	}
	return wsPath, nil
}

func runGitClone(referencePath, url, branch, dest string, gitConfig map[string]string) error {
	cmd := exec.Command("git", cloneArgs(referencePath, url, branch, dest, gitConfig)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cloneArgs builds the `git clone` argv. Each --config <key>=<value> persists
// into the new clone's .git/config; they are emitted in sorted order for
// deterministic behavior. Keys are validated by config (no leading "-") so
// they can't be parsed as git options. The trailing "--" terminates options
// so a url beginning with "-" can't be parsed as a git flag (argument
// injection); url and dest are strictly positional.
func cloneArgs(referencePath, url, branch, dest string, gitConfig map[string]string) []string {
	args := []string{"clone", "--reference", referencePath, "--branch", branch}
	for _, k := range sortedKeys(gitConfig) {
		args = append(args, "--config", k+"="+gitConfig[k])
	}
	return append(args, "--", url, dest)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
	cmd := exec.Command("git", "-C", paths.RepoStorePath(name),
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
	cmd := exec.Command("git", "-C", paths.RepoStorePath(name),
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
	cmd := exec.Command("git", "-C", path, "log", "-g", "-1", "--format=%ct")
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// LandedInDefault reports whether the workspace's branch has already landed in
// its remote default branch — that is, the branch tip (HEAD) is an ancestor of
// refs/remotes/origin/HEAD, so every commit is already contained in the default
// branch. This catches merge- and rebase-merged work even when no PR is
// associated (e.g. a direct push or a local merge).
//
// The second return value is the default branch's short name (e.g. "main") for
// use in messages. landed is false when the default branch can't be resolved
// (treated conservatively as "not landed", so a stale or missing origin/HEAD
// never causes a deletion) or when the branch is itself the default branch (so
// a checkout of main is never pruned just for containing itself).
//
// Comparing against the last-fetched origin/HEAD means staleness only ever
// makes this more conservative: an out-of-date default branch yields a false
// negative (keep), never a false positive (delete).
func LandedInDefault(path, branch string) (landed bool, defaultBranch string, err error) {
	def, err := defaultBranchShortName(path)
	if err != nil {
		// Can't resolve the default branch — stay conservative and keep.
		return false, "", nil
	}
	if def == branch {
		return false, def, nil
	}
	cmd := exec.Command("git", "-C", path,
		"merge-base", "--is-ancestor", "HEAD", "refs/remotes/origin/HEAD")
	err = cmd.Run()
	if err == nil {
		return true, def, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, def, nil
	}
	return false, def, err
}

// defaultBranchShortName resolves the workspace's remote default branch to its
// short name (e.g. "main") via refs/remotes/origin/HEAD.
func defaultBranchShortName(path string) (string, error) {
	cmd := exec.Command("git", "-C", path,
		"symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	short := strings.TrimSpace(string(out))
	return strings.TrimPrefix(short, "origin/"), nil
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

// Rename renames a workspace from oldBranch to newBranch: it renames the
// workspace's branch to newBranch and moves the workspace directory from
// <repo>/<oldBranch> to <repo>/<newBranch>, keeping the two in sync the way
// `New` first created them. Returns the new absolute workspace path on success.
//
// It renames the local branch named oldBranch specifically — the one matching
// the directory, the invariant `New` establishes — rather than whatever HEAD
// happens to be, so a workspace whose checkout has drifted can never have an
// unrelated branch renamed out from under it. The branch and directory move
// together so a renamed workspace stays addressable by its branch name. If the
// directory move fails, the branch rename is rolled back so the workspace is
// left untouched. The store is not involved, so no store lock is taken.
func Rename(name, oldBranch, newBranch string) (string, error) {
	// Guard newBranch the same way New guards its branch: it forms a path and
	// becomes a git ref, so a traversing or option-looking name is rejected.
	if err := paths.ValidateBranch(newBranch); err != nil {
		return "", err
	}
	if oldBranch == newBranch {
		return "", fmt.Errorf("new branch %q is the same as the current one", newBranch)
	}
	if !Exists(name, oldBranch) {
		return "", fmt.Errorf("workspace not found at %s", PathFor(name, oldBranch))
	}
	oldPath := PathFor(name, oldBranch)
	newPath := PathFor(name, newBranch)
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("workspace already exists at %s", newPath)
	}

	// Rename the branch that matches the directory, not HEAD, so the two stay in
	// correspondence. Refuse up front (rather than surface a raw git error) when
	// that branch is missing or the target name is already taken locally.
	if !localBranchExists(oldPath, oldBranch) {
		return "", fmt.Errorf("workspace has no local branch %q to rename", oldBranch)
	}
	if localBranchExists(oldPath, newBranch) {
		return "", fmt.Errorf("a local branch %q already exists in the workspace", newBranch)
	}
	if err := runGit(oldPath, "branch", "-m", oldBranch, newBranch); err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		_ = runGit(oldPath, "branch", "-m", newBranch, oldBranch)
		return "", err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		_ = runGit(oldPath, "branch", "-m", newBranch, oldBranch)
		return "", err
	}
	// A slash-containing old branch leaves empty intermediate dirs behind;
	// clean them up to RepoDir, mirroring how rm/prune tidy the tree.
	paths.PruneEmptyDirs(filepath.Dir(oldPath), RepoDir(name))
	return newPath, nil
}

// localBranchExists reports whether the workspace at path has a local branch
// named branch.
func localBranchExists(path, branch string) bool {
	cmd := exec.Command("git", "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// Remove deletes the workspace dir.
func Remove(name, branch string) error {
	p := PathFor(name, branch)
	if !Exists(name, branch) {
		return fmt.Errorf("workspace not found at %s", p)
	}
	return os.RemoveAll(p)
}

// RepoDir returns the directory holding all workspaces for a repo.
func RepoDir(name string) string {
	return filepath.Join(paths.WorkspacesDir(), filepath.FromSlash(name))
}

// RemoveAllForRepo deletes every workspace belonging to the repo, along
// with the now-empty per-repo workspace directory. Returns nil if no
// workspaces exist. Workspaces are plain writable clones, so a single
// os.RemoveAll suffices.
func RemoveAllForRepo(name string) error {
	dir := RepoDir(name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	paths.PruneEmptyDirs(filepath.Dir(dir), paths.WorkspacesDir())
	return nil
}

// ListAll scans the workspaces directory directly and returns every
// workspace found, without consulting config. Use this when config may
// be missing or about to be deleted (e.g. purge). The Name field holds
// the repo-relative path on disk (repo name plus branch); callers that
// only need paths and dirty/unpushed status should prefer this over List.
func ListAll() ([]Info, error) {
	root := paths.WorkspacesDir()
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Info
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if s, err := os.Stat(filepath.Join(p, ".git")); err == nil && s.IsDir() {
			rel, err := filepath.Rel(root, p)
			if err != nil || rel == "." {
				return nil
			}
			out = append(out, infoFor(filepath.ToSlash(rel), "", p))
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}
