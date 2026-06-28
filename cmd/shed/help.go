package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/errs"
)

// overviewTopline is the first line of the curated overview, shared by
// `shed help`, bare `shed`, and `shed --help`.
const overviewTopline = "shed — git repo management for terminal coding agents"

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
	if alias, ok := helpAliases[topic]; ok {
		topic = alias
	}
	text, ok := helpTopics[topic]
	if !ok {
		return errs.New(errs.NotFound, "unknown topic %q (try: %s)",
			topic, strings.Join(topicList(), ", "))
	}
	fmt.Print(text)
	return nil
}

// helpAliases lets `shed help <command>` resolve to the topic that documents
// it, even when several commands share one topic (e.g. add/rm/ls → library).
// Without these, `shed help add` would error despite `add` being a real command.
var helpAliases = map[string]string{
	"add":       "library",
	"rm":        "library",
	"ls":        "library",
	"repo":      "library",
	"new":       "workspace",
	"path":      "workspace",
	"uninstall": "init",
	"purge":     "init",
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

	"overview": `shed — git repo management for terminal coding agents

Manages a read-only store of your git repos and hands your agents isolated,
writable workspaces to make changes. Run 'shed init' to begin.

Supports claude, cursor-agent, and opencode.

The whole loop:

    # One-time init to teach your agents how to use shed
    shed init

    # Add a repo or a user/org to the library (GitHub short-form allowed)
    shed add AndrewHannigan/shed       # Add a repo
    shed add octocat                   # Add an owner, auto-sync future repos

    # That's it. Your agent knows how to use shed from here and will
    # manage workspaces on-demand.

Commands:
  add           add a repo (or a whole user/org) to the library
  help <topic>  long-form docs (also accepts a command name, e.g. 'shed help add')
  history       show recent shed commands
  init          bootstrap + integrate with detected agents (--uninstall reverses it)
  ls            list owners, repos, and workspaces (everything shed manages)
  owner         {ls,add,rm} of tracked users/orgs
  prune         delete workspaces whose work has already landed
  repo          {ls,add,rm} of the read-only repo library
  resume        reopen the agent session that created a workspace
  rm            remove tracked repos or owners
  status        report sync health; show a repo's error and the likely fix
  sync          fetch tracked repos and re-apply read-only chmod (usually automatic)
  workspace     {new,ls,path,rm} of writable workspaces

Topics: agents, auth, concepts, history, init, library, locking, owner, prune, sync, workspace
`,

	"concepts": `Concepts

Library
  The set of repos you've told shed to track. Stored in
  ~/.config/shed/config.toml. Edit via 'shed add/rm/ls'.

Repo store
  A full clone of one library repo, kept on disk with its working tree
  marked read-only (chmod -R a-w). One per library entry. Lives under
  ~/.shed/repos/<host>/<owner>/<repo>/.

Workspace
  An editable clone of a stored repo, derived via 'git clone --reference'.
  Identified by (repo, branch). Multiple workspaces may exist per stored
  repo, even on the same branch. Lives under
  ~/.shed/workspaces/<host>/<owner>/<repo>/<branch>/.

Agent integration
  Per-agent edits that 'shed init' makes so each terminal agent:
  (a) knows shed exists (via a SessionStart hook that injects the
  shed guide into the session context), (b) has filesystem access
  to the store and workspaces directories, (c) refreshes the store in
  the background at session start.
`,

	"init": `init — bootstrap + agent integration (--uninstall reverses it)

Creates the shed config and data directories if missing, writes an
empty config file, and in TTY mode prompts to install integration for
each detected agent.

Flags:
  --agents=auto|all|none|<list>   which agents to integrate (default auto)
  --no-bg-sync                    skip the SessionStart bg-sync hook
                                  (keeps the session-context hook)
  --uninstall                     reverse agent integration instead of
                                  installing it (see below)
  --purge                         with --uninstall, also delete the data
                                  and config dirs (see below)

Modes:
  auto (TTY)    detect installed agents and prompt to install
  auto (non-TTY) skip agent integration silently (scripted use)
  all           install for every supported agent, even undetected ones
  none          skip agent integration entirely
  <list>        comma-separated agent keys, e.g. claude,cursor

Re-running init is safe: directory and hook entries are idempotent. The
guide the agent sees is generated by the binary at session start (see
'shed __session-context'), so it stays current across upgrades with
no re-init needed.

Reversing integration ('shed init --uninstall')
  Removes exactly the entries 'shed init' added to each agent's config:
    - the allowed-directory entries in settings.json / config.toml
    - the SessionStart hooks (session-context and bg-sync)
  A sidecar state file (~/.shed/agents.state.json) records which entries
  are shed's, so any entries you added yourself are preserved. This does
  NOT remove the shed binary, and by default leaves ~/.config/shed/ and
  ~/.shed/ in place.

  Add --purge to also delete both directories, removing all stored repos,
  workspaces, and config. If any workspace has uncommitted or unpushed
  work, --purge lists those workspaces and asks for confirmation before
  deleting (and refuses when stdin is not a TTY).
`,

	"library": `library — manage tracked repos and owners

  shed add <repo> [--name <n>] [--owner|--repo]
    Add a repo to the library. <repo> may be a full git URL or GitHub
    shorthand: a bare 'owner/repo' or 'owner' is expanded against
    github.com, so 'shed add octocat/Hello-World' works.
    Name defaults to <host>/<owner>/<repo> derived from the URL. --name
    overrides. Fetches the new repo right away (runs a scoped 'sync').
    Exit 3 if the name already exists.

    If <repo> is a bare user/org (one path segment, e.g. octocat or
    https://github.com/octocat) it is tracked as an owner instead;
    sync then discovers and adds that owner's repos. Detection is automatic;
    --owner / --repo force it. See 'shed help owner'.

  shed rm <name>... [--force]
    Remove a repo completely: the config entry, the store on disk, and
    every workspace derived from it. When the removal would also delete a
    workspace, rm asks for confirmation first. Restores write permissions
    on the read-only store tree automatically.

    If <name> is an owner, removes the owner entry and every repo it
    auto-added, along with their workspaces and stores — again asking for
    confirmation first. Answering no keeps the repos: they stay on disk,
    just untied from the owner (so a later sync no longer manages them).

    Several names may be given at once ('shed rm a b c'); each is removed
    independently, so a failure on one is reported but doesn't stop the rest.

    --force skips the confirmation prompt and discards uncommitted or
    unpushed work without asking. When stdin is not a TTY, rm will not
    delete workspaces without --force: a repo removal refuses, and an
    owner is untied instead.

  shed ls [--json]
    Show everything shed manages, in three captioned sections so it's
    clear what each is:
      Owners      whole users/orgs you track; sync auto-adds their repos
      Repos       read-only reference copies, with last sync and (when an
                  owner auto-added it) which owner did
      Workspaces  isolated writable clones, with dirty/unpushed state and age
    The Owners and Workspaces sections are omitted when empty (a hint to
    create your first workspace is shown when you have repos but none yet).

These are also grouped under a 'repo' noun (mirroring 'workspace'):
'shed repo add' and 'shed repo rm' are the same as 'shed add'/'shed rm',
and 'shed repo ls' lists just the repos — where plain 'shed ls' also
includes the Owners and Workspaces sections.
`,

	"owner": `owner — track a whole user or org

Add an owner with 'shed add <owner-url>' (a URL with a single
path segment, e.g. https://github.com/octocat). On every sync,
shed lists that owner's repos and adds any new ones to the library
automatically, so repos created upstream after you start tracking are
picked up and fetched without another 'add'. This also happens in
the background at each agent session start (see 'shed help sync').

Discovery uses the 'gh' CLI — shed's only dependency beyond 'git',
and only for discovery. Once a repo has been discovered it is an ordinary
library entry that syncs with plain 'git'. So if 'gh' is missing or not
authenticated, shed degrades gracefully: it warns and skips
discovery, but already-known repos still sync.

By default an owner pulls its non-fork, non-archived repos (including
private ones you can access). Tune per owner in config.toml:

  [[owner]]
  url = "https://github.com/octocat"
  include_forks = false       # default
  include_archived = false    # default
  visibility = "all"          # all|public|private

Reconciliation is additive: repos that disappear upstream are left in
place (so a workspace with unpushed work is never deleted out from under
you). Remove them yourself with 'shed rm <name>', or drop the
whole owner with 'shed rm <owner>'.

Owners also have their own noun (mirroring 'repo' and 'workspace'):

  shed owner ls [--json]   list just the tracked owners and their repo counts
  shed owner add <owner>   track an owner (forces the owner reading, so even
                           an 'owner/repo' argument is tracked as an owner)
  shed owner rm <name>...  drop one or more owners; names resolve against
                           owners only (a repo name here is "not in the config")

These are the owner-scoped forms of 'shed add'/'shed rm'/'shed ls': use them
when you mean an owner specifically. 'shed owner add' is 'shed add --owner',
and 'shed owner rm' is 'shed rm' restricted to owners.
`,

	"sync": `sync — fetch repos and re-apply read-only chmod

  shed sync [<name>...] [--if-older-than <dur>] [--jobs N] [--json]

Before fetching, sync expands any tracked owners in scope: it lists each
owner's repos via 'gh' and adds new ones to the library, then fetches them
in the same pass (a brand-new repo has no store, so it is cloned). Naming
an owner syncs all of its repos. If 'gh' is unavailable, discovery is
skipped with a warning and already-known repos still sync. See
'shed help owner'.

Behavior per repo:
  1. Clone if missing (with gc.auto=0).
  2. Acquire exclusive flock on the stored repo (5 min timeout).
  3. If --if-older-than D and last_sync_at is fresher than D, skip.
  4. chmod -R u+w on the working tree (excluding .git/) so checkout works.
  5. git fetch --all --prune --tags
  6. git checkout --detach --force origin/HEAD (skipped if the remote is
     empty; GIT_LFS_SKIP_SMUDGE=1 keeps LFS blobs out of the store).
  7. chmod -R a-w on the working tree (excluding .git/) — read-only again.
  8. Write .git/shed.meta with new last_sync_at.

Parallelism via --jobs (default 4). Per-repo locks serialize concurrent
syncs of the same repo. Aggregate exit:
  5 if any per-repo lock acquisition timed out
  6 if any git fetch/clone failed
  else 0

The background variant ('shed __bg-sync', invoked by Claude's
SessionStart hook) wraps this with a global flock so multiple sessions
don't stampede.
`,

	"workspace": `workspace — manage writable workspaces

A workspace is a git clone created with --reference against the store,
sharing object storage but with independent refs. Edits happen here.

  shed workspace new <repo> <branch> [--base <branch>]
    Syncs the repo first (cloning it into the store if needed), so the
    workspace forks from up-to-date code; if the sync fails but a store
    exists, it warns and uses that. Then clones --reference into
    ~/.shed/workspaces/.../<branch>/. If <branch> exists on origin,
    check it out. Otherwise create it off <base> (or origin/HEAD). Prints
    the absolute workspace path on stdout; make changes there, then commit
    and push.

  shed workspace ls [--json]
    Every workspace with repo, branch, dirty state, unpushed-commit count,
    age of the newest file.

  shed workspace path <name>
    Print absolute workspace path. Names are globally unique, so the
    name alone identifies the workspace. Exit 2 if missing.

  shed workspace rm <name>... [--force]
    Delete the named workspace dirs (names are globally unique). Several
    names may be given at once; each is removed independently, so a failure
    on one doesn't stop the rest. Refuses with exit 4 if dirty or unpushed
    unless --force.

The workspace's origin remote points at the upstream URL, not the store,
so 'git push' works normally. New branches have no upstream until your
first 'git push -u origin <branch>'.

To bulk-clean workspaces whose work has already landed, see 'shed help prune'.
`,

	"prune": `prune — delete workspaces whose work has already landed

  shed prune [--dry-run] [--force] [--yes] [--if-older-than <dur>]
    Delete every workspace whose work has already landed, reclaiming the
    ones safe to delete. A workspace is reclaimed when its branch has a
    merged pull request (asked of GitHub via the gh CLI), or its own commits
    are already contained in the remote default branch (a merge- or
    rebase-merge with no PR). A workspace that never committed anything of
    its own is kept — an empty workspace has nothing to reclaim, so having
    no commits beyond the default branch is not on its own a reason to
    delete it. With --if-older-than, also reclaim workspaces whose last
    activity (newest reflog entry) is older than the given duration, e.g.
    --if-older-than 720h. Skips workspaces with uncommitted or unpushed
    changes so local work is never lost; pass --force to remove them anyway.
    Before deleting, prune lists the workspaces and asks for confirmation;
    pass --yes to skip the prompt or --dry-run to preview without deleting.

The merged-PR check is gh-driven, so gh must be installed and authenticated;
prune fails fast rather than degrade when gh can't report merge status.
`,

	"history": `history — show recent shed commands

  shed history [-n <count>] [--json]
    Print recent shed commands, newest last. Default 20; -n/--limit
    changes how many. --json emits the raw events.

What's recorded
  Only "working" commands that change the library or workspaces are logged:
  add, rm, prune, init, and workspace new/rm. Read-only queries (ls,
  status, workspace ls/path), background syncs, and the plain 'sync' command
  are not recorded, and only commands that succeed are. Each entry is the
  command exactly as you typed it, with a timestamp.

Storage and truncation
  Appended to ~/.shed/history.jsonl (one JSON object per line). The log
  is trimmed back to the most recent 200 entries, at most once every few
  minutes (a marker file debounces the trim), so it never grows without bound
  and the trim cost isn't paid on every command.
`,

	"agents": `agents — terminal coding agent integration

Supported (auto-detected by config-dir presence):

  claude       ~/.claude/             — Claude Code
  cursor       ~/.cursor/             — Cursor's CLI agent (cursor-agent)
  opencode     ~/.config/opencode/    — opencode

For claude, 'shed init' writes (idempotently, recorded in a
sidecar state file so 'shed init --uninstall' can reverse precisely):

  1. The store + workspaces dirs in the allowed-filesystem-paths
     setting (additionalDirectories / writable_roots)
  2. A SessionStart hook running 'shed __session-context --agent
     <key>', which injects the shed guide into the session context.
     --agent selects the output shape that agent expects (the content is
     the same for all). The text is generated by the binary, so it never
     drifts after an upgrade.
  3. A SessionStart hook running 'shed __bg-sync', which refreshes
     the store in the background (skip with --no-bg-sync).

cursor is also a SessionStart-hook agent, but its hooks live in
~/.cursor/hooks.json under 'hooks.sessionStart' — a flatter, camelCase
shape than Claude's. 'init' adds the same two hooks there (session-
context + bg-sync); --agent cursor emits a '{"additional_context":"…"}'
JSON object Cursor injects into the conversation. Cursor has no path
allowlist (the chmod a-w on repos/ enforces read-only), so no paths are
registered. A hand-rolled ~/.cursor/plugins/local/shed plugin, if
present, is removed so the guide isn't injected twice.

opencode has no SessionStart hook or path allowlist, so 'init' instead
drops a plugin at ~/.config/opencode/plugin/shed.js. It runs
'shed __bg-sync' at startup and injects the guide ('shed
__session-context --agent opencode', the raw body) into the model's
system prompt. 'shed init --uninstall' deletes the file.
`,

	"auth": `auth — shed delegates to git

Shed does not manage credentials. Every git operation defers to
whatever 'git clone <url>' already works with in your shell:

  HTTPS  — credential helper ('gh auth setup-git',
           'git-credential-manager', OS keychain helpers)
  SSH    — your ssh-agent for git@github.com:... URLs

If 'git clone <url>' works in your shell, shed works. If it
doesn't, sync exits 6 with the underlying git error.
`,

	"locking": `locking — deadlock-free by design

Three lock scopes:

  global bg-sync  ~/.shed/.bg-sync.lock
                  exclusive, non-blocking. Held only by __bg-sync workers.

  config          ~/.config/shed/.lock
                  exclusive, 2s timeout. Held briefly for config edits
                  (add/rm).

  per-store-repo  <store>/.git/shed.lock
                  exclusive (5min timeout) for sync, shared (2s timeout)
                  for workspace new.

Fixed acquisition order: bg-sync → config → per-repo. No code path
acquires in reverse. flock auto-releases on process exit (including
SIGKILL).

The read-only enforcement (chmod -R a-w on the store tree) excludes
.git/, so the lockfile and metadata sidecar remain writable. Sync's
first action after acquiring its lock is 'chmod -R u+w' to re-enable
write before checkout.

The chmod is a UX gotcha for cleanup: 'rm -rf ~/.shed/
repos/<name>' will fail. Run 'chmod -R u+w' first.

See SPEC §7.1 for the full deadlock-freedom argument.
`,
}
