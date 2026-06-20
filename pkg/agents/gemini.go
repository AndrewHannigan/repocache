package agents

import (
	"fmt"
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

func (g *Gemini) docFile() string      { return filepath.Join(g.dir, "REPOCACHE.md") }
func (g *Gemini) memoryFile() string   { return filepath.Join(g.dir, "GEMINI.md") }
func (g *Gemini) settingsFile() string { return filepath.Join(g.dir, "settings.json") }

func (g *Gemini) Install(_ InstallOptions) (Installed, error) {
	if err := os.MkdirAll(g.dir, 0755); err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(g.docFile(), DocContent, 0644); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", g.docFile(), err)
	}
	added, err := ensureImportLine(g.memoryFile(), "REPOCACHE.md")
	if err != nil {
		return Installed{}, err
	}
	paths, err := ensureArrayEntries(loadJSONC, saveJSON, g.settingsFile(),
		[]string{"includeDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	return Installed{
		AddedPaths:   paths,
		AddedImports: importLineRecord(added, "REPOCACHE.md"),
	}, nil
}

func (g *Gemini) Uninstall(prev Installed) error {
	if err := removeImportLine(g.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadJSONC, saveJSON, g.settingsFile(),
			[]string{"includeDirectories"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	_ = os.Remove(g.docFile())
	return nil
}
