// Package agents handles installing and uninstalling repocache
// integration into each supported terminal coding agent.
package agents

import (
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// DocContent is the repocache guide bundled into the binary. It is the
// body emitted by `repocache __session-context` and injected into each
// agent's context via its SessionStart hook. Because it ships with the
// binary, it is always current — there is no installed copy to drift.
//
//go:embed embed/guide.md
var DocContent []byte

// Marker is the string used in inline comments and sidecar state to
// identify entries repocache added. See SPEC §8.5.
const Marker = "repocache:managed"

// InstallOptions tunes a per-agent install. Most agents ignore most options.
type InstallOptions struct {
	NoBgSync bool // Claude only: skip the SessionStart hook.
}

// Agent is the interface every supported agent implements.
type Agent interface {
	Key() string    // stable lower-case identifier: "claude", "codex", ...
	Name() string   // display name: "Claude Code"
	Detected() bool // is the agent installed (config dir present)?
	Install(opts InstallOptions) (Installed, error)
	Uninstall(prev Installed) error
	// SessionContextOutput renders the repocache guide body into the exact
	// shape this agent's session-context integration expects (e.g. the
	// hookSpecificOutput JSON envelope for the hook-based agents, or the raw
	// Markdown body for opencode's plugin). The content is identical across
	// agents; only the surrounding shape differs. See session_context.go.
	SessionContextOutput(body string) (string, error)
}

// Installed records what an agent's Install did, so Uninstall can
// reverse exactly those changes.
type Installed struct {
	AddedPaths []string `json:"added_paths,omitempty"`
	AddedHooks []string `json:"added_hooks,omitempty"`
	// AddedFiles are whole files repocache materialized (not edits to an
	// existing settings file). Used by agents like opencode whose
	// integration is a dropped-in plugin file rather than config edits;
	// uninstall deletes exactly these.
	AddedFiles []string `json:"added_files,omitempty"`
}

// All returns the registered set of agents. New agents are added here.
func All() []Agent {
	return []Agent{
		NewClaude(),
		NewCodex(),
		NewAntigravity(),
		NewOpencode(),
	}
}

// ByKey returns the agent with the given key, or nil.
func ByKey(key string) Agent {
	for _, a := range All() {
		if a.Key() == key {
			return a
		}
	}
	return nil
}

// SelectByFlag interprets a --agents flag value into a list of agents.
// Valid values: "auto" (only detected), "all" (every registered),
// "none" (empty), or a comma-separated list of keys.
func SelectByFlag(value string) ([]Agent, error) {
	switch value {
	case "none":
		return nil, nil
	case "all":
		return All(), nil
	case "auto":
		var out []Agent
		for _, a := range All() {
			if a.Detected() {
				out = append(out, a)
			}
		}
		return out, nil
	}
	keys := strings.Split(value, ",")
	out := make([]Agent, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		a := ByKey(k)
		if a == nil {
			return nil, fmt.Errorf("unknown agent %q (valid: %s)", k, strings.Join(allKeys(), ", "))
		}
		out = append(out, a)
	}
	return out, nil
}

func allKeys() []string {
	out := make([]string, 0)
	for _, a := range All() {
		out = append(out, a.Key())
	}
	sort.Strings(out)
	return out
}

// PathsToRegister returns the two repocache directories that every agent
// must be told it can access.
func PathsToRegister() []string {
	return []string{paths.ReposDir(), paths.WorkspacesDir()}
}

// installHooks installs the hook commands every agent gets: session-context
// (always — it replaces the old @import as how the agent learns about
// repocache) and bg-sync (unless --no-bg-sync). sessionContextCmd and bgSyncCmd
// are the agent-specific commands; both may carry a trailing --agent <key> so
// the subcommand renders the shape that agent expects (antigravity needs it on
// bg-sync too, to emit JSON). ensure is an agent-specific closure that adds one
// hook entry for a command and reports whether it was newly added. Returns the
// commands added this call, for the install state.
func installHooks(opts InstallOptions, sessionContextCmd, bgSyncCmd string, ensure func(command string) (bool, error)) ([]string, error) {
	commands := []string{sessionContextCmd}
	if !opts.NoBgSync {
		commands = append(commands, bgSyncCmd)
	}
	var added []string
	for _, command := range commands {
		ok, err := ensure(command)
		if err != nil {
			return nil, err
		}
		if ok {
			added = append(added, command)
		}
	}
	return added, nil
}

// hookLabel is the short human label for a hook command, used for the
// Codex statusMessage and (dashed) the Google CLI hook name. Both commands
// may carry a trailing --agent <key>, so they are matched by prefix rather
// than exact equality.
func hookLabel(command string) string {
	switch {
	case strings.HasPrefix(command, BgSyncCommand):
		return "repocache bg-sync"
	case strings.HasPrefix(command, SessionContextCommand):
		return "repocache session-context"
	default:
		return "repocache"
	}
}

func hookName(command string) string {
	return strings.ReplaceAll(hookLabel(command), " ", "-")
}

// ErrNotDetected is returned by Install when the agent's config dir is
// missing (called for an undetected agent via --agents=all or explicit).
var ErrNotDetected = errors.New("agent not detected (config dir missing)")
