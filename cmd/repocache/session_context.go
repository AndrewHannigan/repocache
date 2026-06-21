package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/agents"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// SessionContextCommand is the canonical command string installed into
// each agent's SessionStart hook to inject the repocache guide as
// context. Shared so install/uninstall can match it for idempotency.
const SessionContextCommand = "repocache session-context"

func newSessionContextCmd() *cobra.Command {
	var text bool
	cmd := &cobra.Command{
		Use:   "session-context",
		Short: "Emit the repocache guide as SessionStart hook context (JSON)",
		Long: `session-context prints a JSON object that terminal coding agents read
from their SessionStart hook and inject into the model's context,
delimited by <repocache-session-context>...</repocache-session-context>
tags so it can be extracted unambiguously from surrounding hook output:

    <repocache-session-context>{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"..."}}</repocache-session-context>

Claude Code, Codex CLI, and Gemini CLI all accept this shape (Gemini
requires it — it rejects plain stdout). The text is generated from the
running binary, so it is always current: there is no on-disk doc to
drift after an upgrade. It also appends a live snapshot of the library
(the "repo list" table) so the agent knows which repos are available
without having to run it.

--text prints just the Markdown guide body, with no JSON envelope or
delimiters. opencode's plugin consumes this and pushes it into the
model's system prompt itself, so it needs the raw text, not the hook
envelope.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if text {
				_, err := fmt.Fprintln(os.Stdout, sessionContextBody())
				return err
			}
			return printSessionContext(os.Stdout)
		},
	}
	cmd.Flags().BoolVar(&text, "text", false, "print the raw guide body (no JSON envelope); for opencode's plugin")
	return cmd
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
	env.HookSpecificOutput.AdditionalContext = sessionContextBody()
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "<repocache-session-context>%s</repocache-session-context>\n", string(data))
	return err
}

// sessionContextBody is the bundled guide followed by a live snapshot of the
// library, so the agent starts each session already knowing which repos are
// available without having to run `repocache repo list` itself. The snapshot
// is best-effort: if the library can't be read it is simply omitted.
func sessionContextBody() string {
	body := string(agents.DocContent)
	if w := cwdCollisionWarning(); w != "" {
		body = w + "\n" + body
	}
	if list := repoListText(); list != "" {
		body += "\nThe library currently contains (output of `repocache repo list`):\n\n```\n" + list + "```\n"
	}
	return body
}

// cwdCollisionWarning returns a prominent callout when the agent's current
// working directory is itself a separate checkout of a library repo (matched
// by its git "origin" remote, protocol-independently). This is exactly the
// situation the guide's "local checkout collision" guardrail describes —
// surfaced here for the specific repo so the agent can't skip the check.
//
// Best-effort: any failure (not in a git repo, no origin, unreadable config)
// yields "" and the body is emitted unchanged. A cwd inside repocache's own
// data dir (a workspace, or the read-only cache) shares the upstream origin
// and is the intended place to edit, so it never warns there.
func cwdCollisionWarning() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if isWithin(cwd, paths.DataDir()) {
		return ""
	}
	origin, err := gitOriginURL(cwd)
	if err != nil {
		return ""
	}
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	return collisionWarning(cwd, origin, cfg.Repos)
}

// collisionWarning is the pure core of cwdCollisionWarning: if origin (the
// working directory's git remote) normalizes to the same host/owner/repo
// identity as one of the library repos, it returns the callout naming that
// repo; otherwise "". Both sides go through paths.DefaultName so https and
// ssh URLs for the same repo match.
func collisionWarning(cwd, origin string, repos []config.Repo) string {
	originKey, err := paths.DefaultName(origin)
	if err != nil {
		return ""
	}
	for _, r := range repos {
		key, err := paths.DefaultName(r.URL)
		if err != nil || key != originKey {
			continue
		}
		name, err := r.ResolvedName()
		if err != nil {
			name = originKey
		}
		return fmt.Sprintf("> ⚠️ HEADS UP — local checkout collision\n"+
			">\n"+
			"> Your current working directory `%s` is also library repo `%s`.\n"+
			"> They are two independent clones. Before editing anything here, STOP\n"+
			"> and ask the user which to use:\n"+
			">   - edit this checkout in place, or\n"+
			">   - create an isolated workspace: `repocache workspace new %s <branch>`\n"+
			">\n"+
			"> Do not assume — the choice decides where your commits land.\n",
			paths.Display(cwd), name, name)
	}
	return ""
}

// gitOriginURL returns the "origin" remote URL of the git repo containing dir,
// erroring if dir is not in a git repo or has no origin remote.
func gitOriginURL(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isWithin reports whether path is dir or a descendant of it.
func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
