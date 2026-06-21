package agents

import (
	"os"
	"path/filepath"
)

// Antigravity implements Agent for Google's Antigravity CLI (`agy`).
//
// Antigravity is a Gemini-CLI fork that shares the ~/.gemini config dir, but
// its integration surface is its own, not Gemini's:
//   - Detection: it creates ~/.gemini/antigravity-cli/ as its app-data dir.
//     ~/.gemini alone is ambiguous (the standalone Gemini CLI uses it too).
//   - Hooks: a dedicated ~/.gemini/config/hooks.json (name → event → handlers),
//     NOT settings.json. There is no SessionStart event; the session-start
//     equivalent is a PreInvocation hook gated on invocationNum==0.
//   - Directory access: there is no includeDirectories key. Repos under
//     ~/.repocache are reached via the guide + the CLI's file-access policy
//     (or `agy --add-dir`), so repocache installs no settings.json edits.
//
// See https://antigravity.google/docs/hooks and /docs/cli-reference.
type Antigravity struct {
	dir string // the Gemini config dir, ~/.gemini
}

func NewAntigravity() *Antigravity {
	home, _ := os.UserHomeDir()
	return &Antigravity{dir: filepath.Join(home, ".gemini")}
}

func (a *Antigravity) Key() string  { return "antigravity" }
func (a *Antigravity) Name() string { return "Antigravity CLI" }

// Detected reports whether the Antigravity CLI is installed. Because ~/.gemini
// is shared with the standalone Gemini CLI, its mere presence is ambiguous;
// Antigravity is identified by its own app-data subdir, ~/.gemini/antigravity-cli,
// which the CLI creates on first run.
func (a *Antigravity) Detected() bool {
	s, err := os.Stat(filepath.Join(a.dir, "antigravity-cli"))
	return err == nil && s.IsDir()
}

// hooksFile is Antigravity's global hooks file, ~/.gemini/config/hooks.json.
func (a *Antigravity) hooksFile() string { return filepath.Join(a.dir, "config", "hooks.json") }

// legacyGeminiSettings is ~/.gemini/settings.json, where the now-removed Gemini
// CLI agent wrote repocache's hooks + includeDirectories. The Antigravity CLI
// does not read it, so Install strips those stale entries.
func (a *Antigravity) legacyGeminiSettings() string { return filepath.Join(a.dir, "settings.json") }

func (a *Antigravity) Install(opts InstallOptions) (Installed, error) {
	a.removeLegacyGeminiEntries()

	hooks, err := installHooks(opts, sessionContextCommand(a.Key()), bgSyncCommand(a.Key()), func(command string) (bool, error) {
		return ensurePreInvocationHook(loadJSONC, saveJSON, a.hooksFile(), hookName(command), command)
	})
	if err != nil {
		return Installed{}, err
	}
	return Installed{AddedHooks: hooks}, nil
}

func (a *Antigravity) Uninstall(prev Installed) error {
	for _, hookCmd := range prev.AddedHooks {
		if err := removeNamedHook(loadJSONC, saveJSON, a.hooksFile(), hookName(hookCmd)); err != nil {
			return err
		}
	}
	return nil
}

// removeLegacyGeminiEntries clears repocache's entries from ~/.gemini/settings.json
// left by the superseded Gemini CLI agent: the includeDirectories paths and the
// SessionStart session-context + bg-sync hooks. The Antigravity CLI never reads
// this file, so the entries are dead; left behind they could still run (or
// error) under a stray standalone Gemini CLI. Best-effort, like the other
// install migrations — errors are ignored so they can't block a fresh install.
func (a *Antigravity) removeLegacyGeminiEntries() {
	f := a.legacyGeminiSettings()
	_ = removeArrayEntries(loadJSONC, saveJSON, f, []string{"includeDirectories"}, PathsToRegister())
	removeLegacySessionContextHooks(loadJSONC, saveJSON, f)
	_ = removeSessionStartHook(loadJSONC, saveJSON, f, sessionContextCommand(a.Key()))
	_ = removeSessionStartHook(loadJSONC, saveJSON, f, BgSyncCommand)
}
