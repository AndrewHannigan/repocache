package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// withHome points os.UserHomeDir (via $HOME) at a temp dir so the agent
// constructors resolve their config dirs under it.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestReconcileDocs(t *testing.T) {
	home := withHome(t)

	// Claude is integrated with a stale doc; Codex is integrated and
	// already up to date; Gemini is integrated but its doc was deleted.
	claudeDoc := filepath.Join(home, ".claude", "REPOCACHE.md")
	codexDoc := filepath.Join(home, ".codex", "REPOCACHE.md")
	geminiDoc := filepath.Join(home, ".gemini", "REPOCACHE.md")

	mustWrite(t, claudeDoc, []byte("old stale content"))
	mustWrite(t, codexDoc, DocContent)
	// geminiDoc intentionally absent.

	state := &State{Agents: map[string]Installed{
		"claude": {},
		"codex":  {},
		"gemini": {},
	}}

	updated, err := ReconcileDocs(state)
	if err != nil {
		t.Fatalf("ReconcileDocs: %v", err)
	}

	if len(updated) != 1 || updated[0] != "claude" {
		t.Fatalf("updated = %v, want [claude]", updated)
	}
	if got, _ := os.ReadFile(claudeDoc); string(got) != string(DocContent) {
		t.Errorf("claude doc not refreshed to embedded content")
	}
	if _, err := os.Stat(geminiDoc); !os.IsNotExist(err) {
		t.Errorf("gemini doc was resurrected; reconcile should leave deleted files deleted")
	}
}

func TestReconcileDocsSkipsUnknownAgent(t *testing.T) {
	withHome(t)
	state := &State{Agents: map[string]Installed{"bogus": {}}}
	updated, err := ReconcileDocs(state)
	if err != nil {
		t.Fatalf("ReconcileDocs: %v", err)
	}
	if len(updated) != 0 {
		t.Fatalf("updated = %v, want none", updated)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
