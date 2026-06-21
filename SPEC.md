# repocache — specification

This document defines the intended behavior of `repocache`. It is the authoritative reference for implementation. When implementation and SPEC disagree, SPEC wins (or SPEC is amended; never silently divergent).

## 1. Contract

`repocache` provides a terminal coding agent with two things:

1. A **read-only local mirror** ("the cache") of a user-curated set of git repositories, kept on the local filesystem in a known location, refreshable with one command.
2. A **writable workspace** for any (repo, branch) pair, derived from the cache via `git clone --reference` so it doesn't duplicate object storage on disk.

The agent reads code by searching the cache directly with standard tools (`rg`, `grep`, `git`). It edits code only inside workspaces. `repocache` never wraps standard tooling that the agent can use itself; its surface is restricted to the operations that only `repocache` can do.

## 2. Concepts

- **Library** — the set of repos the user has told `repocache` to track. Stored in `~/.config/repocache/config.toml`.
- **Cache repo** — a full clone of one library repo, kept on disk with its working tree marked read-only. One per library entry.
- **Workspace** — an editable clone derived from a cache repo via `git clone --reference`. Identified by `(repo, branch)`. Multiple workspaces may exist for a single cache repo, including on the same branch.
- **Agent integration** — the per-agent configuration `repocache` writes so each supported agent (a) knows the tool exists, (b) has filesystem access to the cache and workspaces, (c) refreshes the cache in the background at session start (Claude Code only).

## 3. Filesystem layout

```
~/.config/repocache/
└── config.toml                                 # tracked repos + settings

~/.repocache/
├── repos/                                      # cache
│   └── <host>/<owner>/<repo>/
│       ├── .git/                               # writable; gc.auto = 0
│       │   ├── repocache.lock                  # flock target
│       │   └── repocache.meta                  # JSON sidecar (last_sync_at, etc.)
│       └── <working tree>                      # chmod -R a-w after each sync
├── workspaces/
│   └── <host>/<owner>/<repo>/<branch>/         # full repo; clone --reference
├── logs/
│   └── bg-sync.log                             # rotating log for __bg-sync
└── .bg-sync.lock                               # global flock for __bg-sync stampede prevention
```

Rules:
- `<host>` is preserved from the repo URL (e.g. `github.com`). No flattening.
- `<owner>/<repo>` preserves case from the URL.
- Branches containing `/` produce nested directories under `workspaces/`. No escaping.
- Config dir uses `XDG_CONFIG_HOME` when set; otherwise `~/.config`.
- Data dir is `~/.repocache`.

## 4. Config file

`~/.config/repocache/config.toml`:

```toml
# Optional settings.
[settings]
bg_sync_interval = "1h"   # optional staleness gate for __bg-sync's `sync
                          # --if-older-than`. Unset (default) = refresh on
                          # every session start.

# One [[repo]] block per tracked repository.
[[repo]]
url = "https://github.com/octocat/Hello-World"
# Implicit: name = "github.com/octocat/Hello-World"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "myorg/bar"        # optional override; must be unique across the config

# A repo auto-added by an owner (see below) carries a `source` tag naming the
# owner that added it. User-added repos have no source.
[[repo]]
url = "https://github.com/octocat/Hello-World"
source = "github.com/octocat"

# One [[owner]] block per tracked user/org. sync discovers the owner's repos
# (via gh) and materializes new ones as source-tagged [[repo]] entries.
[[owner]]
url = "https://github.com/octocat"   # single-path-segment URL
# name = "github.com/octocat"         # optional override; default host/owner
# include_forks = false                  # default false
# include_archived = false               # default false
# visibility = "all"                     # all|public|private; default all
```

Rules:
- `url` is required and must be a valid git URL (HTTPS or SSH).
- `name` defaults to `<host>/<owner>/<repo>` for repos, `<host>/<owner>` for owners.
- All names must be unique across **both** repos and owners (they share one
  namespace, since commands resolve a single argument against both); duplicates → exit 7 on load.
- `source` on a repo names the owner that auto-added it (informational; managed by sync — do not hand-edit). User-added repos omit it.
- An owner URL has a single path segment (`host/owner`); a repo URL has two or more (`host/owner/repo`).
- Unknown TOML keys are ignored (forward-compatible) — older binaries tolerate `[[owner]]` and `source`.
- File is loaded with shared lock; written with exclusive lock (separate file: `~/.config/repocache/.lock`).

## 5. Commands

For each command: signature, behavior, output, exit codes used. All commands accept `--help`. `--version` prints semver and exits 0.

### 5.0 Repo name resolution

Commands that resolve a `<repo>`/`<name>` argument against the configured repos (`sync`, `rm`, `workspace new`, `workspace path`, `workspace rm`) use a single shared rule, in order:

1. **Exact match** on a repo's resolved name (`<host>/<owner>/<repo>`, or the explicit `name` override). If one matches, use it.
2. **Unambiguous suffix match.** Otherwise, match the argument against the trailing path segments of each resolved name, on segment (`/`) boundaries. `hello-world` matches `github.com/octocat/hello-world`; `octocat/hello-world` matches it too; `world` does not (not a segment boundary). If exactly one repo matches, use it.

Resolution outcomes:
- Exactly one match (by either rule) → resolved.
- Zero matches → exit 2, message `repo "<arg>" is not in the config`.
- Two or more suffix matches → exit 2, message naming the ambiguity and listing the candidate full names so the user can disambiguate.

The rule is identical across all commands; no command resolves names differently. Exact match always wins over suffix match, so a short name can never shadow a repo whose full resolved name equals that string.

The same rule applies to **owner** names (`<host>/<owner>`). `rm` and `sync` resolve an argument against both repos and owners; since names are unique across the two (§4), at most one kind matches. The rare case where an argument matches both a repo and an owner → exit 2 asking for the full name.

### 5.1 `repocache init [--agents=auto|all|none|<list>] [--no-bg-sync]`

Bootstraps repocache and optionally integrates with detected agents.

Behavior:
1. Create `~/.config/repocache/` and `~/.repocache/{repos,workspaces,logs}/` if missing.
2. Create `~/.config/repocache/config.toml` if missing, with an empty `[[repo]]` list and a comment header.
3. Determine agent set:
   - `--agents=none`: skip agent integration.
   - `--agents=auto` (default in TTY): detect by file existence (see §8). Prompt `Install repocache integration for: <list>? [Y/n]`. Default to yes.
   - `--agents=auto` in non-TTY: skip agent integration (no surprise edits in scripts/CI). Print a hint.
   - `--agents=all`: install for every supported agent, regardless of detection.
   - `--agents=claude,codex,...`: explicit list. Unknown agent name → exit 7.
4. For each selected agent, perform agent install (see §8): the allowed-dir entries and the `session-context` SessionStart hook, plus the `__bg-sync` hook unless `--no-bg-sync`. Any legacy `@REPOCACHE.md` import + on-disk doc is removed. Idempotent.
5. Print a summary of what was done.

The guide the agent sees is generated by the binary (`repocache __session-context`, §8.2), so there is nothing to refresh on upgrade — re-running `init` is only needed to add integration for a newly-installed agent.

Output (human): step-by-step lines naming each path created or skipped.
JSON: not supported (init is interactive/setup-oriented).

Exit codes: 0; 7 (config write error or `--agents=` value invalid).

### 5.2 `repocache uninstall [--agents=auto|all|<list>] [--purge]`

Reverses agent integration. By default does not delete `~/.config/repocache/` or `~/.repocache/`.

Behavior:
1. Determine agent set as in `init`.
2. For each selected agent, remove only the entries recorded in the sidecar state (see §8.5): the allowed-dir entries and the `session-context` + `__bg-sync` SessionStart hooks. Untracked user entries are preserved.
3. Clean up any legacy install: the `@REPOCACHE.md` import line and the on-disk `REPOCACHE.md` doc that older versions wrote.
4. With `--purge`: after step 3, delete both `~/.repocache/` (DataDir) and `~/.config/repocache/` (ConfigDir), removing all cached repos, workspaces, and config. Before deleting, scan all workspaces; if any have uncommitted changes or unpushed commits, list them and prompt for confirmation (`[y/N]`). If stdin is not a TTY, refuse and abort rather than destroy dirty work. A clean set of workspaces is purged without prompting.

Exit codes: 0; 7 (file write error).

### 5.3 `repocache add <url> [--name <n>] [--owner|--repo]`

Appends a `[[repo]]` or `[[owner]]` entry to `config.toml`.

Behavior:
0. Normalize `<url>`. A full URL (`scheme://…`) or scp-style remote (`git@host:owner/repo`) is taken as-is. Otherwise `<url>` is treated as shorthand: a leading segment that looks like a host (contains `.` or `:`) gets only an `https://` scheme (`github.com/owner` ⇒ `https://github.com/owner`); anything else is GitHub shorthand and is expanded against `github.com` (`owner/repo` ⇒ `https://github.com/owner/repo`, bare `owner` ⇒ `https://github.com/owner`). The normalized URL is what classification, naming, and the stored config entry all use.
1. Classify the normalized URL as a repo or an owner. Default: by path-segment count — one segment (`host/owner`) ⇒ owner, two or more (`host/owner/repo`) ⇒ repo. `--owner` / `--repo` force it; passing both → exit 7.
2. Parse URL; derive default name (`host/owner/repo` for a repo, `host/owner` for an owner). If `--name` given, use it.
3. Reject if the name already exists as a repo or an owner → exit 3.
4. Acquire exclusive lock on config; append the `[[repo]]` or `[[owner]]`; release.
5. Immediately run a `sync` scoped to the just-added entry: a repo is fetched into the cache; an owner is discovered (its repos added as `source`-tagged entries) and fetched. The sync's own output follows the `added` line.
6. For an owner, additionally check `gh` is installed and authenticated; if not, print a non-fatal warning (the entry is saved and will expand once `gh` is available).

Output (repo): `added <name>` followed by the scoped `sync` output.
Output (owner): `added owner <name>` followed by the scoped `sync` output.
Exit codes: 0; 3 (name exists); 7 (config error or both `--owner` and `--repo`); plus the `sync` exit codes (5 lock, 6 network, 8 `git` missing) if the implicit sync fails.

### 5.4 `repocache rm <name> [--force]`

Removes a repo completely: the config entry, the cache on disk, and every workspace derived from it. If `<name>` resolves to an owner, removes the owner entry and every repo it auto-added (Source == owner) in one cascade.

Behavior:
1. Resolve `<name>` per §5.0 against both repos and owners. Not found → exit 2; ambiguous, or matching both a repo and an owner → exit 2 listing candidates.
2. Gather the target repo(s): one repo, or — for an owner — every repo with `source == <owner>`. Inspect their workspaces. Unless `--force`, refuse (exit 4) if any has uncommitted or unpushed changes, listing the offending branches (checked across all managed repos up front, so the cascade is all-or-nothing).
3. For each target repo: delete its workspaces (`rm -rf` the per-repo workspaces dir), then delete its cache (acquire the exclusive per-cache-repo lock, restore writability with `chmod -R u+w` — see §8.3 — then `rm -rf`).
4. Acquire the exclusive config lock; remove the repo entries (and, for an owner, the `[[owner]]` entry) in one transaction; release.

On-disk artifacts are removed before the config entry so a failure partway through leaves the entry as a record of remaining cleanup rather than orphaning untracked files.

Exit codes: 0; 2; 4 (workspace has unsaved work, no `--force`); 5 (cache lock contended); 7.

### 5.5 `repocache ls [--json]`

Lists tracked owners, then repos with last sync time and the owner (if any) that auto-added each. The probes are deliberately cheap (a stat and a small metadata read — no recursive size walk or `git` subprocess) so the output can be embedded verbatim in `session-context` (§8.2), which runs on every session start.

Behavior:
1. Read config.
2. For each entry, stat `~/.repocache/repos/<name>/`:
   - Path (or `null` if never synced)
   - `last_sync_at` from `.git/repocache.meta` (or `null`)
3. Output table (human) or a JSON object (JSON).

Human output: when any owners are tracked, an `OWNER / REPOS` table first (each owner and its count of auto-added repos), then the repo table with columns NAME, LAST SYNC (relative), SOURCE (the owner that added the repo, or `—`).

JSON is an object with `repos` and `owners` arrays:
```json
{
  "repos": [
    {
      "name": "...",
      "url": "...",
      "source": "github.com/owner",  // omitted for user-added repos
      "path": "...|null",
      "last_sync_at": "ISO8601|null"
    }
  ],
  "owners": [
    { "name": "github.com/owner", "url": "...", "repo_count": 0 }
  ]
}
```

Exit codes: 0; 7.

### 5.6 `repocache sync [<name>...] [--if-older-than <dur>] [--jobs N]`

Fetches updates for all (or named) cache repos and refreshes their working trees. Idempotent. Safe to interrupt.

Behavior:
0. **Reconcile owners in scope** (before resolving targets, so newly-discovered repos are fetched in this same pass). Owners in scope = all owners when no args, else the args that resolve to an owner. For each, run `gh repo list <owner>` (filtered by the owner's `include_forks` / `include_archived` / `visibility`), and for every repo not already tracked, append a `source`-tagged `[[repo]]` under a brief exclusive config-lock (2s). The `gh` call happens **outside** the config lock, and the lock is released before any per-repo lock is taken, preserving the §7 lock order (config → per-repo). This step is:
   - **Additive only** — repos that disappeared upstream are never removed (that could delete a workspace with unpushed work); remove them with `rm`.
   - **Gracefully degrading** — if `gh` is missing/unauthenticated or errors, print a warning to stderr, skip that owner, and continue. Already-known repos still sync. Discovery failures do not change the exit code.
   - Not gated by `--if-older-than` (that gate is per-repo freshness, not owner enumeration), so new repos are caught even when unchanged repos are skipped.
1. Resolve target set: no args = all repos in config (including those just added in step 0); args = explicit subset per §5.0. A repo arg resolves to that repo; an **owner** arg expands to all repos with `source == <owner>`. Unknown → exit 2; ambiguous → exit 2 listing candidates.
2. For each target, in parallel up to `--jobs N` (default 4):
   1. If `<cache>` does not exist yet, clone first:
      `git clone --no-checkout --config gc.auto=0 <url> <cache>`
      If the clone fails with "directory exists" / "directory not empty" (another sync raced us to it), treat as success and proceed.
   2. Acquire exclusive flock on `<cache>/.git/repocache.lock` with a long timeout (default 5 minutes; see §7). If timeout exceeded → exit 5 for this repo.
   3. Read `.git/repocache.meta`. If `--if-older-than D` and `now - last_sync_at < D`, skip (record as skipped); release lock.
   4. Restore writability on the entire working tree, both files and directories: `chmod -R u+w <cache-tree>` (excluding `.git/`). This is required because the previous sync left files chmod a-w; git checkout cannot overwrite them otherwise.
   5. `git fetch --all --prune --tags`.
   6. `git checkout --detach origin/HEAD` (so the cache never owns a local branch).
   7. `chmod -R a-w <cache>` (excluding `.git/`). This is the final read-only state. `.git/` is always excluded so the lockfile and metadata remain writable.
   8. Write `.git/repocache.meta` with new `last_sync_at`.
   9. Release lock (also auto-released if the process dies; flock guarantee).
3. Print one line per repo with status: ✓ (synced), skipped (fresh), ✗ (error).

Output (human):
```
syncing 3 repos (jobs=4)
  github.com/octocat/Hello-World  ✓  142 MB  (3.2s)
  github.com/foo/bar                 -  skipped (synced 4 min ago)
  github.com/baz/qux                 ✗  fetch failed: authentication required
2 of 3 ok; 1 failed; 0 skipped
```

JSON: NDJSON per-repo with `{name, status: "ok"|"skipped"|"error", duration_ms, error?}`.

Exit codes: 0 (all ok or skipped); 2 (unknown name); 5 (lock contention timed out for any repo); 6 (any network failure); 7 (config); 8 (`git` not on PATH).

### 5.7 `repocache workspace new <repo> <branch> [--base <branch>]`

Creates a workspace at `~/.repocache/workspaces/<name>/<branch>/` derived from the cache repo via `--reference`.

Behavior:
1. Resolve `<repo>` to a config entry per §5.0; if not in config → exit 2; if ambiguous → exit 2 listing candidates.
2. Compute workspace path. If exists → exit 3.
3. Sync the repo first (equivalent to `repocache sync <repo>`, §5.12) so the
   workspace forks from up-to-date code. This clones the repo into the cache
   if it isn't cached yet. If the sync fails (e.g. offline):
   - cache already exists → warn on stderr and proceed from the existing cache.
   - cache does not exist → exit 6 (network), or exit 5 if the failure was a lock timeout.
4. Acquire shared flock on cache repo.
5. If `<branch>` exists on origin (check `refs/remotes/origin/<branch>` in cache):
   - `git clone --reference <cache> --branch <branch> <url> <workspace>`
6. Else (new branch):
   - Resolve base: `--base` value if given, else read `origin/HEAD` symbolic ref from cache.
   - `git clone --reference <cache> --branch <base> <url> <workspace>`
   - `git -C <workspace> checkout -b <branch>`
7. Release cache lock.
8. Print absolute workspace path on stdout (nothing else; sync progress and warnings go to stderr).

Notes:
- The new workspace's `origin` points at the upstream URL. `git push` works normally.
- New branches have no upstream tracking until the user runs `git push -u origin <branch>`.
- Because `new` syncs first, the old "not in cache; run sync first" failure is gone — an uncached repo is fetched on demand.

Exit codes: 0; 3 (workspace exists); 5 (locked); 6 (clone/sync failed); 7 (config); 8 (`git` not on PATH).

### 5.8 `repocache workspace list [--json]`

Lists all workspaces under `~/.repocache/workspaces/`.

For each workspace, reports:
- `repo` (derived from path)
- `branch` (derived from path)
- `path` (absolute)
- `dirty` (true if `git status --porcelain` non-empty)
- `unpushed` (count from `git log @{u}..` if upstream set; null otherwise)
- `mtime_age` (relative, age of most recent file in working tree)

Human table: REPO, BRANCH, DIRTY, UNPUSHED, AGE, PATH.
JSON: array of objects.

Exit codes: 0.

### 5.9 `repocache workspace path <repo> <branch>`

Prints the absolute workspace path on stdout. No other output on success.

Behavior:
1. Compute the path. If does not exist on disk → exit 2.
2. Print path.

Output is the bare path so the caller can address the workspace directly.

Exit codes: 0; 2.

### 5.10 `repocache workspace rm <repo> <branch> [--force]`

Deletes a workspace, refusing if it has uncommitted or unpushed changes (unless `--force`).

Behavior:
1. Compute path; if missing → exit 2.
2. Check `git status --porcelain` (uncommitted) and `git log @{u}..` (unpushed). If either non-empty and not `--force` → exit 4 with a clear message naming the count of each.
3. `rm -rf <workspace>`.

Exit codes: 0; 2; 4.

### 5.11 `repocache help [<topic>]`

`repocache help` with no arg: prints a one-screen overview — tagline, commands, quick example.

`repocache help <topic>`: prints long-form docs on the named command or concept. Topics include each command name plus: `concepts`, `agents`, `auth`, `locking`.

Designed for the agent to consult on demand without bloating the session-context guide.

Exit codes: 0; 2 (unknown topic).

### 5.12 `repocache __bg-sync` (internal, undocumented in `--help`)

Invoked by each agent's SessionStart hook. Not for end users.

Behavior:
1. Acquire non-blocking exclusive flock on `~/.repocache/.bg-sync.lock`. If already held by another process, exit 0 immediately (no error, no message).
2. If no repo has ever been synced (no `.git/repocache.meta` exists for any tracked repo):
   - Print to stdout: `repocache: cache is empty. Run \`repocache sync\` to fetch your tracked repos.`
   - Release lock; exit 0. (Claude Code and Codex surface plain stdout as session context.)
3. Otherwise:
   - Double-fork to detach from the session.
   - Redirect stdout/stderr to `~/.repocache/logs/bg-sync.log` (append, with rotation at e.g. 5 MB).
   - Exec `repocache sync --if-older-than <bg_sync_interval>`. If `bg_sync_interval` is unset (the default), no `--if-older-than` gate is applied and the cache refreshes on every session start.
   - Release lock on exit.

Exit codes: 0 (always; bg failures don't propagate).

### 5.13 `repocache status [<repo>]`

Reports sync health from the on-disk `.git/repocache.meta` sidecars. No
network, no `git` subprocess, no locks — like `repo list` (§5.5) the probes
are a cheap meta read per repo. It shares its failure-collection logic with
the `session-context` staleness banner (§8.2), so what the agent sees at
session start and what `status` prints stay consistent.

Behavior:
1. Read config.
2. **No argument** — list every tracked repo whose most recent sync attempt
   failed (`last_error` set in its meta), newest failure first, each with the
   age of its last *successful* sync. If none failed, print `All N tracked
   repos synced cleanly.`
3. **`<repo>`** (resolved per §5.0) — print that repo's last good sync time,
   and if its last attempt failed, when it failed, the full captured git
   error, and a one-line "likely cause" that maps common git failures (auth,
   repo-not-found, network, lock contention, disk-full) to a suggested fix.
   The cause is a best-effort string heuristic over git's output, never
   authoritative. An unsynced repo prints a "not synced yet" hint instead.

Output (human): a summary table (no arg) or a per-repo detail block. No JSON
mode.

Exit codes: 0; 2 (`<repo>` not in config); 7 (config unreadable).

## 6. Read-only enforcement

Cache working trees are made read-only after each sync:
- `chmod -R a-w` is applied to the working tree (`<cache>/` excluding `<cache>/.git/`).
- `.git/` stays writable so fetch and metadata writes function.
- During sync, the working tree is temporarily made writable (`chmod -R u+w`) to permit checkout, then re-locked at the end.
- This is enforced by the OS, not the tool. A direct `sed -i` or `rm` from any process will receive `Permission denied`.

Workspaces are never chmod-restricted. They are normal git working trees.

## 7. Locking

| Scope | Lockfile | Mode | Acquire timeout | Held during |
|-------|----------|------|-----------------|-------------|
| Global bg-sync | `~/.repocache/.bg-sync.lock` | exclusive, **non-blocking** | 0s (fail-fast) | `__bg-sync` startup |
| Config file | `~/.config/repocache/.lock` | exclusive | 2s (blocking) | brief: read-modify-write of config |
| Per-cache-repo | `<cache>/.git/repocache.lock` | exclusive | **5 minutes** (blocking) | `sync` of that repo (fetch + checkout + chmod + meta write) |
| Per-cache-repo | (same file) | shared | 2s (blocking) | `workspace new` (during clone) |

- The exclusive cache-repo lock includes the `chmod` steps so `workspace new` can't observe a transitional state.
- `workspace rm`, `workspace list`, `workspace path`, `ls` take no locks.
- On timeout → exit 5 with a message naming the lock and (if recorded) the holder's PID.

### 7.1 Deadlock-freedom

Several properties combine to guarantee `repocache` never deadlocks, never wedges itself on a stale lock, and never gets blocked by its own read-only enforcement:

1. **Fixed lock-acquisition order.** A process must acquire locks top-down in the table order, and must never acquire a higher-listed lock while holding a lower-listed one:
   1. bg-sync gate
   2. config lock
   3. per-cache-repo lock

   No command holds a per-repo lock and then attempts to acquire the config lock. With a single fixed order across all code paths, no cycle is possible — and thus no deadlock.

2. **Auto-release on process exit.** `flock(2)` releases all held locks when the process dies (clean exit, signal, kill -9). A stale lockfile on disk is not a held lock — the next `flock` call succeeds immediately. No manual cleanup needed.

3. **`.git/` is excluded from read-only enforcement.** Every `chmod -R a-w` call walks only the working tree and skips `<cache>/.git/`. The lockfile (`<cache>/.git/repocache.lock`) and metadata sidecar (`<cache>/.git/repocache.meta`) always remain writable. There is no scenario where read-only enforcement prevents the next sync from acquiring the lock or recording its progress.

4. **Sync re-enables write before mutating the tree.** Step 4 of sync (§5.6) explicitly runs `chmod -R u+w` on the working tree before fetch/checkout. This is what prevents the read-only state set by the previous sync from blocking the next one.

5. **Owner can always re-chmod.** `chmod -R a-w` removes the write bit even for the owner, but the owner retains the meta-permission to change permissions. `chmod -R u+w` always works for the cache owner. The read-only invariant is not a one-way trap.

6. **Cache-creation race is handled.** If two sync processes both observe a missing cache and race to clone, the second sees a "directory exists" / "directory not empty" error from git and proceeds to acquire the per-repo lock (which serializes the rest). No coordination beyond standard filesystem semantics needed.

7. **Long timeout for sync, short for everyone else.** Sync's per-repo exclusive lock has a 5-minute timeout so that user-initiated sync does not spuriously fail due to brief contention with `__bg-sync` or another sync. Fast operations (`workspace new`, config writes) use a 2s timeout because they shouldn't ever block on a long-running sync — better to exit 5 quickly and let the user retry than to hang.

8. **`__bg-sync` never blocks the session.** The bg-sync gate is non-blocking; if another bg-sync is already running, the new one exits 0 immediately. Inside `__bg-sync`, the invoked `sync` may wait on per-repo locks up to its 5-minute timeout, but this happens after detaching from the session and therefore never delays Claude Code startup.

9. **Partial state self-heals.** If a process is killed mid-sync, the cache may be left in a partially-writable state. The lock is auto-released; the next sync re-enters cleanly: step 4 forces writability, steps 5–7 redo fetch/checkout/chmod idempotently. No deadlock, just temporary inconsistency that recovers on the next sync.

These guarantees are part of the contract. Any implementation that violates one of them is incorrect.

## 8. Agent integration

Each supported agent's install edits its settings file (allowed-dir list
+ SessionStart hooks) and records what it added in a sidecar state file
(§8.5) so `uninstall` can reverse precisely. All edits are idempotent.
The repocache guide is **not** written to disk: it is injected into the
session context by the `session-context` hook (§8.2, §8.3), so it is
always generated by the running binary and can never drift.

### 8.1 Supported agents (v1)

| Agent | Detection | Settings file | SessionStart hooks |
|-------|-----------|---------------|--------------------|
| Claude Code | `~/.claude/` exists | `~/.claude/settings.json` | session-context + bg-sync |
| Codex CLI | `~/.codex/` exists | `~/.codex/config.toml` | session-context + bg-sync (requires `/hooks` trust) |
| Antigravity CLI | `~/.gemini/antigravity-cli/` exists | `~/.gemini/config/hooks.json` | session-context + bg-sync (PreInvocation; §8.8) |
| opencode | `~/.config/opencode/` exists | n/a — plugin file (§8.9) | n/a — handled inside the plugin (§8.9) |

Antigravity is the other exception. It is a Gemini-CLI fork that shares the
`~/.gemini` config dir with the standalone Gemini CLI, but its integration
surface is its own, not Gemini's (§8.8): hooks live in a dedicated
`~/.gemini/config/hooks.json` (not `settings.json`), there is no `SessionStart`
event (the session-start equivalent is a `PreInvocation` hook), and there is no
`includeDirectories` allowlist (so it registers no paths — §8.4). Because
`~/.gemini` alone is ambiguous, detection keys off the
`~/.gemini/antigravity-cli/` app-data subdir the Antigravity CLI creates on
first run.

opencode is the third exception: it has no SessionStart
shell-command hook and no path allowlist, so it does not edit a settings
file at all. Its integration is a dropped-in plugin (§8.9). Subsections
§8.3–§8.7 describe the SessionStart settings-file agents (Claude, Codex);
§8.8 covers Antigravity's hooks.json/PreInvocation model; §8.9 covers
opencode.

### 8.2 Guide content (`session-context`)

Bundled with the binary (`//go:embed`). Emitted by `repocache
__session-context` and injected into context by each agent's SessionStart
hook (§8.3). Because it ships with the binary there is no installed copy
to drift after an upgrade. Target: ≤ 20 lines. The guide tells the agent:

- The cache lives at `~/.repocache/repos/<host>/<owner>/<repo>/` and is read-only — search with `rg`/`grep`, do not modify.
- Tracked repos: `repocache ls`.
- To edit: `repocache workspace new <repo> <branch>` prints the workspace path; make changes there, then commit, push, open PR with `gh`.
- To clean up: `repocache workspace rm <repo> <branch>`.
- To add a new repo to the library: ask the user to run `repocache add <url>`.
- For more detail: `repocache help <topic>` or `repocache <cmd> --help`.
- Branch listing and full-text search are done with native git/`rg` — not wrapped.

After the bundled guide, `session-context` appends a live snapshot of the
library — the `ls` table — so the agent starts each session knowing
which repos exist without having to run it. The snapshot is generated at hook
time (so it reflects the current cache) and is best-effort: if the library
can't be read, or nothing is tracked yet, it is omitted and only the guide is
emitted.

### 8.3 Guide injection via the `session-context` hook

Instead of importing an on-disk doc into the agent's always-loaded
instructions, the guide reaches the model through a session-start hook that
runs `repocache __session-context --agent <key>` — a SessionStart hook for
Claude and Codex (§8.6), a PreInvocation hook for Antigravity (§8.8), and
the plugin's load-time call for opencode (§8.9). The `--agent` flag selects
the output **shape** the named agent's integration expects; the guide
**content** is identical across agents. Each agent owns its shape rather
than reusing one agent's convention everywhere, so a future divergence is a
localized change.

claude gets a JSON envelope, wrapped in
`<repocache-session-context>…</repocache-session-context>` tags, which it
injects as session context from its SessionStart hook:

```json
{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"<guide>"}}
```

The `hookSpecificOutput` key is Claude Code's native SessionStart shape.

antigravity gets a different JSON shape — a PreInvocation `injectSteps`
envelope (it has no SessionStart event) — emitted only on the first model
call of a conversation. It is a bare JSON document (no delimiter tags),
carrying the guide as a `userMessage`; see §8.8.

codex and opencode get the raw Markdown body, with no envelope or
delimiters. Codex accepts plain stdout from a hook as developer context,
and plain text renders more cleanly as the injected developer message than
the JSON envelope does. opencode (§8.9) is plugin-based: it pushes the text
into the model's system prompt itself rather than handing it to hook
plumbing (`--agent opencode`).

Back-compat: a bare `repocache __session-context` (no `--agent`, the hook
command earlier versions installed) defaults to the claude shape — the
envelope every hook agent accepts — so already-installed hooks keep
working until the next `init`. `--text` is a deprecated alias for `--agent
opencode`, kept for opencode plugins installed before `--agent` existed.

Migration: older repocache versions appended `@REPOCACHE.md   #
repocache:managed` to agent memory files and wrote a `REPOCACHE.md` doc
file. `init` (and `uninstall`) remove both if present.

### 8.4 Directory registration

Two paths are added to each agent's filesystem-access list:

- `~/.repocache/repos/`
- `~/.repocache/workspaces/`

| Agent | File | Key |
|-------|------|-----|
| Claude Code | `~/.claude/settings.json` | `permissions.additionalDirectories` (array) |
| Codex CLI | `~/.codex/config.toml` | `sandbox_workspace_write.writable_roots` (array) |

The OS-level `chmod a-w` on `repos/` enforces read-only regardless of what each agent considers writable — so adding both paths uniformly is safe.

The Antigravity CLI has no per-directory allowlist (no `includeDirectories`
or equivalent in its `settings.json`), so repocache registers no paths for
it. The agent learns the repo paths from the injected guide and reaches them
via its file-access policy (or an explicit `agy --add-dir`); see §8.8.
opencode likewise has no allowlist.

### 8.5 Marker convention for idempotent edits

Every entry repocache adds must be identifiable for clean uninstall:

- **JSON/JSONC** (Claude, Antigravity): wrap repocache entries in a sentinel comment block:
  ```jsonc
  // repocache:managed:begin
  "permissions": {
    "additionalDirectories": [
      "/Users/.../repos/",
      "/Users/.../workspaces/"
    ]
  }
  // repocache:managed:end
  ```
  If the user already has the same key with their own entries, **merge**: keep user entries, add ours, mark only ours with a per-element marker comment where possible. Where per-element marker comments aren't preservable through round-trip, track our additions in a sidecar `~/.repocache/agents.state.json` so uninstall knows which entries to remove.
- **TOML** (Codex): same approach — sidecar state file is authoritative for which entries are repocache's.

The sidecar state file (`agents.state.json`) records, per agent:
```json
{
  "claude": {
    "added_paths": ["/Users/.../repos/", "/Users/.../workspaces/"],
    "added_hooks": ["repocache __session-context --agent claude", "repocache __bg-sync"]
  }
}
```

Uninstall reads this file to know exactly what to remove. If the user has hand-edited the agent's config and our entries are gone, uninstall is a no-op for those.

### 8.6 SessionStart hooks (Claude, Codex)

Each agent gets **two** SessionStart hook commands installed:

- `repocache __session-context --agent <key>` — injects the guide as
  context (§8.3). Always installed; it is how the agent learns repocache
  exists. `<key>` is the agent's own key (`claude`, `codex`),
  so the subcommand emits that agent's output shape.
- `repocache __bg-sync` — refreshes the cache in the background (§5.12).
  Installed unless `--no-bg-sync`.

(Antigravity also gets these two commands, but via its own hook model —
see §8.8.)

Each is a separate entry of the agent-specific shape below (shown for
`session-context`; the bg-sync entry is identical but for the command,
the Codex `statusMessage`, and the Google CLI `name`).

**Claude Code** — `~/.claude/settings.json`:
```jsonc
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "repocache __session-context --agent claude" } ] }
    ]
  }
}
```

**Codex CLI** — `~/.codex/config.toml`:
```toml
[[hooks.SessionStart]]
matcher = "startup|resume"

[[hooks.SessionStart.hooks]]
type = "command"
command = "repocache __session-context --agent codex"
statusMessage = "repocache session-context"
```

Codex requires the user to trust new hooks via the `/hooks` command
before they run. `repocache init` prints a one-line note after
installing the Codex hooks.

Sidecar state records each successful hook addition; uninstall reverses
exactly those entries.

### 8.7 No doc reconcile needed

Earlier designs wrote the guide to an on-disk `REPOCACHE.md` per agent,
which drifted after a binary upgrade (the binary ships newer embedded
content, but nothing re-runs `init`). That required a reconcile pass in
the `__bg-sync` worker to rewrite drifted copies.

That problem no longer exists: the guide is produced by `repocache
__session-context` from the embedded content on every session start
(§8.3), so it always reflects the running binary. There is no on-disk
copy and nothing to reconcile.

### 8.8 Antigravity (hooks.json / PreInvocation integration)

Antigravity is a Gemini-CLI fork, but it does not fit the SessionStart
settings-file model of §8.6. Three things differ (per
https://antigravity.google/docs/hooks and /docs/cli-reference):

1. **Hooks file.** Hooks live in a dedicated `~/.gemini/config/hooks.json`
   (global) — not `settings.json`. Its top level maps a hook *name* to its
   event configuration.
2. **No SessionStart event.** Antigravity's events are `PreToolUse`,
   `PostToolUse`, `PreInvocation`, `PostInvocation`, `Stop`. The
   session-start equivalent is a `PreInvocation` hook (fires before each
   model call), gated on `invocationNum==0` so it acts once per conversation.
3. **No directory allowlist** (§8.4): nothing is registered for filesystem
   access.

`init` installs the same two commands as the other agents — but as
PreInvocation hooks, each carrying `--agent antigravity` (bg-sync needs it
too, so it emits JSON rather than a plain-text hint):

```json
{
  "repocache-session-context": {
    "PreInvocation": [
      { "type": "command", "command": "repocache __session-context --agent antigravity" }
    ]
  },
  "repocache-bg-sync": {
    "PreInvocation": [
      { "type": "command", "command": "repocache __bg-sync --agent antigravity" }
    ]
  }
}
```

The hook commands read the PreInvocation payload from stdin. On the first
model call (`invocationNum==0`):

- `__session-context` emits a PreInvocation `injectSteps` envelope that adds
  the guide (§8.3) to the conversation as a `userMessage` (it persists for
  the session), with no `<repocache-session-context>` tags. Antigravity
  parses the hook's stdout as JSON, so this is a bare JSON document — no
  surrounding delimiter tags like Claude's envelope:

  ```json
  {"injectSteps":[{"userMessage":"…guide…"}]}
  ```
- `__bg-sync` runs the background refresh (§5.12) and emits `{}` (or, on an
  empty cache, an `injectSteps` message nudging `repocache sync`).

On later invocations (`invocationNum>0`) both emit `{}` so the guide is not
re-injected before every model call.

Detection keys off `~/.gemini/antigravity-cli/` (§8.1). Because the
`~/.gemini` dir is shared with the standalone Gemini CLI — for which a
removed repocache "gemini" agent once wrote hooks + `includeDirectories`
into `~/.gemini/settings.json` — `init` also strips those dead entries from
that file (Antigravity never reads them).

### 8.9 opencode (plugin-based integration)

opencode does not fit the settings-file model of §8.3–§8.6. It has no
SessionStart shell-command hook (hooks are JS/TS plugin modules, not
`{type, command}` config entries) and no per-directory access allowlist
(its read/grep tools already reach absolute paths; the `chmod a-w` on
`repos/` enforces read-only). So both the directory-registration step
(§8.4) and the settings-hook step (§8.6) are no-ops for opencode.

Instead, `init` materializes a single plugin file:

```
~/.config/opencode/plugin/repocache.js
```

opencode auto-loads any file in that directory at startup — no
`opencode.json` edit is required. The plugin shells back to the
`repocache` binary, so it never has to change across upgrades:

- **bg-sync**: the plugin module body runs once when opencode loads it
  at startup (the "session start" analog), and fire-and-forgets
  `repocache __bg-sync`. `InstallOptions.NoBgSync` does not apply —
  there is no separate hook to omit.
- **guide injection**: at load it snapshots `repocache __session-context
  --agent opencode` (the raw guide body — see below) and, on opencode's
  `experimental.chat.system.transform` hook, pushes it into the model's
  system-prompt array. The snapshot is taken once at session start, like
  the repo-list snapshot the other agents emit at hook time.

`--agent opencode` is opencode's session-context shape under §8.3: it
prints just the Markdown body with no JSON envelope or delimiters, because
the plugin injects the text itself rather than handing it to opencode's
hook plumbing. (`--text` is a deprecated alias for the same output, kept
so plugins installed before `--agent` existed keep working.)

State and uninstall: install records the plugin path in the sidecar
state's `added_files` (§8.5, a field alongside `added_paths`/
`added_hooks`); uninstall deletes exactly those files. Re-running `init`
overwrites the plugin idempotently.

Caveat: `experimental.chat.system.transform` is an experimental opencode
hook and may change across opencode releases; this integration is
inherently less stable than the declarative-hook agents.

### 8.10 Failure modes

- Agent config file is malformed (invalid JSON/TOML): `init` refuses to modify, prints a clear error pointing at the file and line, exits 7. User must fix manually before re-running.
- Agent dir does not exist (under `--agents=auto`): silently skip.

## 9. Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Not found (repo or workspace doesn't exist where expected) |
| 3 | Already exists (duplicate config entry, workspace already on disk) |
| 4 | Dirty (workspace has uncommitted or unpushed work; refused without `--force`) |
| 5 | Locked (couldn't acquire a lock within its timeout — see §7 for per-lock timeouts) |
| 6 | Network (git fetch or clone failed) |
| 7 | Config (config invalid, unreadable, or unwritable; or agent settings file malformed) |
| 8 | Missing dependency (`git` not found on PATH) |

Reserved for future use: 9–15.

## 10. Output conventions

- **TTY detection**: color enabled only when stdout is a TTY.
- **Human mode**: terse. No banners. No progress spinners by default (Git's own progress is acceptable for `sync` since it streams to stderr).
- **`--json` mode**: structured output for every command that has list/show semantics (`repo list`, `workspace list`, `sync`). NDJSON (one record per line) where multiple records stream over time, e.g. `sync` results.
- **Errors**: a fatal error prints a plain `error: <message>` line on stderr (in every mode, including `--json`) and the process exits with the matching numeric code from §9. There is no separate JSON error envelope — only list/show *output* is structured; failures are reported uniformly as the text line plus the exit code.
- **`repocache <cmd> --help`** always works for every command.

## 11. Authentication

`repocache` does not manage credentials. Every git operation defers to whatever `git clone <url>` works with in the user's shell:

- HTTPS: credential helper (`gh auth setup-git`, `git-credential-manager`, OS keychain helpers, etc.).
- SSH: `ssh-agent` and the user's SSH config for `git@github.com:...` style URLs.

If `git clone <url>` works at the user's shell, `repocache` works. If it doesn't, `repocache` exits 6 with the underlying git error. If `git` itself is not on PATH, commands that need it (`sync`, `add`, `workspace new`) fail fast with exit 8 and a one-line "install git" message rather than a raw exec error.

## 12. Concurrency model

- Multiple `repocache` processes can run safely against the same library (e.g. multiple agent sessions).
- Locks (§7) prevent corruption.
- `__bg-sync` global lock prevents stampedes from many concurrent SessionStart hooks.
- Workspace creation against a repo currently being synced waits (shared lock blocks on exclusive); waits longer than 2s → exit 5.

## 13. In scope vs out of scope (v1)

### In scope
- Library management (add/rm/ls)
- Read-only cache mirror (sync)
- Workspaces via `git clone --reference`
- Read-only enforcement via `chmod a-w`
- Agent integration for Claude Code, Codex CLI, Antigravity CLI
- Background sync via the agents' SessionStart hooks
- `--json` output throughout
- Stable exit codes

### Out of scope (deliberately)
- Wrappers around tools the agent can use itself: `repocache search`, `repocache branches`, `repocache locate`, `repocache workspace pr`. The agent uses `rg`, `git`, `gh` directly.
- Trash/undo for `workspace rm`. Dirty-check is the only safety.
- Sparse / partial-clone for huge repos. Document the limitation; revisit if a real user hits it.
- MCP server. CLI-first; can wrap later.
- Background daemons (other than the session-start hook).
- Per-repo settings (branch override, sync schedule, depth). Add when needed.
- Telemetry of any kind.
- Stats dashboard / `repocache log`.
- Cross-host smarts beyond URL parsing. GitLab/Bitbucket "just work" via standard git.

## 14. Versioning

- Semantic versioning.
- `repocache --version` prints the semver.
- The on-disk layout (paths, lockfile names, sidecar state schema) is part of the contract. Breaking changes to layout require a major version bump and a documented migration path.

## 15. Implementation status

All nine planned steps are implemented and verified end-to-end:

1. ✅ Config loader + `pkg/paths` + Cobra subcommand tree
2. ✅ `repocache init` (dirs + config only; no agent integration)
3. ✅ `repocache {add,rm,ls}`
4. ✅ `repocache sync` (parallel, locked, chmod-enforced)
5. ✅ `repocache workspace {new,list,path,rm}` (with `git clone --reference`)
6. ✅ `pkg/agents/claude.go` + wired into `init`/`uninstall`
7. ✅ `pkg/agents/{codex,antigravity}.go` + auto-detect
8. ✅ `repocache __session-context` + `__bg-sync` SessionStart hooks (all agents)
9. ✅ `repocache help <topic>` + polish + README

Subsequent work is not "implementation" but "operations": real release/
packaging, public install path, CI, polishing UX based on usage.
