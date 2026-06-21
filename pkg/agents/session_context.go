package agents

// Per-agent session-context output. The repocache guide body is generated
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
	sessionContextOpenTag  = "<repocache-session-context>"
	sessionContextCloseTag = "</repocache-session-context>"
)

// hookEnvelope is the SessionStart hook JSON Claude reads to inject context.
// additionalContext is a single string, so the guide body is JSON-escaped
// into it by the encoder.
//
// The wrapper key `hookSpecificOutput` originated in Claude Code. Codex accepts
// the same envelope too, but also accepts plain stdout, so it takes the raw
// body instead (below). Each agent owns its SessionContextOutput method, so
// these choices are independent.
type hookEnvelope struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// renderHookEnvelope marshals body into the SessionStart hook envelope,
// wrapped in <repocache-session-context> tags.
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

// injectStepsOutput is the PreInvocation hook JSON the Antigravity CLI reads.
// A userMessage step injects the guide into the conversation trajectory before
// the first model call, where it persists for the rest of the session. The
// guide is wrapped in <repocache-session-context> tags so the model can see it
// as one delimited block. See https://antigravity.google/docs/hooks.
type injectStepsOutput struct {
	InjectSteps []map[string]string `json:"injectSteps"`
}

// renderInjectSteps marshals body into the PreInvocation injectSteps envelope.
// Unlike the Claude envelope it carries no delimiter tags around the JSON —
// Antigravity parses the hook's stdout as a JSON document directly.
func renderInjectSteps(body string) (string, error) {
	out := injectStepsOutput{
		InjectSteps: []map[string]string{
			{"userMessage": body},
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Claude Code reads the hookSpecificOutput envelope from its SessionStart
// hook stdout. (It also accepts plain stdout, but the envelope is its native,
// documented shape.)
func (c *Claude) SessionContextOutput(body string) (string, error) {
	return renderHookEnvelope(body)
}

// Codex CLI accepts plain stdout from a hook as developer context, so it gets
// the raw Markdown body — no JSON envelope, no delimiter tags. (Codex also
// accepts the hookSpecificOutput envelope, but plain text renders more cleanly
// as the injected developer message.)
func (c *Codex) SessionContextOutput(body string) (string, error) {
	return "\n" + body, nil
}

// Antigravity CLI injects the guide via a PreInvocation hook (it has no
// SessionStart event), so it gets the injectSteps envelope rather than Claude's
// hookSpecificOutput one. The command layer emits this only on the first
// invocation (invocationNum==0); on later invocations it prints "{}" so the
// guide is injected once per conversation, not before every model call.
func (a *Antigravity) SessionContextOutput(body string) (string, error) {
	return renderInjectSteps(body)
}

// opencode injects the guide itself via its plugin (experimental.chat.system
// .transform), so it wants the raw Markdown body with no hook envelope or
// delimiters — it is not handing the output to any hook plumbing.
func (o *Opencode) SessionContextOutput(body string) (string, error) {
	return body, nil
}

// sessionContextCommand is the session-context hook command installed for a
// given agent: the base __session-context subcommand plus --agent <key>, so
// the subcommand renders the output shape that agent expects.
func sessionContextCommand(agentKey string) string {
	return SessionContextCommand + " --agent " + agentKey
}

// bgSyncCommand is the bg-sync hook command installed for a given agent. Most
// agents use the bare command; antigravity needs --agent so the command emits
// a JSON result for its PreInvocation hook instead of a plain-text hint.
func bgSyncCommand(agentKey string) string {
	if agentKey == "antigravity" {
		return BgSyncCommand + " --agent " + agentKey
	}
	return BgSyncCommand
}

// legacySessionContextCommands are session-context hook commands that earlier
// repocache versions installed. Install strips them so a superseded form does
// not run (or error) on every session start:
//   - "repocache session-context"   — the pre-rename public subcommand,
//     which no longer exists (now the internal __session-context).
//   - "repocache __session-context" — the pre-per-agent bare form, before
//     --agent <key> selected the per-agent output shape.
var legacySessionContextCommands = []string{
	legacySessionContextCommand,
	SessionContextCommand,
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
