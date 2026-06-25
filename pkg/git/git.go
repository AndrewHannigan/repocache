// Package git runs git subprocesses with a single, consistent error shape.
//
// Every cache and workspace operation shells out to git, and `shed status`
// classifies failures (auth vs. network vs. not-found) by parsing the error
// text. Routing those invocations through here keeps the
// "git <op>: <err> (output: <combined output>)" format — which status depends
// on — defined in exactly one place, so the cache and workspace packages can't
// drift from what status parses.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Run executes `git [-C dir] args...` and returns git's combined stdout+stderr.
// dir may be "" to run in the current process directory. On failure the error
// is "git <args[0]>: <err> (output: <trimmed combined output>)"; the output is
// returned in both cases so a caller can inspect it (e.g. for a benign
// "already exists" clone race) without re-running git.
func Run(dir string, args ...string) ([]byte, error) {
	return RunEnv(dir, nil, args...)
}

// RunEnv is Run with an explicit child environment (nil inherits the parent's),
// for the few invocations that must set GIT_* variables — e.g.
// GIT_LFS_SKIP_SMUDGE for the read-only mirror checkout, or GIT_TERMINAL_PROMPT
// and GIT_SSH_COMMAND for a non-interactive reachability probe.
func RunEnv(dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", gitArgs(dir, args)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w (output: %s)", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// gitArgs prepends "-C <dir>" to args when dir is set, so callers pass the
// working directory positionally instead of threading -C through every call.
func gitArgs(dir string, args []string) []string {
	if dir == "" {
		return args
	}
	return append([]string{"-C", dir}, args...)
}
