package workspace

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/repostore"
)

// gitOut runs a git command in dir and returns its trimmed stdout, failing the
// test on error. Companion to the git helper (which discards output).
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// buildStore creates a faithful shed store under the (temp) HOME from a local
// upstream, using the same repostore steps a real sync runs: a --no-checkout
// clone, a fetch of all refs, then a detached checkout of origin/HEAD. The
// store's branches end up under refs/remotes/origin/*, exactly as in production.
func buildStore(t *testing.T, name, upstream string) {
	t.Helper()
	if err := repostore.Clone(upstream, name); err != nil {
		t.Fatalf("store clone: %v", err)
	}
	if err := repostore.Fetch(name); err != nil {
		t.Fatalf("store fetch: %v", err)
	}
	if err := repostore.CheckoutDetachedHEAD(name); err != nil {
		t.Fatalf("store checkout: %v", err)
	}
}

// TestNewClonesFromStoreOffline is the regression for "shed ws new works
// offline": New must build the workspace entirely from the local store and
// never contact the remote. The origin URL handed to New is unreachable, so if
// New cloned or fetched from it (as the old --reference-against-remote path
// did) creation would fail. It still has to set origin to that URL so a later
// push/pull reaches the real remote once online.
func TestNewClonesFromStoreOffline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("HOME", t.TempDir())

	// Local upstream with main (a.txt) and a feat branch (feat.txt).
	upstream := filepath.Join(t.TempDir(), "upstream")
	git(t, t.TempDir(), nil, "init", "-q", "-b", "main", upstream)
	writeFile(t, upstream, "a.txt", "1")
	git(t, upstream, nil, "add", "a.txt")
	git(t, upstream, nil, "commit", "-q", "-m", "first")
	git(t, upstream, nil, "checkout", "-q", "-b", "feat")
	writeFile(t, upstream, "feat.txt", "2")
	git(t, upstream, nil, "add", "feat.txt")
	git(t, upstream, nil, "commit", "-q", "-m", "feat work")
	git(t, upstream, nil, "checkout", "-q", "main")

	const name = "github.com/test/repo"
	buildStore(t, name, upstream)

	// An unreachable remote: New must never touch it.
	const offlineURL = "https://offline.invalid/test/repo.git"

	t.Run("existing branch is checked out tracking origin", func(t *testing.T) {
		ws, err := New(name, "feat", "", offlineURL, nil)
		if err != nil {
			t.Fatalf("New(feat) offline = %v, want success", err)
		}
		if got := gitOut(t, ws, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
			t.Errorf("HEAD branch = %q, want %q", got, "feat")
		}
		if got := gitOut(t, ws, "rev-parse", "--abbrev-ref", "@{u}"); got != "origin/feat" {
			t.Errorf("upstream = %q, want %q", got, "origin/feat")
		}
		if got := gitOut(t, ws, "remote", "get-url", "origin"); got != offlineURL {
			t.Errorf("origin url = %q, want %q", got, offlineURL)
		}
		// origin/HEAD reproduced from the store so default-branch resolution works.
		if got := gitOut(t, ws, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/main" {
			t.Errorf("origin/HEAD = %q, want refs/remotes/origin/main", got)
		}
		// Working tree populated from the feat branch.
		if got := gitOut(t, ws, "status", "--porcelain"); got != "" {
			t.Errorf("workspace not clean after create:\n%s", got)
		}
		if _, err := exec.Command("git", "-C", ws, "cat-file", "-e", "HEAD:feat.txt").Output(); err != nil {
			t.Errorf("feat.txt missing from feat workspace: %v", err)
		}
	})

	t.Run("new branch forks from default with no upstream", func(t *testing.T) {
		ws, err := New(name, "brand-new", "", offlineURL, nil)
		if err != nil {
			t.Fatalf("New(brand-new) offline = %v, want success", err)
		}
		if got := gitOut(t, ws, "rev-parse", "--abbrev-ref", "HEAD"); got != "brand-new" {
			t.Errorf("HEAD branch = %q, want %q", got, "brand-new")
		}
		// A fresh branch isn't on origin yet, so it must have no upstream.
		if err := exec.Command("git", "-C", ws, "rev-parse", "--abbrev-ref", "@{u}").Run(); err == nil {
			t.Errorf("brand-new has an upstream, want none")
		}
		if got := gitOut(t, ws, "remote", "get-url", "origin"); got != offlineURL {
			t.Errorf("origin url = %q, want %q", got, offlineURL)
		}
		// Forked from main, so feat's file must be absent.
		if err := exec.Command("git", "-C", ws, "cat-file", "-e", "HEAD:feat.txt").Run(); err == nil {
			t.Errorf("feat.txt present in branch forked from main")
		}
	})
}
