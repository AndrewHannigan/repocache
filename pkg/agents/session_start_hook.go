package agents

// SessionStart hook helpers, generic over the settings file format
// (JSON or TOML). Each agent constructs its own outer entry with the
// matcher/extras it wants and passes the canonical command string used
// for idempotency + cleanup.

// ensureSessionStartHook adds `entry` to hooks.SessionStart (creating
// the nested structure if missing). Idempotent — if any existing entry
// contains an inner hook whose command equals `command`, no-op.
// Returns true if added this call.
//
// Entry must be a map with a "hooks" key holding []any of command
// objects (each with "type" and "command" at minimum). The entry may
// carry other fields like "matcher", "name", "timeout", etc., which
// are passed through verbatim.
func ensureSessionStartHook(load loadFn, save saveFn, filePath string, entry map[string]any, command string) (bool, error) {
	root, err := load(filePath)
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
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if entryContainsCommand(em, command) {
			return false, nil
		}
	}
	sessionStart = append(sessionStart, entry)
	hooks["SessionStart"] = sessionStart
	return true, save(filePath, root)
}

// removeSessionStartHook removes any SessionStart entries whose inner
// hooks contain a command matching `command`. If an outer entry's
// inner hooks become empty after removal, the outer entry is dropped.
func removeSessionStartHook(load loadFn, save saveFn, filePath, command string) error {
	root, err := load(filePath)
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
		em, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		inner, _ := em["hooks"].([]any)
		innerKept := inner[:0]
		for _, ih := range inner {
			if ihm, ok := ih.(map[string]any); ok {
				if ihm["type"] == "command" && ihm["command"] == command {
					continue
				}
			}
			innerKept = append(innerKept, ih)
		}
		if len(innerKept) == 0 {
			continue
		}
		em["hooks"] = innerKept
		kept = append(kept, em)
	}
	hooks["SessionStart"] = kept
	return save(filePath, root)
}

func entryContainsCommand(entry map[string]any, command string) bool {
	inner, _ := entry["hooks"].([]any)
	for _, ih := range inner {
		ihm, ok := ih.(map[string]any)
		if !ok {
			continue
		}
		if ihm["type"] == "command" && ihm["command"] == command {
			return true
		}
	}
	return false
}
