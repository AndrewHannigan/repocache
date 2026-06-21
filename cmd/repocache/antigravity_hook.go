package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// Antigravity runs hooks via PreInvocation, which fires before *every* model
// call and passes a JSON payload on stdin that includes invocationNum (1 for
// the first model call of a turn), conversationId (unique per conversation),
// and other metadata. The hook commands use the conversationId to create a
// once-per-conversation sentinel (atomic mkdir), ensuring session-start
// actions like guide injection and bg-sync run at most once per conversation.
// See https://antigravity.google/docs/hooks.

// preInvocationPayload is the JSON payload antigravity sends to PreInvocation
// hook commands on stdin. invocationNum is 1-based and resets per turn;
// conversationId uniquely identifies the conversation session.
type preInvocationPayload struct {
	InvocationNum  int      `json:"invocationNum"`
	ConversationID string   `json:"conversationId"`
}

// hookStdin returns the reader carrying the hook payload, or nil when stdin
// is an interactive terminal (a manual run, no payload) so callers don't
// block reading it.
func hookStdin() io.Reader {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return nil
	}
	return os.Stdin
}

// parsePreInvocationPayload reads and parses the PreInvocation JSON payload
// from stdin. Returns nil if there is no payload or it can't be parsed.
func parsePreInvocationPayload(stdin io.Reader) *preInvocationPayload {
	if stdin == nil {
		return nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil || len(data) == 0 {
		return nil
	}
	var p preInvocationPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	return &p
}

// hookStateDirOverride is set by tests to redirect the hook state directory
// away from the real ~/.repocache/hook_state.
var hookStateDirOverride string

// hookStateDir returns the directory for hook state files.
func hookStateDir() string {
	if hookStateDirOverride != "" {
		return hookStateDirOverride
	}
	return filepath.Join(paths.DataDir(), "hook_state")
}

// agySentinelPath returns the path to the once-per-conversation sentinel
// directory for the given conversationId. Returns "" if conversationId is
// empty, which causes antigravityShouldAct to default to true.
func agySentinelPath(conversationID string) string {
	if conversationID == "" {
		return ""
	}
	return filepath.Join(hookStateDir(), "agy_injected_"+conversationID)
}

// antigravityShouldAct reports whether a session-start action (guide
// injection or bg-sync) should run for this PreInvocation. It uses two gates:
//
//  1. invocationNum == 1 (first model call of the turn) — a quick pre-check
//     so the sentinel is only checked once per user message. invocationNum
//     is 1-based per the antigravity hooks documentation.
//  2. A conversationId-scoped sentinel directory, created atomically via
//     os.Mkdir, ensuring once-per-conversation semantics regardless of turn
//     boundaries.
//
// On error, or when there is no payload (manual run), it defaults to true so
// session-start actions still fire rather than being silently skipped.
func antigravityShouldAct(stdin io.Reader) bool {
	p := parsePreInvocationPayload(stdin)
	if p == nil {
		return true
	}
	if p.InvocationNum != 1 {
		return false
	}
	sentinel := agySentinelPath(p.ConversationID)
	if sentinel == "" {
		return true
	}
	if err := os.MkdirAll(hookStateDir(), 0755); err != nil {
		return true
	}
	err := os.Mkdir(sentinel, 0755)
	return err == nil
}

// printInjectMessage writes a PreInvocation injectSteps envelope carrying a
// single userMessage — the JSON shape Antigravity reads from a hook's stdout
// to add a message to the conversation before the model is called.
func printInjectMessage(w io.Writer, msg string) error {
	out := map[string]any{
		"injectSteps": []any{map[string]string{"userMessage": msg}},
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
