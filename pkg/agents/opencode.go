package agents

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
)

// opencodePlugin is the plugin module repocache drops into opencode's
// plugin directory. It shells back to the `repocache` binary, so it never
// needs to change across upgrades.
//
//go:embed embed/opencode-plugin.js
var opencodePlugin []byte

// Opencode implements Agent for opencode (https://opencode.ai).
//
// opencode is unlike the other agents: it has no SessionStart shell-command
// hook and no per-directory access allowlist. Integration is a JS plugin
// dropped into ~/.config/opencode/plugin/, auto-loaded at startup — no edits
// to opencode.json are needed. So this agent does not use the shared
// settings-file helpers (installHooks / ensureArrayEntries); it materializes
// one file and records it for clean removal.
type Opencode struct {
	dir string // ~/.config/opencode
}

func NewOpencode() *Opencode {
	home, _ := os.UserHomeDir()
	return &Opencode{dir: filepath.Join(home, ".config", "opencode")}
}

func (o *Opencode) Key() string  { return "opencode" }
func (o *Opencode) Name() string { return "opencode" }

func (o *Opencode) Detected() bool {
	s, err := os.Stat(o.dir)
	return err == nil && s.IsDir()
}

func (o *Opencode) pluginFile() string {
	return filepath.Join(o.dir, "plugin", "repocache.js")
}

func (o *Opencode) Install(opts InstallOptions) (Installed, error) {
	pluginDir := filepath.Join(o.dir, "plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return Installed{}, err
	}
	// opencode has no allowlist to populate (its tools already reach
	// absolute paths; chmod a-w on repos/ enforces read-only) and no
	// session-context/bg-sync settings hooks — both are handled inside the
	// plugin itself, so InstallOptions.NoBgSync does not apply here.
	//
	// Idempotent: if the plugin is already present with identical content,
	// leave it and report nothing added (mirrors how the other agents only
	// report genuinely-new entries). Otherwise (re)write it.
	if cur, err := os.ReadFile(o.pluginFile()); err == nil && bytes.Equal(cur, opencodePlugin) {
		return Installed{}, nil
	}
	if err := os.WriteFile(o.pluginFile(), opencodePlugin, 0644); err != nil {
		return Installed{}, err
	}
	return Installed{AddedFiles: []string{o.pluginFile()}}, nil
}

func (o *Opencode) Uninstall(prev Installed) error {
	for _, f := range prev.AddedFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
