package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

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
func (c *Claude) memoryFile() string   { return filepath.Join(c.dir, "CLAUDE.md") }
func (c *Claude) settingsFile() string { return filepath.Join(c.dir, "settings.json") }

func (c *Claude) Install() (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(c.docFile(), DocContent, 0644); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", c.docFile(), err)
	}
	added, err := ensureImportLine(c.memoryFile(), "REPOCACHE.md")
	if err != nil {
		return Installed{}, err
	}
	paths, err := ensureArrayEntries(loadJSONC, saveJSON, c.settingsFile(),
		[]string{"permissions", "additionalDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	return Installed{
		AddedPaths:   paths,
		AddedImports: importLineRecord(added, "REPOCACHE.md"),
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
	_ = os.Remove(c.docFile())
	return nil
}
