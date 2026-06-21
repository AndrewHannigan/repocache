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
	if !sliceHas(got.AddedHooks, sessionContextCommand("claude")) || !sliceHas(got.AddedHooks, BgSyncCommand) {
		t.Errorf("AddedHooks = %v, want both session-context and bg-sync", got.AddedHooks)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	for _, want := range []string{sessionContextCommand("claude"), BgSyncCommand} {
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
	if !sliceHas(got.AddedHooks, sessionContextCommand("claude")) {
		t.Errorf("session-context must install even with NoBgSync; got %v", got.AddedHooks)
	}
}

// Antigravity installs PreInvocation hooks into ~/.gemini/config/hooks.json
// (its dedicated hooks file — it has no SessionStart event) and makes no
// settings.json / includeDirectories edits (it has no such key).
func TestAntigravityInstallUsesHooksJSON(t *testing.T) {
	home := withHome(t)

	got, err := NewAntigravity().Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if len(got.AddedPaths) != 0 {
		t.Errorf("antigravity should register no paths (no includeDirectories); got %v", got.AddedPaths)
	}
	if !sliceHas(got.AddedHooks, sessionContextCommand("antigravity")) || !sliceHas(got.AddedHooks, bgSyncCommand("antigravity")) {
		t.Errorf("AddedHooks = %v, want session-context and antigravity bg-sync", got.AddedHooks)
	}

	hooks := filepath.Join(home, ".gemini", "config", "hooks.json")
	data, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatalf("reading hooks.json: %v", err)
	}
	for _, want := range []string{
		"repocache-session-context", "PreInvocation",
		sessionContextCommand("antigravity"),
		"repocache-bg-sync", bgSyncCommand("antigravity"),
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("hooks.json missing %q\n%s", want, data)
		}
	}
	// No settings.json should be written for the integration.
	if _, err := os.Stat(filepath.Join(home, ".gemini", "settings.json")); err == nil {
		t.Errorf("antigravity install should not write ~/.gemini/settings.json")
	}

	// Idempotent: a second install adds nothing new.
	again, err := NewAntigravity().Install(InstallOptions{})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(again.AddedHooks) != 0 {
		t.Errorf("second Install should be a no-op; got AddedHooks=%v", again.AddedHooks)
	}
}

// Install strips repocache's dead entries from the legacy ~/.gemini/settings.json
// left by the removed Gemini CLI agent (the Antigravity CLI doesn't read it).
func TestAntigravityInstallCleansLegacyGeminiSettings(t *testing.T) {
	home := withHome(t)
	settings := filepath.Join(home, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0755); err != nil {
		t.Fatal(err)
	}
	// A legacy file: repocache's old includeDirectories + bare session-context
	// hook, plus unrelated user content that must be preserved.
	os.WriteFile(settings, []byte(`{`+
		`"includeDirectories":["`+filepath.Join(home, ".repocache", "repos")+`","keep/me"],`+
		`"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"`+SessionContextCommand+`"}]}]},`+
		`"security":{"auth":{"selectedType":"oauth-personal"}}}`), 0644)

	if _, err := NewAntigravity().Install(InstallOptions{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	data, _ := os.ReadFile(settings)
	s := string(data)
	if strings.Contains(s, filepath.Join(home, ".repocache", "repos")) {
		t.Errorf("legacy repocache includeDirectories entry should be stripped:\n%s", s)
	}
	if strings.Contains(s, `"command":"`+SessionContextCommand+`"`) {
		t.Errorf("legacy session-context hook should be stripped:\n%s", s)
	}
	if !strings.Contains(s, "keep/me") || !strings.Contains(s, "oauth-personal") {
		t.Errorf("unrelated user content must be preserved:\n%s", s)
	}
}

// Antigravity shares ~/.gemini with the standalone Gemini CLI, so it is
// detected by its own app-data subdir, not by ~/.gemini alone.
func TestAntigravityDetectsViaAppDataSubdir(t *testing.T) {
	home := withHome(t)
	a := NewAntigravity()

	if a.Detected() {
		t.Fatal("Detected() = true with no ~/.gemini")
	}
	// ~/.gemini alone (e.g. only the standalone Gemini CLI) must not count.
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0755); err != nil {
		t.Fatal(err)
	}
	if a.Detected() {
		t.Error("Detected() = true with ~/.gemini but no antigravity-cli subdir")
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "antigravity-cli"), 0755); err != nil {
		t.Fatal(err)
	}
	if !a.Detected() {
		t.Error("Detected() = false with ~/.gemini/antigravity-cli present")
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

// Install migrates superseded session-context hooks to the per-agent form.
// An older install wrote either the public `repocache session-context`
// subcommand (since renamed, now removed) or the bare `repocache
// __session-context` (before --agent <key> selected the per-agent shape).
// Both must be stripped and replaced with `repocache __session-context
// --agent claude`.
func TestClaudeInstallMigratesLegacyHooks(t *testing.T) {
	for _, legacy := range legacySessionContextCommands {
		t.Run(legacy, func(t *testing.T) {
			home := withHome(t)
			dir := filepath.Join(home, ".claude")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			settings := filepath.Join(dir, "settings.json")
			os.WriteFile(settings, []byte(`{"hooks":{"SessionStart":[`+
				`{"hooks":[{"type":"command","command":"`+legacy+`"}]}]}}`), 0644)

			if _, err := NewClaude().Install(InstallOptions{}); err != nil {
				t.Fatalf("Install: %v", err)
			}

			data, _ := os.ReadFile(settings)
			if want := sessionContextCommand("claude"); !strings.Contains(string(data), want) {
				t.Errorf("per-agent hook %q should be present:\n%s", want, data)
			}
			// The bare legacy command must not survive as its own entry. It is
			// a substring of the per-agent command, so match the exact JSON
			// command string rather than a loose substring.
			if exact := `"command":"` + legacy + `"`; strings.Contains(string(data), exact) {
				t.Errorf("legacy hook %q should be stripped:\n%s", legacy, data)
			}
		})
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
	if !strings.Contains(string(data), "__session-context --agent opencode") {
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

// Uninstall removes the named hooks it recorded from hooks.json.
func TestAntigravityUninstallReverses(t *testing.T) {
	home := withHome(t)
	a := NewAntigravity()

	installed, err := a.Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := a.Uninstall(installed); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".gemini", "config", "hooks.json"))
	for _, gone := range []string{"repocache-session-context", "repocache-bg-sync", SessionContextCommand, BgSyncCommand} {
		if strings.Contains(string(data), gone) {
			t.Errorf("uninstall left %q behind\n%s", gone, data)
		}
	}
}
