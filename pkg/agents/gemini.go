package agents

import (
	"os"
	"path/filepath"
)

// Gemini implements Agent for Google's Gemini CLI.
type Gemini struct {
	dir string
}

func NewGemini() *Gemini {
	home, _ := os.UserHomeDir()
	return &Gemini{dir: filepath.Join(home, ".gemini")}
}

func (g *Gemini) Key() string  { return "gemini" }
func (g *Gemini) Name() string { return "Gemini CLI" }

func (g *Gemini) Detected() bool {
	s, err := os.Stat(g.dir)
	return err == nil && s.IsDir()
}

func (g *Gemini) legacyDocFile() string { return filepath.Join(g.dir, "REPOCACHE.md") }
func (g *Gemini) memoryFile() string    { return filepath.Join(g.dir, "GEMINI.md") }
func (g *Gemini) settingsFile() string  { return filepath.Join(g.dir, "settings.json") }

func geminiHookEntry(command string) map[string]any {
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

func (g *Gemini) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(g.dir, 0755); err != nil {
		return Installed{}, err
	}
	// Migrate off the legacy @REPOCACHE.md import + on-disk doc. Best-effort.
	_ = removeImportLine(g.memoryFile(), "REPOCACHE.md")
	_ = os.Remove(g.legacyDocFile())
	// The session-context hook command was renamed `session-context` →
	// `__session-context`; strip the stale entry so the old (now-unknown)
	// subcommand doesn't error on every session start. Best-effort.
	_ = removeSessionStartHook(loadJSONC, saveJSON, g.settingsFile(), legacySessionContextCommand)

	paths, err := ensureArrayEntries(loadJSONC, saveJSON, g.settingsFile(),
		[]string{"includeDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	hooks, err := installHooks(opts, func(command string) (bool, error) {
		return ensureSessionStartHook(loadJSONC, saveJSON, g.settingsFile(),
			geminiHookEntry(command), command)
	})
	if err != nil {
		return Installed{}, err
	}
	return Installed{AddedPaths: paths, AddedHooks: hooks}, nil
}

func (g *Gemini) Uninstall(prev Installed) error {
	// Legacy cleanup: older versions added an @import + on-disk doc.
	if err := removeImportLine(g.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	_ = os.Remove(g.legacyDocFile())
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadJSONC, saveJSON, g.settingsFile(),
			[]string{"includeDirectories"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	for _, hookCmd := range prev.AddedHooks {
		if err := removeSessionStartHook(loadJSONC, saveJSON, g.settingsFile(), hookCmd); err != nil {
			return err
		}
	}
	return nil
}
