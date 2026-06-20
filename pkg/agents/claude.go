package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

const BgSyncCommand = "repocache __bg-sync"

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

func (c *Claude) docFile() string      { return filepath.Join(c.dir, "REPOCACHE.md") }
func (c *Claude) DocPath() string      { return c.docFile() }
func (c *Claude) memoryFile() string   { return filepath.Join(c.dir, "CLAUDE.md") }
func (c *Claude) settingsFile() string { return filepath.Join(c.dir, "settings.json") }

func (c *Claude) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(c.docFile(), DocContent, 0644); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", c.docFile(), err)
	}
	addedImport, err := ensureImportLine(c.memoryFile(), "REPOCACHE.md")
	if err != nil {
		return Installed{}, err
	}
	paths, err := ensureArrayEntries(loadJSONC, saveJSON, c.settingsFile(),
		[]string{"permissions", "additionalDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	var hooks []string
	if !opts.NoBgSync {
		entry := map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": BgSyncCommand,
				},
			},
		}
		addedHook, err := ensureSessionStartHook(loadJSONC, saveJSON, c.settingsFile(), entry, BgSyncCommand)
		if err != nil {
			return Installed{}, err
		}
		if addedHook {
			hooks = []string{BgSyncCommand}
		}
	}
	return Installed{
		AddedPaths:   paths,
		AddedImports: importLineRecord(addedImport, "REPOCACHE.md"),
		AddedHooks:   hooks,
	}, nil
}

func (c *Claude) Uninstall(prev Installed) error {
	if err := removeImportLine(c.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
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
	_ = os.Remove(c.docFile())
	return nil
}
