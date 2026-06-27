package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// makeWorkspace creates a minimal workspace dir (with a .git subdir) so
// Exists/LocateByName treat it as a real workspace.
func makeWorkspace(t *testing.T, repo, name string) {
	t.Helper()
	gitDir := filepath.Join(paths.WorkspacePath(repo, name), ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestLocateByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repos := []string{"github.com/a/one", "github.com/b/two"}
	makeWorkspace(t, "github.com/a/one", "fix-bug")
	makeWorkspace(t, "github.com/b/two", "add-feature")

	repo, path, found := LocateByName(repos, "add-feature")
	if !found || repo != "github.com/b/two" {
		t.Fatalf("LocateByName(add-feature) = (%q, %q, %v), want repo github.com/b/two", repo, path, found)
	}
	if path != PathFor("github.com/b/two", "add-feature") {
		t.Errorf("path = %q, want %q", path, PathFor("github.com/b/two", "add-feature"))
	}

	if _, _, found := LocateByName(repos, "nonexistent"); found {
		t.Error("expected not found for nonexistent name")
	}
}

func TestLinkRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	makeWorkspace(t, "github.com/a/one", "task")

	want := SessionLink{Agent: "claude", SessionID: "sess-1", CWD: "/work"}
	if err := WriteLink("github.com/a/one", "task", want); err != nil {
		t.Fatalf("WriteLink: %v", err)
	}
	got, err := LoadLink("github.com/a/one", "task")
	if err != nil || got == nil {
		t.Fatalf("LoadLink = (%v, %v)", got, err)
	}
	if got.Agent != want.Agent || got.SessionID != want.SessionID || got.CWD != want.CWD {
		t.Errorf("LoadLink = %+v, want %+v", *got, want)
	}

	// No link for a different workspace returns (nil, nil).
	makeWorkspace(t, "github.com/a/one", "other")
	if l, err := LoadLink("github.com/a/one", "other"); err != nil || l != nil {
		t.Errorf("LoadLink(other) = (%v, %v), want (nil, nil)", l, err)
	}
}

func TestPendingRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := WritePending("feature/x", SessionLink{Agent: "cursor", SessionID: "c1", CWD: "/c"}); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	got, err := TakePending("feature/x")
	if err != nil || got == nil {
		t.Fatalf("TakePending = (%v, %v)", got, err)
	}
	if got.Agent != "cursor" || got.SessionID != "c1" {
		t.Errorf("pending = %+v", *got)
	}
	// Taken once, gone after.
	if again, _ := TakePending("feature/x"); again != nil {
		t.Errorf("pending should be consumed; got %+v", *again)
	}
}
