package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/errs"
)

func newHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [topic]",
		Short: "Long-form docs on a command or concept",
		Long: `help prints prose documentation for a command or concept.
With no topic, prints an overview.

Topics: ` + strings.Join(topicList(), ", "),
		Args:               cobra.MaximumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := "overview"
			if len(args) == 1 {
				topic = args[0]
			}
			return runHelp(topic)
		},
	}
}

func runHelp(topic string) error {
	text, ok := helpTopics[topic]
	if !ok {
		return errs.New(errs.NotFound, "unknown topic %q (try: %s)",
			topic, strings.Join(topicList(), ", "))
	}
	fmt.Print(text)
	return nil
}

func topicList() []string {
	keys := make([]string, 0, len(helpTopics))
	for k := range helpTopics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var helpTopics = map[string]string{

	"overview": `repocache — read-only library of git repos for terminal coding agents

The whole loop:

    repocache init                          # one-time: dirs + agent integration
    repocache repo add <git-url>            # add a repo to the library
    repocache sync                          # fetch + chmod a-w on the working tree
    cd "$(repocache workspace new <repo> <branch>)"   # writable clone
    # ... edit, commit, push ...
    repocache workspace rm <repo> <branch>  # tear down

Commands:
  init          bootstrap + integrate with detected agents
  uninstall     reverse agent integration
  repo          {add,rm,list} of tracked repos
  sync          fetch tracked repos and re-apply read-only chmod
  workspace     {new,list,path,rm} of writable workspaces
  help <topic>  long-form docs

Topics: agents, auth, concepts, init, locking, repo, sync, workspace

For SPEC see: https://github.com/AndrewHannigan/repocache/blob/main/SPEC.md
`,

	"concepts": `Concepts

Library
  The set of repos you've told repocache to track. Stored in
  ~/.config/repocache/config.toml. Edit via 'repocache repo add/rm/list'.

Cache repo
  A full clone of one library repo, kept on disk with its working tree
  marked read-only (chmod -R a-w). One per library entry. Lives under
  ~/.local/share/repocache/repos/<host>/<owner>/<repo>/.

Workspace
  An editable clone of a cache repo, derived via 'git clone --reference'.
  Identified by (repo, branch). Multiple workspaces may exist per cache
  repo, even on the same branch. Lives under
  ~/.local/share/repocache/workspaces/<host>/<owner>/<repo>/<branch>/.

Agent integration
  Per-agent edits that 'repocache init' makes so each terminal agent:
  (a) knows repocache exists (via an @REPOCACHE.md @import in its
  always-loaded instructions), (b) has filesystem access to the cache
  and workspaces directories, (c) refreshes the cache in the
  background at session start (Claude Code only).
`,

	"init": `init — bootstrap + agent integration

Creates the repocache config and data directories if missing, writes an
empty config file, and in TTY mode prompts to install integration for
each detected agent.

Flags:
  --agents=auto|all|none|<list>   which agents to integrate (default auto)
  --no-bg-sync                    skip the Claude SessionStart bg-sync hook
  --print-agent-doc               print the embedded REPOCACHE.md and exit

Modes:
  auto (TTY)    detect installed agents and prompt to install
  auto (non-TTY) skip agent integration silently (scripted use)
  all           install for every supported agent, even undetected ones
  none          skip agent integration entirely
  <list>        comma-separated agent keys, e.g. claude,codex

Re-running init is safe: directories, @import lines, and settings.json
entries are all idempotent. The embedded REPOCACHE.md is overwritten on
each run, so re-init after upgrading refreshes the agent's doc.

To reverse only the agent integration (without removing your config or
cache), use 'repocache uninstall'.
`,

	"uninstall": `uninstall — reverse agent integration

Removes the entries 'repocache init' added to each agent's config:
  - the @REPOCACHE.md line in CLAUDE.md / AGENTS.md / GEMINI.md
  - the allowed-directory entries in settings.json / config.toml
  - (Claude only) the SessionStart bg-sync hook
  - the REPOCACHE.md file in each agent's config dir

Uses a sidecar state file
(~/.local/share/repocache/agents.state.json) to know exactly which
entries are repocache's, so user-added entries in the same files are
preserved.

Does NOT delete ~/.config/repocache/ or ~/.local/share/repocache/.
`,

	"repo": `repo — manage the library

  repocache repo add <url> [--name <n>]
    Add a repo to the library. Name defaults to <host>/<owner>/<repo>
    derived from URL. --name overrides. Does not fetch — run 'sync'.
    Exit 3 if the name already exists.

  repocache repo rm <name> [--force]
    Remove a repo completely: the config entry, the cache on disk, and
    every workspace derived from it. Refuses if any workspace has
    uncommitted or unpushed changes unless --force is given. Restores
    write permissions on the read-only cache tree automatically.

  repocache repo list [--json]
    Show tracked repos with last sync, on-disk size, branch count.
`,

	"sync": `sync — fetch repos and re-apply read-only chmod

  repocache sync [<name>...] [--if-older-than <dur>] [--jobs N] [--json]

Behavior per repo:
  1. Clone if missing (with gc.auto=0).
  2. Acquire exclusive flock on the cache repo (5 min timeout).
  3. If --if-older-than D and last_sync_at is fresher than D, skip.
  4. chmod -R u+w on the working tree (excluding .git/) so checkout works.
  5. git fetch --all --prune --tags
  6. git checkout --detach origin/HEAD
  7. chmod -R a-w on the working tree (excluding .git/) — read-only again.
  8. Write .git/repocache.meta with new last_sync_at.

Parallelism via --jobs (default 4). Per-repo locks serialize concurrent
syncs of the same repo. Aggregate exit:
  5 if any per-repo lock acquisition timed out
  6 if any git fetch/clone failed
  else 0

The background variant ('repocache __bg-sync', invoked by Claude's
SessionStart hook) wraps this with a global flock so multiple sessions
don't stampede.
`,

	"workspace": `workspace — manage writable workspaces

A workspace is a git clone created with --reference against the cache,
sharing object storage but with independent refs. Edits happen here.

  repocache workspace new <repo> <branch> [--base <branch>]
    Clone --reference into ~/.local/share/repocache/workspaces/.../<branch>/.
    If <branch> exists on origin, check it out. Otherwise create it off
    <base> (or origin/HEAD). Prints the absolute workspace path on stdout.
    Common pattern: cd "$(repocache workspace new <repo> <branch>)"

  repocache workspace list [--json]
    Every workspace with repo, branch, dirty state, unpushed-commit count,
    age of the newest file.

  repocache workspace path <repo> <branch>
    Print absolute workspace path (for cd $(...)). Exit 2 if missing.

  repocache workspace rm <repo> <branch> [--force]
    Delete workspace dir. Refuses with exit 4 if dirty or unpushed
    unless --force.

The workspace's origin remote points at the upstream URL, not the cache,
so 'git push' works normally. New branches have no upstream until your
first 'git push -u origin <branch>'.
`,

	"agents": `agents — terminal coding agent integration

Supported (auto-detected by config-dir presence):

  claude    ~/.claude/        — Claude Code
  codex     ~/.codex/         — OpenAI's Codex CLI
  gemini    ~/.gemini/        — Google's Gemini CLI
  opencode  ~/.config/opencode/ — sst/opencode

For each, 'repocache init' writes (idempotently, tagged with the marker
'repocache:managed' so 'uninstall' can reverse precisely):

  1. ~/.<agent>/REPOCACHE.md — a 12-line doc describing the library
  2. The @import line in the always-loaded instructions file
     (CLAUDE.md / AGENTS.md / GEMINI.md)
  3. The cache + workspaces dirs in the allowed-filesystem-paths
     setting (additionalDirectories / writable_roots /
     includeDirectories / external_directory)
  4. (Claude only) A SessionStart hook running 'repocache __bg-sync'

OpenCode caveat: when a project-level AGENTS.md exists, OpenCode
silently ignores the global one — including the @REPOCACHE.md line we
added. Upstream bug; nothing repocache can do from its side.
`,

	"auth": `auth — repocache delegates to git

Repocache does not manage credentials. Every git operation defers to
whatever 'git clone <url>' already works with in your shell:

  HTTPS  — credential helper ('gh auth setup-git',
           'git-credential-manager', OS keychain helpers)
  SSH    — your ssh-agent for git@github.com:... URLs

If 'git clone <url>' works in your shell, repocache works. If it
doesn't, sync exits 6 with the underlying git error.
`,

	"locking": `locking — deadlock-free by design

Three lock scopes:

  global bg-sync  ~/.local/share/repocache/.bg-sync.lock
                  exclusive, non-blocking. Held only by __bg-sync workers.

  config          ~/.config/repocache/.lock
                  exclusive, 2s timeout. Held briefly for config edits
                  (repo add/rm).

  per-cache-repo  <cache>/.git/repocache.lock
                  exclusive (5min timeout) for sync, shared (2s timeout)
                  for workspace new.

Fixed acquisition order: bg-sync → config → per-repo. No code path
acquires in reverse. flock auto-releases on process exit (including
SIGKILL).

The read-only enforcement (chmod -R a-w on the cache tree) excludes
.git/, so the lockfile and metadata sidecar remain writable. Sync's
first action after acquiring its lock is 'chmod -R u+w' to re-enable
write before checkout.

The chmod is a UX gotcha for cleanup: 'rm -rf ~/.local/share/repocache/
repos/<name>' will fail. Run 'chmod -R u+w' first.

See SPEC §7.1 for the full deadlock-freedom argument.
`,
}
