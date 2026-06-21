package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/agents"
)

// SessionContextCommand is the canonical command string installed into
// each agent's SessionStart hook to inject the repocache guide as
// context. Shared so install/uninstall can match it for idempotency.
const SessionContextCommand = "repocache session-context"

func newSessionContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-context",
		Short: "Emit the repocache guide as SessionStart hook context (JSON)",
		Long: `session-context prints a JSON object that terminal coding agents read
from their SessionStart hook and inject into the model's context:

    {"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"..."}}

Claude Code, Codex CLI, and Gemini CLI all accept this shape (Gemini
requires it — it rejects plain stdout). The text is generated from the
running binary, so it is always current: there is no on-disk doc to
drift after an upgrade.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printSessionContext(os.Stdout)
		},
	}
}

// sessionContextEnvelope is the JSON shape all three agents accept for
// SessionStart context injection. additionalContext is a single string,
// so the guide is JSON-escaped into it by the encoder.
type sessionContextEnvelope struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func printSessionContext(w io.Writer) error {
	var env sessionContextEnvelope
	env.HookSpecificOutput.HookEventName = "SessionStart"
	env.HookSpecificOutput.AdditionalContext = string(agents.DocContent)
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
