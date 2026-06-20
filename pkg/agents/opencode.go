package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenCode implements Agent for sst/opencode.
//
// Caveat: when a project-level AGENTS.md exists in the user's cwd,
// OpenCode silently ignores the global one — including the
// `@REPOCACHE.md` line repocache adds. Upstream bug; nothing we can do
// from this side. Install() prints a warning.
type OpenCode struct {
	dir string
}

func NewOpenCode() *OpenCode {
	home, _ := os.UserHomeDir()
	return &OpenCode{dir: filepath.Join(home, ".config", "opencode")}
}

func (o *OpenCode) Key() string  { return "opencode" }
func (o *OpenCode) Name() string { return "OpenCode" }

func (o *OpenCode) Detected() bool {
	s, err := os.Stat(o.dir)
	return err == nil && s.IsDir()
}

func (o *OpenCode) docFile() string      { return filepath.Join(o.dir, "REPOCACHE.md") }
func (o *OpenCode) memoryFile() string   { return filepath.Join(o.dir, "AGENTS.md") }
func (o *OpenCode) settingsFile() string { return filepath.Join(o.dir, "opencode.json") }

func (o *OpenCode) Install() (Installed, error) {
	if err := os.MkdirAll(o.dir, 0755); err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(o.docFile(), DocContent, 0644); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", o.docFile(), err)
	}
	added, err := ensureImportLine(o.memoryFile(), "REPOCACHE.md")
	if err != nil {
		return Installed{}, err
	}
	paths, err := ensureArrayEntries(loadJSONC, saveJSON, o.settingsFile(),
		[]string{"external_directory"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	fmt.Fprintln(os.Stderr, "  note: OpenCode silently ignores the global AGENTS.md when a project-level one exists.")
	fmt.Fprintln(os.Stderr, "        Affected projects won't see the @REPOCACHE.md line (upstream issue).")
	return Installed{
		AddedPaths:   paths,
		AddedImports: importLineRecord(added, "REPOCACHE.md"),
	}, nil
}

func (o *OpenCode) Uninstall(prev Installed) error {
	if err := removeImportLine(o.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadJSONC, saveJSON, o.settingsFile(),
			[]string{"external_directory"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	_ = os.Remove(o.docFile())
	return nil
}
