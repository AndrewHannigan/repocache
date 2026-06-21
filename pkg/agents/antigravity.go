package agents

import (
	"os"
	"path/filepath"
)

// Antigravity implements Agent for Google's Antigravity CLI. Antigravity is a
// Gemini-CLI fork and shares the Gemini config dir (~/.gemini): user settings —
// includeDirectories and SessionStart hooks — live in ~/.gemini/settings.json,
// the same file the standalone Gemini CLI reads.
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

func (a *Antigravity) settingsFile() string { return filepath.Join(a.dir, "settings.json") }

func googleHookEntry(command string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{
			map[string]any{
				"name":    hookName(command),
				"type":    "command",
				"command": command,
				"timeout": 5000,
			},
		},
	}
}

func (a *Antigravity) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(a.dir, 0755); err != nil {
		return Installed{}, err
	}
	// Strip superseded session-context hook commands (the pre-rename public
	// subcommand and the pre-per-agent bare form) so they don't run or error
	// on every session start. Best-effort.
	removeLegacySessionContextHooks(loadJSONC, saveJSON, a.settingsFile())

	paths, err := ensureArrayEntries(loadJSONC, saveJSON, a.settingsFile(),
		[]string{"includeDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	hooks, err := installHooks(opts, sessionContextCommand(a.Key()), func(command string) (bool, error) {
		return ensureSessionStartHook(loadJSONC, saveJSON, a.settingsFile(),
			googleHookEntry(command), command)
	})
	if err != nil {
		return Installed{}, err
	}
	return Installed{AddedPaths: paths, AddedHooks: hooks}, nil
}

func (a *Antigravity) Uninstall(prev Installed) error {
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadJSONC, saveJSON, a.settingsFile(),
			[]string{"includeDirectories"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	for _, hookCmd := range prev.AddedHooks {
		if err := removeSessionStartHook(loadJSONC, saveJSON, a.settingsFile(), hookCmd); err != nil {
			return err
		}
	}
	return nil
}
