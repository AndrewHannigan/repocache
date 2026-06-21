package agents

// Antigravity hooks.json helpers. Unlike the Gemini/Claude/Codex
// `settings.json → hooks.SessionStart` shape (see session_start_hook.go),
// the Antigravity CLI reads a dedicated hooks.json whose top level maps a
// hook *name* to its event configuration:
//
//	{
//	  "repocache-session-context": {
//	    "PreInvocation": [
//	      { "type": "command", "command": "repocache __session-context --agent antigravity" }
//	    ]
//	  }
//	}
//
// Antigravity has no SessionStart event; PreInvocation (fires before each
// model call) is the session-start equivalent — the hook command gates on
// invocationNum==0 so it injects only once per conversation. See
// https://antigravity.google/docs/hooks.

// ensurePreInvocationHook adds a named PreInvocation hook running command,
// idempotent by hook name. If the name already exists it is overwritten so
// the command stays current across upgrades. Returns true if the file changed.
func ensurePreInvocationHook(load loadFn, save saveFn, filePath, name, command string) (bool, error) {
	root, err := load(filePath)
	if err != nil {
		return false, err
	}
	if root == nil {
		root = map[string]any{}
	}
	want := map[string]any{
		"PreInvocation": []any{
			map[string]any{"type": "command", "command": command},
		},
	}
	if existing, ok := root[name].(map[string]any); ok && preInvocationCommand(existing) == command {
		return false, nil
	}
	root[name] = want
	return true, save(filePath, root)
}

// removeNamedHook deletes the top-level hook entry `name` from hooks.json.
// Missing file or missing entry is a no-op.
func removeNamedHook(load loadFn, save saveFn, filePath, name string) error {
	root, err := load(filePath)
	if err != nil || root == nil {
		return err
	}
	if _, ok := root[name]; !ok {
		return nil
	}
	delete(root, name)
	return save(filePath, root)
}

// preInvocationCommand returns the command of the first PreInvocation handler
// in a hook entry, or "" if absent.
func preInvocationCommand(entry map[string]any) string {
	handlers, _ := entry["PreInvocation"].([]any)
	for _, h := range handlers {
		if hm, ok := h.(map[string]any); ok {
			if cmd, ok := hm["command"].(string); ok {
				return cmd
			}
		}
	}
	return ""
}
