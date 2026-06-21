package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Antigravity runs hooks via PreInvocation, which fires before *every* model
// call and passes a JSON payload on stdin that includes invocationNum (0 for
// the first call of a conversation). repocache's session-context and bg-sync
// are session-start actions, so their antigravity hook commands act only on
// invocationNum==0 and otherwise emit an empty result.
// See https://antigravity.google/docs/hooks.

// hookStdin returns the reader carrying the PreInvocation payload, or nil when
// stdin is an interactive terminal (a manual run, no payload) so callers don't
// block reading it.
func hookStdin() io.Reader {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return nil
	}
	return os.Stdin
}

// antigravityFirstInvocation reports whether this PreInvocation is the first of
// the conversation (invocationNum==0), i.e. whether a session-start action
// should run. It defaults to true when there is no payload or it can't be
// parsed (a manual run, or a schema change), so the action still fires rather
// than being silently skipped.
func antigravityFirstInvocation(stdin io.Reader) bool {
	if stdin == nil {
		return true
	}
	data, err := io.ReadAll(stdin)
	if err != nil || len(data) == 0 {
		return true
	}
	var payload struct {
		InvocationNum *int `json:"invocationNum"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.InvocationNum == nil {
		return true
	}
	return *payload.InvocationNum == 0
}

// printInjectMessage writes a PreInvocation injectSteps envelope carrying a
// single userMessage — the JSON shape Antigravity reads from a hook's stdout to
// add a message to the conversation before the model is called.
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
