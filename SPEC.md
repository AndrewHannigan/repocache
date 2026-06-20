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
bg_sync_interval = "1h"   # passed to `sync --if-older-than` from __bg-sync

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

### 5.2 `repocache uninstall [--agents=auto|all|<list>]`

Reverses agent integration. Does not delete `~/.config/repocache/` or `~/.local/share/repocache/`.

Behavior:
1. Determine agent set as in `init`.
2. For each selected agent, remove only the entries tagged with the `repocache:managed` marker (see §8.5). Untagged user entries are preserved.
3. For Claude, also remove the SessionStart hook entry tagged as `repocache:bg-sync`.
4. Delete each agent's `REPOCACHE.md` file.

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

### 5.4 `repocache repo rm <name>`

Removes a config entry. Does not delete the cache on disk.

Behavior:
1. Acquire exclusive lock on config.
2. If name not present → exit 2.
3. Remove entry; release lock.
4. Print the cache path so the user can `rm -rf` it manually if desired.

Exit codes: 0; 2; 7.

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
1. Resolve target set: no args = all repos in config; args = explicit subset. Unknown name → exit 2.
2. For each target, in parallel up to `--jobs N` (default 4):
   1. Acquire exclusive flock on `<cache>/.git/repocache.lock`. If `<cache>` does not exist yet, clone first with:
      `git clone --no-checkout --config gc.auto=0 <url> <cache>`
      Then acquire the lock.
   2. Read `.git/repocache.meta`. If `--if-older-than D` and `now - last_sync_at < D`, skip (record as skipped).
   3. Restore writability on the working tree (`chmod -R u+w <cache-tree>`) so the next steps can touch tracked files.
   4. `git fetch --all --prune --tags`.
   5. `git checkout --detach origin/HEAD` (so the cache never owns a local branch).
   6. `chmod -R a-w <cache>` (excluding `.git/`). This is the final read-only state.
   7. Write `.git/repocache.meta` with new `last_sync_at`.
   8. Release lock.
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
1. Resolve `<repo>` to a config entry; if not in config → exit 2.
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
   - Exec `repocache sync --if-older-than <bg_sync_interval>` (default 1h from config).
   - Release lock on exit.

Exit codes: 0 (always; bg failures don't propagate).

## 6. Read-only enforcement

Cache working trees are made read-only after each sync:
- `chmod -R a-w` is applied to the working tree (`<cache>/` excluding `<cache>/.git/`).
- `.git/` stays writable so fetch and metadata writes function.
- During sync, the working tree is temporarily made writable (`chmod -R u+w`) to permit checkout, then re-locked at the end.
- This is enforced by the OS, not the tool. A direct `sed -i` or `rm` from any process will receive `Permission denied`.

Workspaces are never chmod-restricted. They are normal git working trees.

## 7. Locking

| Scope | Lockfile | Mode | Held during |
|-------|----------|------|-------------|
| Per-cache-repo | `<cache>/.git/repocache.lock` | exclusive | `sync` (whole repo's update) |
| Per-cache-repo | (same file) | shared | `workspace new` (during clone) |
| Config file | `~/.config/repocache/.lock` | exclusive | `repo add`/`rm` while editing config |
| Global bg-sync | `~/.local/share/repocache/.bg-sync.lock` | exclusive, non-blocking | `__bg-sync` startup |

- Lock timeout (for blocking acquires): 2 seconds. On timeout → exit 5.
- The exclusive cache-repo lock includes the `chmod` steps so workspace creation can't observe a transitional state.
- `workspace rm`, `workspace list`, `repo list` take no locks.

## 8. Agent integration

Each supported agent's install touches up to three files. All edits are idempotent and tagged with the marker `repocache:managed` so `uninstall` can remove only entries we created.

### 8.1 Supported agents (v1)

| Agent | Detection | Files written | Background sync? |
|-------|-----------|---------------|------------------|
| Claude Code | `~/.claude/` exists | `~/.claude/REPOCACHE.md`, `~/.claude/CLAUDE.md`, `~/.claude/settings.json` | Yes |
| Codex CLI | `~/.codex/` exists | `~/.codex/REPOCACHE.md`, `~/.codex/AGENTS.md`, `~/.codex/config.toml` | No |
| Gemini CLI | `~/.gemini/` exists | `~/.gemini/REPOCACHE.md`, `~/.gemini/GEMINI.md`, `~/.gemini/settings.json` | No |
| OpenCode | `~/.config/opencode/` exists | `~/.config/opencode/REPOCACHE.md`, `~/.config/opencode/AGENTS.md`, `~/.config/opencode/opencode.json` | No |

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
| OpenCode | `~/.config/opencode/opencode.json` | `external_directory` (array) |

The OS-level `chmod a-w` on `repos/` enforces read-only regardless of what each agent considers writable — so adding both paths uniformly is safe.

### 8.5 Marker convention for idempotent edits

Every entry repocache adds must be identifiable for clean uninstall:

- **JSON/JSONC** (Claude, Gemini, OpenCode): wrap repocache entries in a sentinel comment block:
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

### 8.6 SessionStart hook (Claude Code only)

Added to `~/.claude/settings.json` under `hooks.SessionStart`:

```jsonc
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "repocache __bg-sync"
            // repocache:managed:bg-sync
          }
        ]
      }
    ]
  }
}
```

Sidecar state records the addition. Uninstall removes by sidecar.

### 8.7 Failure modes

- Agent config file is malformed (invalid JSON/TOML): `init` refuses to modify, prints a clear error pointing at the file and line, exits 7. User must fix manually before re-running.
- Agent dir does not exist (under `--agents=auto`): silently skip.
- OpenCode special case: when a project-level `AGENTS.md` exists, the global `AGENTS.md` is silently ignored (known upstream bug). `init` for OpenCode prints a warning to that effect.

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
- Agent integration for Claude Code, Codex CLI, Gemini CLI, OpenCode
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

## 15. Implementation order

Each step ends with a buildable, working binary.

1. Config loader + `pkg/paths` + subcommand tree
2. `repocache init` (dirs + config only; no agent integration)
3. `repocache repo {add,rm,list}`
4. `repocache sync`
5. `repocache workspace {new,list,path,rm}`
6. `pkg/agents/claude.go` + wire into `init`/`uninstall`
7. `pkg/agents/{codex,gemini,opencode}.go` + auto-detect
8. `repocache __bg-sync` + SessionStart hook (Claude only)
9. `repocache help` + polish + README updates
