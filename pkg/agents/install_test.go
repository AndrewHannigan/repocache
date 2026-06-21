package agents

import (
	"os"
	"path/filepath"
	"strings"
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

func sliceHas(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Install must integrate via SessionStart hooks, never the old on-disk
// REPOCACHE.md / @import.
func TestClaudeInstallUsesHooksNotImport(t *testing.T) {
	home := withHome(t)

	got, err := NewClaude().Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".claude", "REPOCACHE.md")); !os.IsNotExist(err) {
		t.Errorf("REPOCACHE.md should not be installed")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Errorf("CLAUDE.md should not be created for an @import")
	}
	if !sliceHas(got.AddedHooks, SessionContextCommand) || !sliceHas(got.AddedHooks, BgSyncCommand) {
		t.Errorf("AddedHooks = %v, want both session-context and bg-sync", got.AddedHooks)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	for _, want := range []string{SessionContextCommand, BgSyncCommand} {
		if !strings.Contains(string(data), want) {
			t.Errorf("settings.json missing hook %q\n%s", want, data)
		}
	}
}

// --no-bg-sync skips only the bg-sync hook; session-context still installs
// (it is how the agent learns about repocache at all).
func TestClaudeInstallNoBgSync(t *testing.T) {
	withHome(t)

	got, err := NewClaude().Install(InstallOptions{NoBgSync: true})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if sliceHas(got.AddedHooks, BgSyncCommand) {
		t.Errorf("NoBgSync should skip bg-sync; got %v", got.AddedHooks)
	}
	if !sliceHas(got.AddedHooks, SessionContextCommand) {
		t.Errorf("session-context must install even with NoBgSync; got %v", got.AddedHooks)
	}
}

// Install migrates away from a legacy install: the @import line and the
// on-disk doc are removed, but unrelated user content is preserved.
func TestClaudeInstallMigratesLegacy(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	memory := filepath.Join(dir, "CLAUDE.md")
	doc := filepath.Join(dir, "REPOCACHE.md")
	os.WriteFile(memory, []byte("# my notes\n@REPOCACHE.md  <!-- repocache:managed -->\n"), 0644)
	os.WriteFile(doc, []byte("stale doc"), 0644)

	if _, err := NewClaude().Install(InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(doc); !os.IsNotExist(err) {
		t.Errorf("legacy REPOCACHE.md should be removed")
	}
	got, _ := os.ReadFile(memory)
	if strings.Contains(string(got), "@REPOCACHE.md") {
		t.Errorf("legacy @import should be removed; got %q", got)
	}
	if !strings.Contains(string(got), "# my notes") {
		t.Errorf("user content should be preserved; got %q", got)
	}
}

// opencode integrates via a dropped-in plugin file (no settings edits, no
// path allowlist). Install writes the plugin and records it in AddedFiles.
func TestOpencodeInstallWritesPlugin(t *testing.T) {
	home := withHome(t)

	got, err := NewOpencode().Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	plugin := filepath.Join(home, ".config", "opencode", "plugin", "repocache.js")
	if _, err := os.Stat(plugin); err != nil {
		t.Errorf("plugin not written: %v", err)
	}
	if !sliceHas(got.AddedFiles, plugin) {
		t.Errorf("AddedFiles = %v, want %q", got.AddedFiles, plugin)
	}
	if len(got.AddedHooks) != 0 || len(got.AddedPaths) != 0 {
		t.Errorf("opencode should record no hooks/paths; got hooks=%v paths=%v", got.AddedHooks, got.AddedPaths)
	}

	data, _ := os.ReadFile(plugin)
	if !strings.Contains(string(data), "session-context --text") {
		t.Errorf("plugin missing session-context call:\n%s", data)
	}

	// Idempotent: a second install with the plugin already present and
	// unchanged reports nothing newly added.
	again, err := NewOpencode().Install(InstallOptions{})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(again.AddedFiles) != 0 {
		t.Errorf("second Install should be a no-op; got AddedFiles=%v", again.AddedFiles)
	}
}

// Uninstall deletes exactly the plugin file it recorded.
func TestOpencodeUninstallReverses(t *testing.T) {
	home := withHome(t)
	o := NewOpencode()

	installed, err := o.Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := o.Uninstall(installed); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	plugin := filepath.Join(home, ".config", "opencode", "plugin", "repocache.js")
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		t.Errorf("plugin should be removed, stat err = %v", err)
	}
}

// Uninstall removes the hooks it recorded.
func TestClaudeUninstallReverses(t *testing.T) {
	home := withHome(t)
	c := NewClaude()

	installed, err := c.Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := c.Uninstall(installed); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	for _, gone := range []string{SessionContextCommand, BgSyncCommand} {
		if strings.Contains(string(data), gone) {
			t.Errorf("uninstall left hook %q behind\n%s", gone, data)
		}
	}
}
