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

func (c *Codex) legacyDocFile() string { return filepath.Join(c.dir, "REPOCACHE.md") }
func (c *Codex) memoryFile() string    { return filepath.Join(c.dir, "AGENTS.md") }
func (c *Codex) settingsFile() string  { return filepath.Join(c.dir, "config.toml") }

func codexHookEntry(command string) map[string]any {
	return map[string]any{
		"matcher": "startup|resume",
		"hooks": []any{
			map[string]any{
				"type":          "command",
				"command":       command,
				"statusMessage": hookLabel(command),
			},
		},
	}
}

func (c *Codex) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	// Migrate off the legacy @REPOCACHE.md import + on-disk doc. Best-effort.
	_ = removeImportLine(c.memoryFile(), "REPOCACHE.md")
	_ = os.Remove(c.legacyDocFile())
	// Strip superseded session-context hook commands (the pre-rename public
	// subcommand and the pre-per-agent bare form) so they don't run or error
	// on every session start. Best-effort.
	removeLegacySessionContextHooks(loadTOML, saveTOML, c.settingsFile())

	paths, err := ensureArrayEntries(loadTOML, saveTOML, c.settingsFile(),
		[]string{"sandbox_workspace_write", "writable_roots"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	hooks, err := installHooks(opts, sessionContextCommand(c.Key()), BgSyncCommand, func(command string) (bool, error) {
		return ensureSessionStartHook(loadTOML, saveTOML, c.settingsFile(),
			codexHookEntry(command), command)
	})
	if err != nil {
		return Installed{}, err
	}
	if len(hooks) > 0 {
		fmt.Fprintln(os.Stderr, "  note: Codex requires you to trust new hooks before they run.")
		fmt.Fprintln(os.Stderr, "        Open Codex CLI and run `/hooks` once to trust them.")
	}
	return Installed{AddedPaths: paths, AddedHooks: hooks}, nil
}

func (c *Codex) Uninstall(prev Installed) error {
	// Legacy cleanup: older versions added an @import + on-disk doc.
	if err := removeImportLine(c.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	_ = os.Remove(c.legacyDocFile())
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
	return nil
}
