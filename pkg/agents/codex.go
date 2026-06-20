package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

// Codex implements Agent for OpenAI's Codex CLI.
type Codex struct {
	dir string
}

func NewCodex() *Codex {
	home, _ := os.UserHomeDir()
	return &Codex{dir: filepath.Join(home, ".codex")}
}

func (c *Codex) Key() string  { return "codex" }
func (c *Codex) Name() string { return "Codex CLI" }

func (c *Codex) Detected() bool {
	s, err := os.Stat(c.dir)
	return err == nil && s.IsDir()
}

func (c *Codex) docFile() string      { return filepath.Join(c.dir, "REPOCACHE.md") }
func (c *Codex) memoryFile() string   { return filepath.Join(c.dir, "AGENTS.md") }
func (c *Codex) settingsFile() string { return filepath.Join(c.dir, "config.toml") }

func (c *Codex) Install(opts InstallOptions) (Installed, error) {
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
	paths, err := ensureArrayEntries(loadTOML, saveTOML, c.settingsFile(),
		[]string{"sandbox_workspace_write", "writable_roots"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	var hooks []string
	if !opts.NoBgSync {
		entry := map[string]any{
			"matcher": "startup|resume",
			"hooks": []any{
				map[string]any{
					"type":          "command",
					"command":       BgSyncCommand,
					"statusMessage": "repocache bg-sync",
				},
			},
		}
		addedHook, err := ensureSessionStartHook(loadTOML, saveTOML, c.settingsFile(), entry, BgSyncCommand)
		if err != nil {
			return Installed{}, err
		}
		if addedHook {
			hooks = []string{BgSyncCommand}
			fmt.Fprintln(os.Stderr, "  note: Codex requires you to trust new hooks before they run.")
			fmt.Fprintln(os.Stderr, "        Open Codex CLI and run `/hooks` once to trust this hook.")
		}
	}
	return Installed{
		AddedPaths:   paths,
		AddedImports: importLineRecord(addedImport, "REPOCACHE.md"),
		AddedHooks:   hooks,
	}, nil
}

func (c *Codex) Uninstall(prev Installed) error {
	if err := removeImportLine(c.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	if len(prev.AddedPaths) > 0 {
		if err := removeArrayEntries(loadTOML, saveTOML, c.settingsFile(),
			[]string{"sandbox_workspace_write", "writable_roots"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	for _, hookCmd := range prev.AddedHooks {
		if err := removeSessionStartHook(loadTOML, saveTOML, c.settingsFile(), hookCmd); err != nil {
			return err
		}
	}
	_ = os.Remove(c.docFile())
	return nil
}
