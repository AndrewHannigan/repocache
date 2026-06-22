package agents

// Per-agent session-context output. The shed guide body is generated
// once (by the command layer) and handed to the selected agent's
// SessionContextOutput, which wraps it in the exact shape that agent's
// integration expects. The content is identical across agents; only the
// surrounding shape differs — so the wrapper is owned per agent rather than
// hardcoded to one agent's convention.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// sessionContextOpenTag / sessionContextCloseTag delimit the hook-envelope
// output so the model can extract it unambiguously from whatever else an
// agent prints around hook stdout.
const (
	sessionContextOpenTag  = "<shed-session-context>"
	sessionContextCloseTag = "</shed-session-context>"
)

// hookEnvelope is the SessionStart hook JSON Claude reads to inject context.
// additionalContext is a single string, so the guide body is JSON-escaped
// into it by the encoder.
//
// Each agent owns its SessionContextOutput method, so these choices
// are independent.
type hookEnvelope struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// renderHookEnvelope marshals body into the SessionStart hook envelope,
// wrapped in <shed-session-context> tags.
func renderHookEnvelope(body string) (string, error) {
	var env hookEnvelope
	env.HookSpecificOutput.HookEventName = "SessionStart"
	env.HookSpecificOutput.AdditionalContext = body
	data, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return sessionContextOpenTag + string(data) + sessionContextCloseTag, nil
}

// Claude Code reads the hookSpecificOutput envelope from its SessionStart
// hook stdout. (It also accepts plain stdout, but the envelope is its native,
// documented shape.)
func (c *Claude) SessionContextOutput(body string) (string, error) {
	return renderHookEnvelope(body)
}

// opencode injects the guide itself via its plugin (experimental.chat.system
// .transform), so it wants the raw Markdown body with no hook envelope or
// delimiters — it is not handing the output to any hook plumbing.
func (o *Opencode) SessionContextOutput(body string) (string, error) {
	return body, nil
}

// cursorSessionStartOutput is the JSON object Cursor reads from a sessionStart
// hook's stdout. Its additional_context field is injected into the
// conversation's initial context — Cursor's analog of Claude's
// additionalContext. The shape is a flat object with a snake_case key, distinct
// from Claude's hookSpecificOutput envelope, so Cursor gets its own renderer.
type cursorSessionStartOutput struct {
	AdditionalContext string `json:"additional_context"`
}

// Cursor's CLI parses its sessionStart hook's stdout as JSON and injects the
// additional_context field into the conversation. No delimiter tags are needed:
// the whole stdout is the JSON object, not embedded in other hook chatter.
func (c *Cursor) SessionContextOutput(body string) (string, error) {
	data, err := json.Marshal(cursorSessionStartOutput{AdditionalContext: body})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// sessionContextCommand is the session-context hook command installed for a
// given agent: the base __session-context subcommand plus --agent <key>, so
// the subcommand renders the output shape that agent expects.
func sessionContextCommand(agentKey string) string {
	return SessionContextCommand + " --agent " + agentKey
}

// legacySessionContextCommands are session-context hook commands that earlier
// versions installed under the old `repocache` binary name. Install strips
// them so a superseded form does not run (or error) on every session start:
//   - "repocache session-context"   — the original public subcommand,
//     which no longer exists (now the internal __session-context).
//   - "repocache __session-context" — the pre-per-agent bare form, before
//     --agent <key> selected the per-agent output shape.
var legacySessionContextCommands = []string{
	legacySessionContextCommand,
	"repocache __session-context",
}

// removeLegacySessionContextHooks strips every superseded session-context hook
// command from a settings file. Best-effort, like the rest of the install
// migration steps: errors are ignored so a malformed legacy entry can't block
// a fresh install.
func removeLegacySessionContextHooks(load loadFn, save saveFn, filePath string) {
	for _, cmd := range legacySessionContextCommands {
		_ = removeSessionStartHook(load, save, filePath, cmd)
	}
}

// SessionContextOutputFor renders the guide body in the shape the named agent
// expects. It is the entry point the __session-context command uses to map an
// --agent <key> flag to the right per-agent output.
func SessionContextOutputFor(agentKey, body string) (string, error) {
	a := ByKey(agentKey)
	if a == nil {
		return "", fmt.Errorf("unknown agent %q (valid: %s)", agentKey, strings.Join(allKeys(), ", "))
	}
	return a.SessionContextOutput(body)
}
