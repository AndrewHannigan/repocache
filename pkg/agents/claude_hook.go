package agents

// ensureSessionStartHook adds a Claude-Code-format SessionStart hook
// invoking `command` to settingsPath. Returns true if a hook was added
// this call (false if an equivalent entry was already present).
//
// Claude Code's hook schema:
//
//	{
//	  "hooks": {
//	    "SessionStart": [
//	      { "hooks": [ { "type": "command", "command": "..." } ] }
//	    ]
//	  }
//	}
func ensureSessionStartHook(settingsPath, command string) (bool, error) {
	root, err := loadJSONC(settingsPath)
	if err != nil {
		return false, err
	}
	if root == nil {
		root = map[string]any{}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	sessionStart, _ := hooks["SessionStart"].([]any)
	for _, e := range sessionStart {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := entry["hooks"].([]any)
		for _, ih := range inner {
			m, ok := ih.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "command" && m["command"] == command {
				return false, nil
			}
		}
	}
	newEntry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	}
	sessionStart = append(sessionStart, newEntry)
	hooks["SessionStart"] = sessionStart
	return true, saveJSON(settingsPath, root)
}

// removeSessionStartHook removes any SessionStart entry whose inner
// hooks contain a command equal to `command`. If an outer entry's inner
// list becomes empty after removal, the outer entry is dropped too.
func removeSessionStartHook(settingsPath, command string) error {
	root, err := loadJSONC(settingsPath)
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
	sessionStart, _ := hooks["SessionStart"].([]any)
	if sessionStart == nil {
		return nil
	}
	kept := sessionStart[:0]
	for _, e := range sessionStart {
		entry, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		inner, _ := entry["hooks"].([]any)
		innerKept := inner[:0]
		for _, ih := range inner {
			m, ok := ih.(map[string]any)
			if !ok {
				innerKept = append(innerKept, ih)
				continue
			}
			if m["type"] == "command" && m["command"] == command {
				continue
			}
			innerKept = append(innerKept, ih)
		}
		if len(innerKept) == 0 {
			continue
		}
		entry["hooks"] = innerKept
		kept = append(kept, entry)
	}
	hooks["SessionStart"] = kept
	return saveJSON(settingsPath, root)
}
