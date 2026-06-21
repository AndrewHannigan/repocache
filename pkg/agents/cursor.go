package agents

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// Cursor implements Agent for Cursor CLI.
//
// Cursor integrates as a Cursor plugin dropped into
// ~/.cursor/plugins/local/repocache/. Cursor loads local plugins from
// that directory automatically.
//
// The plugin provides a sessionStart hook that:
//   - runs repocache __bg-sync in the background to refresh the cache,
//     and
//   - runs repocache __session-context --cursor to inject the guide into
//     the conversation's initial system context (the equivalent of the
//     session-context hook that Claude/Codex/Antigravity use).
type Cursor struct {
	dir       string // ~/.cursor
	pluginDir string // ~/.cursor/plugins/local/repocache
}

func NewCursor() *Cursor {
	home, _ := os.UserHomeDir()
	return &Cursor{
		dir:       filepath.Join(home, ".cursor"),
		pluginDir: filepath.Join(home, ".cursor", "plugins", "local", "repocache"),
	}
}

func (c *Cursor) Key() string  { return "cursor" }
func (c *Cursor) Name() string { return "Cursor CLI" }

func (c *Cursor) Detected() bool {
	s, err := os.Stat(c.dir)
	return err == nil && s.IsDir()
}

//go:embed embed/cursor-plugin/.cursor-plugin/plugin.json
//go:embed embed/cursor-plugin/hooks/hooks.json
//go:embed embed/cursor-plugin/scripts/session-start.sh
var cursorPluginFS embed.FS

func (c *Cursor) Install(opts InstallOptions) (Installed, error) {
	pluginSrc := "embed/cursor-plugin"
	var addedFiles []string

	err := fs.WalkDir(cursorPluginFS, pluginSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(pluginSrc, path)
		target := filepath.Join(c.pluginDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, readErr := cursorPluginFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		mode := os.FileMode(0644)
		if filepath.Base(path) == "session-start.sh" {
			mode = 0755
		}
		// Idempotent: skip if already present with identical content.
		if cur, readErr := os.ReadFile(target); readErr == nil && bytes.Equal(cur, data) {
			return nil
		}
		if writeErr := os.WriteFile(target, data, mode); writeErr != nil {
			return writeErr
		}
		addedFiles = append(addedFiles, target)
		return nil
	})
	if err != nil {
		return Installed{}, err
	}

	return Installed{AddedFiles: addedFiles}, nil
}

func (c *Cursor) Uninstall(prev Installed) error {
	for _, f := range prev.AddedFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.RemoveAll(c.pluginDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
