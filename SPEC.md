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

~/.local/share/repocache/
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
- Data dir uses `XDG_DATA_HOME` when set; otherwise `~/.local/share`.

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
url = "https://github.com/anthropics/claude-code"
# Implicit: name = "github.com/anthropics/claude-code"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "myorg/bar"        # optional override; must be unique across the config
```

Rules:
- `url` is required and must be a valid git URL (HTTPS or SSH).
- `name` defaults to `<host>/<owner>/<repo>` derived from URL.
- All names must be unique; duplicates → exit 7 on load.
- Unknown TOML keys are ignored (forward-compatible).
- File is loaded with shared lock; written with exclusive lock (separate file: `~/.config/repocache/.lock`).

## 5. Commands

For each command: signature, behavior, output, exit codes used. All commands accept `--help`. `--version` prints semver and exits 0.

### 5.0 Repo name resolution

Commands that resolve a `<repo>`/`<name>` argument against the configured repos (`sync`, `repo rm`, `workspace new`) use a single shared rule, in order:

1. **Exact match** on a repo's resolved name (`<host>/<owner>/<repo>`, or the explicit `name` override). If one matches, use it.
2. **Unambiguous suffix match.** Otherwise, match the argument against the trailing path segments of each resolved name, on segment (`/`) boundaries. `blackboard` matches `github.com/AndrewHannigan/blackboard`; `AndrewHannigan/blackboard` matches it too; `board` does not (not a segment boundary). If exactly one repo matches, use it.

Resolution outcomes:
- Exactly one match (by either rule) → resolved.
- Zero matches → exit 2, message `repo "<arg>" is not in the config`.
- Two or more suffix matches → exit 2, message naming the ambiguity and listing the candidate full names so the user can disambiguate.

The rule is identical across all commands; no command resolves names differently. Exact match always wins over suffix match, so a short name can never shadow a repo whose full resolved name equals that string.

### 5.1 `repocache init [--agents=auto|all|none|<list>] [--no-bg-sync] [--print-agent-doc]`

Bootstraps repocache and optionally integrates with detected agents.

Behavior:
1. Create `~/.config/repocache/` and `~/.local/share/repocache/{repos,workspaces,logs}/` if missing.
2. Create `~/.config/repocache/config.toml` if missing, with an empty `[[repo]]` list and a comment header.
3. If `--print-agent-doc`, print the embedded REPOCACHE.md to stdout and exit 0 — no filesystem changes other than step 1 and 2.
4. Determine agent set:
   - `--agents=none`: skip agent integration.
   - `--agents=auto` (default in TTY): detect by file existence (see §8). Prompt `Install repocache integration for: <list>? [Y/n]`. Default to yes.
   - `--agents=auto` in non-TTY: skip agent integration (no surprise edits in scripts/CI). Print a hint.
   - `--agents=all`: install for every supported agent, regardless of detection.
   - `--agents=claude,codex,...`: explicit list. Unknown agent name → exit 7.
5. For each selected agent, perform agent install (see §8). Idempotent.
6. For Claude Code (only), unless `--no-bg-sync`, install the SessionStart hook (see §10).
7. Print a summary of what was done.

Output (human): step-by-step lines naming each path created or skipped.
JSON: not supported (init is interactive/setup-oriented).

Exit codes: 0; 7 (config write error or `--agents=` value invalid).

### 5.2 `repocache uninstall [--agents=auto|all|<list>] [--purge]`

Reverses agent integration. By default does not delete `~/.config/repocache/` or `~/.local/share/repocache/`.

Behavior:
1. Determine agent set as in `init`.
2. For each selected agent, remove only the entries tagged with the `repocache:managed` marker (see §8.5). Untagged user entries are preserved.
3. For Claude, also remove the SessionStart hook entry tagged as `repocache:bg-sync`.
4. Delete each agent's `REPOCACHE.md` file.
5. With `--purge`: after step 4, delete both `~/.local/share/repocache/` (DataDir) and `~/.config/repocache/` (ConfigDir), removing all cached repos, workspaces, and config. Before deleting, scan all workspaces; if any have uncommitted changes or unpushed commits, list them and prompt for confirmation (`[y/N]`). If stdin is not a TTY, refuse and abort rather than destroy dirty work. A clean set of workspaces is purged without prompting.

Exit codes: 0; 7 (file write error).

### 5.3 `repocache repo add <url> [--name <n>]`

Appends an entry to `config.toml`.

Behavior:
1. Parse URL; derive default name.
2. If `--name` given, use it.
3. Reject if name already in config → exit 3.
4. Acquire exclusive lock on config; append; release.
5. Do not fetch. Print a hint to run `repocache sync`.

Output: `added <name> (run \`repocache sync\` to fetch)`
JSON: `--json` emits the added entry.
Exit codes: 0; 3 (name exists); 7 (config error).

### 5.4 `repocache repo rm <name> [--force]`

Removes a repo completely: the config entry, the cache on disk, and every workspace derived from it.

Behavior:
1. Resolve `<name>` per §5.0. If not found → exit 2; if ambiguous → exit 2 listing candidates.
2. Inspect the repo's workspaces. Unless `--force` is given, refuse (exit 4) if any workspace has uncommitted or unpushed changes, listing the offending branches.
3. Delete all workspaces for the repo (`rm -rf` the per-repo workspaces dir).
4. Delete the cache on disk: acquire the exclusive per-cache-repo lock, restore writability (`chmod -R u+w`, since the working tree is left read-only between syncs — see §8.3), then `rm -rf` the cache dir.
5. Acquire the exclusive config lock, remove the entry, release.

On-disk artifacts are removed before the config entry so a failure partway through leaves the entry as a record of remaining cleanup rather than orphaning untracked files.

Exit codes: 0; 2; 4 (workspace has unsaved work, no `--force`); 5 (cache lock contended); 7.

### 5.5 `repocache repo list [--json]`

Lists tracked repos with last sync time, on-disk size, and branch count.

Behavior:
1. Read config.
2. For each entry, stat `~/.local/share/repocache/repos/<name>/`:
   - Path (or `null` if never synced)
   - `last_sync_at` from `.git/repocache.meta` (or `null`)
   - `size_bytes` = recursive size of the cache dir (best-effort)
   - `branch_count` = count of `refs/remotes/origin/*` (or `0` if never synced)
3. Output table (human) or NDJSON array (JSON).

Human table columns: NAME, LAST SYNC (relative), SIZE, BRANCHES.

JSON object per repo:
```json
{
  "name": "...",
  "url": "...",
  "path": "...|null",
  "last_sync_at": "ISO8601|null",
  "size_bytes": 0,
  "branch_count": 0
}
```

Exit codes: 0; 7.

### 5.6 `repocache sync [<name>...] [--if-older-than <dur>] [--jobs N]`

Fetches updates for all (or named) cache repos and refreshes their working trees. Idempotent. Safe to interrupt.

Behavior:
1. Resolve target set: no args = all repos in config; args = explicit subset, each resolved per §5.0 (unknown → exit 2; ambiguous → exit 2 listing candidates).
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
  github.com/anthropics/claude-code  ✓  142 MB  (3.2s)
  github.com/foo/bar                 -  skipped (synced 4 min ago)
  github.com/baz/qux                 ✗  fetch failed: authentication required
2 of 3 ok; 1 failed; 0 skipped
```

JSON: NDJSON per-repo with `{name, status: "ok"|"skipped"|"error", duration_ms, error?}`.

Exit codes: 0 (all ok or skipped); 2 (unknown name); 5 (lock contention timed out for any repo); 6 (any network failure); 7 (config).

### 5.7 `repocache workspace new <repo> <branch> [--base <branch>]`

Creates a workspace at `~/.local/share/repocache/workspaces/<name>/<branch>/` derived from the cache repo via `--reference`.

Behavior:
1. Resolve `<repo>` to a config entry per §5.0; if not in config → exit 2; if ambiguous → exit 2 listing candidates.
2. If cache does not exist → exit 2 with hint to run `repocache sync <repo>` first.
3. Compute workspace path. If exists → exit 3.
4. Acquire shared flock on cache repo.
5. If `<branch>` exists on origin (check `refs/remotes/origin/<branch>` in cache):
   - `git clone --reference <cache> --branch <branch> <url> <workspace>`
6. Else (new branch):
   - Resolve base: `--base` value if given, else read `origin/HEAD` symbolic ref from cache.
   - `git clone --reference <cache> --branch <base> <url> <workspace>`
   - `git -C <workspace> checkout -b <branch>`
7. Release cache lock.
8. Print absolute workspace path on stdout (nothing else).

Notes:
- The new workspace's `origin` points at the upstream URL. `git push` works normally.
- New branches have no upstream tracking until the user runs `git push -u origin <branch>`.

Exit codes: 0; 2 (repo not in cache); 3 (workspace exists); 5 (locked); 6 (clone failed); 7 (config).

### 5.8 `repocache workspace list [--json]`

Lists all workspaces under `~/.local/share/repocache/workspaces/`.

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

Designed for `cd "$(repocache workspace path foo bar)"`.

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

Designed for the agent to consult on demand without bloating REPOCACHE.md.

Exit codes: 0; 2 (unknown topic).

### 5.12 `repocache __bg-sync` (internal, undocumented in `--help`)

Invoked by the Claude Code SessionStart hook. Not for end users.

Behavior:
1. Acquire non-blocking exclusive flock on `~/.local/share/repocache/.bg-sync.lock`. If already held by another process, exit 0 immediately (no error, no message).
2. If no repo has ever been synced (no `.git/repocache.meta` exists for any tracked repo):
   - Print to stdout: `repocache: cache is empty. Run \`repocache sync\` to fetch your tracked repos.`
   - Release lock; exit 0. (Claude Code surfaces stdout as session context.)
3. Otherwise:
   - Double-fork to detach from the session.
   - Redirect stdout/stderr to `~/.local/share/repocache/logs/bg-sync.log` (append, with rotation at e.g. 5 MB).
   - **Reconcile agent docs** (see §8.7): for every agent in the install
     state, rewrite its `REPOCACHE.md` if it has drifted from the binary's
     embedded copy. Best-effort; logged, never fatal.
   - Exec `repocache sync --if-older-than <bg_sync_interval>`. If `bg_sync_interval` is unset (the default), no `--if-older-than` gate is applied and the cache refreshes on every session start.
   - Release lock on exit.

Exit codes: 0 (always; bg failures don't propagate).

Note: doc reconcile runs in the detached worker, so it is gated behind
the "ever synced" check in step 2 — a freshly-upgraded cache that has
never synced refreshes its docs on the first real sync. The reconcile
runs under the bg-sync lock, so concurrent sessions can't race on the
doc writes.

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
| Global bg-sync | `~/.local/share/repocache/.bg-sync.lock` | exclusive, **non-blocking** | 0s (fail-fast) | `__bg-sync` startup |
| Config file | `~/.config/repocache/.lock` | exclusive | 2s (blocking) | brief: read-modify-write of config |
| Per-cache-repo | `<cache>/.git/repocache.lock` | exclusive | **5 minutes** (blocking) | `sync` of that repo (fetch + checkout + chmod + meta write) |
| Per-cache-repo | (same file) | shared | 2s (blocking) | `workspace new` (during clone) |

- The exclusive cache-repo lock includes the `chmod` steps so `workspace new` can't observe a transitional state.
- `workspace rm`, `workspace list`, `workspace path`, `repo list` take no locks.
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

Each supported agent's install touches up to three files. All edits are idempotent and tagged with the marker `repocache:managed` so `uninstall` can remove only entries we created.

### 8.1 Supported agents (v1)

| Agent | Detection | Files written | Background sync? |
|-------|-----------|---------------|------------------|
| Claude Code | `~/.claude/` exists | `~/.claude/REPOCACHE.md`, `~/.claude/CLAUDE.md`, `~/.claude/settings.json` | Yes |
| Codex CLI | `~/.codex/` exists | `~/.codex/REPOCACHE.md`, `~/.codex/AGENTS.md`, `~/.codex/config.toml` | Yes (requires user to run `/hooks` once to trust) |
| Gemini CLI | `~/.gemini/` exists | `~/.gemini/REPOCACHE.md`, `~/.gemini/GEMINI.md`, `~/.gemini/settings.json` | Yes |

### 8.2 REPOCACHE.md content

Bundled with the binary (`//go:embed`). Identical content per agent. Target: ≤ 20 lines. The doc tells the agent:

- The cache lives at `~/.local/share/repocache/repos/<host>/<owner>/<repo>/` and is read-only — search with `rg`/`grep`, do not modify.
- Tracked repos: `repocache repo list`.
- To edit: `cd "$(repocache workspace new <repo> <branch>)"`, commit, push, open PR with `gh`.
- To clean up: `repocache workspace rm <repo> <branch>`.
- To add a new repo to the library: ask the user to run `repocache repo add <url>`.
- For more detail: `repocache help <topic>` or `repocache <cmd> --help`.
- Branch listing and full-text search are done with native git/`rg` — not wrapped.

### 8.3 @import injection

Each agent's top-level doc file (`CLAUDE.md`/`AGENTS.md`/`GEMINI.md`) gets a line appended (creating the file if missing):

```
@REPOCACHE.md   # repocache:managed
```

The marker comment lets `uninstall` find and remove only this line.

Idempotent: if the line is already present (regardless of comment), do not append again.

### 8.4 Directory registration

Two paths are added to each agent's filesystem-access list:

- `~/.local/share/repocache/repos/`
- `~/.local/share/repocache/workspaces/`

| Agent | File | Key |
|-------|------|-----|
| Claude Code | `~/.claude/settings.json` | `permissions.additionalDirectories` (array) |
| Codex CLI | `~/.codex/config.toml` | `sandbox_workspace_write.writable_roots` (array) |
| Gemini CLI | `~/.gemini/settings.json` | `includeDirectories` (verify exact key at impl time) |

The OS-level `chmod a-w` on `repos/` enforces read-only regardless of what each agent considers writable — so adding both paths uniformly is safe.

### 8.5 Marker convention for idempotent edits

Every entry repocache adds must be identifiable for clean uninstall:

- **JSON/JSONC** (Claude, Gemini): wrap repocache entries in a sentinel comment block:
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
  If the user already has the same key with their own entries, **merge**: keep user entries, add ours, mark only ours with a per-element marker comment where possible. Where per-element marker comments aren't preservable through round-trip, track our additions in a sidecar `~/.local/share/repocache/agents.state.json` so uninstall knows which entries to remove.
- **TOML** (Codex): same approach — sidecar state file is authoritative for which entries are repocache's.
- **Markdown** (`CLAUDE.md` etc.): inline comment on the @import line: `@REPOCACHE.md   # repocache:managed`.

The sidecar state file (`agents.state.json`) records, per agent:
```json
{
  "claude": {
    "added_paths": ["/Users/.../repos/", "/Users/.../workspaces/"],
    "added_imports": ["REPOCACHE.md"],
    "added_hooks": ["bg-sync"]
  }
}
```

Uninstall reads this file to know exactly what to remove. If the user has hand-edited the agent's config and our entries are gone, uninstall is a no-op for those.

### 8.6 SessionStart hook (Claude, Codex, Gemini)

All three agents with a SessionStart-equivalent get the same wrapper
command (`repocache __bg-sync`) installed. The hook entry shape is
agent-specific.

**Claude Code** — `~/.claude/settings.json`:
```jsonc
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "repocache __bg-sync" } ] }
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
command = "repocache __bg-sync"
statusMessage = "repocache bg-sync"
```

Codex requires the user to trust new hooks via the `/hooks` command
before they run. `repocache init` prints a one-line note after
installing the Codex hook.

**Gemini CLI** — `~/.gemini/settings.json`:
```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "name": "repocache-bg-sync",
            "type": "command",
            "command": "repocache __bg-sync",
            "timeout": 5000
          }
        ]
      }
    ]
  }
}
```

Sidecar state records each successful hook addition; uninstall reverses
exactly those entries.

### 8.7 Doc reconcile on upgrade

Upgrading the repocache binary (brew, `go install`, `install.sh`) swaps
in a possibly newer embedded `REPOCACHE.md`, but nothing re-runs `init`,
so each agent's on-disk `REPOCACHE.md` drifts from what the binary ships.

To self-heal without requiring `repocache init` after every upgrade, the
detached `__bg-sync` worker reconciles docs (§5.12): for every agent in
the sidecar install state, if its `REPOCACHE.md` exists but differs from
the embedded copy, it is overwritten with the embedded copy.

- The check is **content-based**, not version-based, so `go install`
  dogfood builds (which stamp `version=dev`) heal too.
- Only the `REPOCACHE.md` file is touched — never the import line,
  allowed-dir list, or hooks. Those are presence-checked and effectively
  never change shape; reconciling them remains the job of `init`.
- A doc the user **deleted** is left deleted (drift is refreshed, files
  are not resurrected).
- Because the worker fires from any agent's SessionStart hook and
  iterates *all* agents in state, a single session in Claude, Codex, or
  Gemini refreshes the docs of every integrated agent, not just the one
  whose hook fired.

### 8.8 Failure modes

- Agent config file is malformed (invalid JSON/TOML): `init` refuses to modify, prints a clear error pointing at the file and line, exits 7. User must fix manually before re-running.
- Agent dir does not exist (under `--agents=auto`): silently skip.

## 9. Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Not found (repo or workspace doesn't exist where expected) |
| 3 | Already exists (duplicate config entry, workspace already on disk) |
| 4 | Dirty (workspace has uncommitted or unpushed work; refused without `--force`) |
| 5 | Locked (couldn't acquire lock within 2s timeout) |
| 6 | Network (git fetch or clone failed) |
| 7 | Config (config invalid, unreadable, or unwritable; or agent settings file malformed) |

Reserved for future use: 8–15.

## 10. Output conventions

- **TTY detection**: color enabled only when stdout is a TTY.
- **Human mode**: terse. No banners. No progress spinners by default (Git's own progress is acceptable for `sync` since it streams to stderr).
- **`--json` mode**: structured output for every command that has list/show semantics. NDJSON (one record per line) where multiple records stream over time, e.g. `sync` results.
- **Error envelope** in JSON mode: `{"error": "<message>", "code": "<short_code>"}` on stderr, with matching numeric exit code.
- **`repocache <cmd> --help`** always works for every command.

## 11. Authentication

`repocache` does not manage credentials. Every git operation defers to whatever `git clone <url>` works with in the user's shell:

- HTTPS: credential helper (`gh auth setup-git`, `git-credential-manager`, OS keychain helpers, etc.).
- SSH: `ssh-agent` and the user's SSH config for `git@github.com:...` style URLs.

If `git clone <url>` works at the user's shell, `repocache` works. If it doesn't, `repocache` exits 6 with the underlying git error.

## 12. Concurrency model

- Multiple `repocache` processes can run safely against the same library (e.g. multiple agent sessions).
- Locks (§7) prevent corruption.
- `__bg-sync` global lock prevents stampedes from many concurrent SessionStart hooks.
- Workspace creation against a repo currently being synced waits (shared lock blocks on exclusive); waits longer than 2s → exit 5.

## 13. In scope vs out of scope (v1)

### In scope
- Library management (add/rm/list)
- Read-only cache mirror (sync)
- Workspaces via `git clone --reference`
- Read-only enforcement via `chmod a-w`
- Agent integration for Claude Code, Codex CLI, Gemini CLI
- Background sync via Claude Code's SessionStart hook
- `--json` output throughout
- Stable exit codes

### Out of scope (deliberately)
- Wrappers around tools the agent can use itself: `repocache search`, `repocache repo branches`, `repocache locate`, `repocache workspace pr`. The agent uses `rg`, `git`, `gh` directly.
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
3. ✅ `repocache repo {add,rm,list}`
4. ✅ `repocache sync` (parallel, locked, chmod-enforced)
5. ✅ `repocache workspace {new,list,path,rm}` (with `git clone --reference`)
6. ✅ `pkg/agents/claude.go` + wired into `init`/`uninstall`
7. ✅ `pkg/agents/{codex,gemini}.go` + auto-detect
8. ✅ `repocache __bg-sync` + Claude SessionStart hook
9. ✅ `repocache help <topic>` + polish + README

Subsequent work is not "implementation" but "operations": real release/
packaging, public install path, CI, polishing UX based on usage.
