package agents

import (
	"os"
	"path/filepath"
)

const BgSyncCommand = "repocache __bg-sync"

// SessionContextCommand is the command installed into each agent's
// SessionStart hook to inject the repocache guide as context. Mirrors
// the public `repocache session-context` subcommand.
const SessionContextCommand = "repocache session-context"

// Claude implements Agent for Claude Code.
type Claude struct {
	dir string // ~/.claude
}

func NewClaude() *Claude {
	home, _ := os.UserHomeDir()
	return &Claude{dir: filepath.Join(home, ".claude")}
}

func (c *Claude) Key() string  { return "claude" }
func (c *Claude) Name() string { return "Claude Code" }

func (c *Claude) Detected() bool {
	s, err := os.Stat(c.dir)
	return err == nil && s.IsDir()
}

func (c *Claude) legacyDocFile() string { return filepath.Join(c.dir, "REPOCACHE.md") }
func (c *Claude) memoryFile() string    { return filepath.Join(c.dir, "CLAUDE.md") }
func (c *Claude) settingsFile() string  { return filepath.Join(c.dir, "settings.json") }

func claudeHookEntry(command string) map[string]any {
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	}
}

func (c *Claude) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	// Migrate off the legacy @REPOCACHE.md import + on-disk doc that older
	// repocache versions installed; context now comes from the
	// session-context hook. Best-effort.
	_ = removeImportLine(c.memoryFile(), "REPOCACHE.md")
	_ = os.Remove(c.legacyDocFile())

	paths, err := ensureArrayEntries(loadJSONC, saveJSON, c.settingsFile(),
		[]string{"permissions", "additionalDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	hooks, err := installHooks(opts, func(command string) (bool, error) {
		return ensureSessionStartHook(loadJSONC, saveJSON, c.settingsFile(),
			claudeHookEntry(command), command)
	})
	if err != nil {
		return Installed{}, err
	}
	return Installed{AddedPaths: paths, AddedHooks: hooks}, nil
}

func (c *Claude) Uninstall(prev Installed) error {
	// Legacy cleanup: older versions added an @import + on-disk doc.
	if err := removeImportLine(c.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	_ = os.Remove(c.legacyDocFile())
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadJSONC, saveJSON, c.settingsFile(),
			[]string{"permissions", "additionalDirectories"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	for _, hookCmd := range prev.AddedHooks {
		if err := removeSessionStartHook(loadJSONC, saveJSON, c.settingsFile(), hookCmd); err != nil {
			return err
		}
	}
	return nil
}
