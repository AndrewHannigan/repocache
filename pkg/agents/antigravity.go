package agents

import (
	"os"
	"path/filepath"
)

// Antigravity implements Agent for Google's Antigravity CLI.
type Antigravity struct {
	dir string
}

func NewAntigravity() *Antigravity {
	home, _ := os.UserHomeDir()
	return &Antigravity{dir: filepath.Join(home, ".antigravity")}
}

func (a *Antigravity) Key() string  { return "antigravity" }
func (a *Antigravity) Name() string { return "Antigravity CLI" }

func (a *Antigravity) Detected() bool {
	s, err := os.Stat(a.dir)
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
	// The session-context hook command was renamed `session-context` →
	// `__session-context`; strip the stale entry so the old (now-unknown)
	// subcommand doesn't error on every session start. Best-effort.
	_ = removeSessionStartHook(loadJSONC, saveJSON, a.settingsFile(), legacySessionContextCommand)

	paths, err := ensureArrayEntries(loadJSONC, saveJSON, a.settingsFile(),
		[]string{"includeDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	hooks, err := installHooks(opts, func(command string) (bool, error) {
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
