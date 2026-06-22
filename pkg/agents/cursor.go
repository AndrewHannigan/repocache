package agents

import (
	"os"
	"path/filepath"
)

// Cursor implements Agent for Cursor's CLI agent (cursor-agent).
//
// Cursor is a SessionStart-hook agent like Claude and Codex, but its hooks file
// differs structurally: it uses the camelCase event name `hooks.sessionStart`
// with FLAT command entries ({"command": "..."}) rather than Claude/Codex's
// PascalCase `hooks.SessionStart` with nested {"hooks":[{"type","command"}]}
// entries, and it requires a top-level "version": 1. So Cursor uses its own
// hook helpers below (ensure/removeCursorSessionStartHook) instead of the
// shared ensure/removeSessionStartHook.
//
// Like opencode, Cursor has no documented per-directory access allowlist (its
// cli-config.json permissions are shell/tool patterns, not filesystem roots),
// and the chmod a-w on repos/ enforces read-only regardless — so Cursor
// registers no paths.
type Cursor struct {
	dir string // ~/.cursor
}

func NewCursor() *Cursor {
	home, _ := os.UserHomeDir()
	return &Cursor{dir: filepath.Join(home, ".cursor")}
}

func (c *Cursor) Key() string  { return "cursor" }
func (c *Cursor) Name() string { return "Cursor CLI" }

func (c *Cursor) Detected() bool {
	s, err := os.Stat(c.dir)
	return err == nil && s.IsDir()
}

func (c *Cursor) hooksFile() string { return filepath.Join(c.dir, "hooks.json") }

// legacyPluginDir is the hand-rolled plugin some pre-first-class setups dropped
// at ~/.cursor/plugins/local/repocache/ (a .cursor-plugin manifest + a
// sessionStart script that shelled back to the old `repocache __session-context
// --agent cursor`). First-class support uses ~/.cursor/hooks.json instead;
// install removes the plugin so the guide isn't injected twice once
// `--agent cursor` becomes valid. Best-effort, mirroring the other agents'
// legacy migrations.
func (c *Cursor) legacyPluginDir() string {
	return filepath.Join(c.dir, "plugins", "local", "repocache")
}

func (c *Cursor) Install(opts InstallOptions) (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	// Migrate off the hand-rolled plugin prototype if present. Best-effort.
	_ = os.RemoveAll(c.legacyPluginDir())

	hooks, err := installHooks(opts, sessionContextCommand(c.Key()), BgSyncCommand, func(command string) (bool, error) {
		return ensureCursorSessionStartHook(c.hooksFile(), command)
	})
	if err != nil {
		return Installed{}, err
	}
	return Installed{AddedHooks: hooks}, nil
}

func (c *Cursor) Uninstall(prev Installed) error {
	// Best-effort cleanup of the hand-rolled plugin prototype, if any.
	_ = os.RemoveAll(c.legacyPluginDir())
	for _, hookCmd := range prev.AddedHooks {
		if err := removeCursorSessionStartHook(c.hooksFile(), hookCmd); err != nil {
			return err
		}
	}
	return nil
}

// ensureCursorSessionStartHook adds a flat {"command": command} entry to
// hooks.sessionStart in Cursor's hooks.json, creating the file, the nested
// structure, and the required top-level "version": 1 if missing. Idempotent: if
// an entry with the same command already exists, it is a no-op. Returns true if
// an entry was added this call.
func ensureCursorSessionStartHook(filePath, command string) (bool, error) {
	root, err := loadJSONC(filePath)
	if err != nil {
		return false, err
	}
	if root == nil {
		root = map[string]any{}
	}
	if _, ok := root["version"]; !ok {
		root["version"] = 1
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	sessionStart, _ := hooks["sessionStart"].([]any)
	for _, e := range sessionStart {
		if em, ok := e.(map[string]any); ok && em["command"] == command {
			return false, nil
		}
	}
	sessionStart = append(sessionStart, map[string]any{"command": command})
	hooks["sessionStart"] = sessionStart
	return true, saveJSON(filePath, root)
}

// removeCursorSessionStartHook removes every hooks.sessionStart entry whose
// command equals command. Missing file or keys are no-ops.
func removeCursorSessionStartHook(filePath, command string) error {
	root, err := loadJSONC(filePath)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	sessionStart, _ := hooks["sessionStart"].([]any)
	if sessionStart == nil {
		return nil
	}
	kept := sessionStart[:0]
	for _, e := range sessionStart {
		if em, ok := e.(map[string]any); ok && em["command"] == command {
			continue
		}
		kept = append(kept, e)
	}
	hooks["sessionStart"] = kept
	return saveJSON(filePath, root)
}
