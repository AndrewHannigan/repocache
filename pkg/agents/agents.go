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

//go:embed embed/REPOCACHE.md
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
	Key() string     // stable lower-case identifier: "claude", "codex", ...
	Name() string    // display name: "Claude Code"
	Detected() bool  // is the agent installed (config dir present)?
	DocPath() string // absolute path to this agent's managed REPOCACHE.md
	Install(opts InstallOptions) (Installed, error)
	Uninstall(prev Installed) error
}

// Installed records what an agent's Install did, so Uninstall can
// reverse exactly those changes.
type Installed struct {
	AddedPaths   []string `json:"added_paths,omitempty"`
	AddedImports []string `json:"added_imports,omitempty"`
	AddedHooks   []string `json:"added_hooks,omitempty"`
}

// All returns the registered set of agents. New agents are added here.
func All() []Agent {
	return []Agent{
		NewClaude(),
		NewCodex(),
		NewGemini(),
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

// ErrNotDetected is returned by Install when the agent's config dir is
// missing (called for an undetected agent via --agents=all or explicit).
var ErrNotDetected = errors.New("agent not detected (config dir missing)")
