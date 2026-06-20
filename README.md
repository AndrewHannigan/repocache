# repocache

A CLI that gives terminal coding agents a **read-only local library of git repositories** and a way to **derive writable workspaces** from it via `git clone --reference`.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go) ![Status](https://img.shields.io/badge/status-alpha-orange)

---

## Highlights

- **Read-only mirror, enforced by the OS** — every sync re-applies `chmod -R a-w` on each cache tree. The agent can search but never accidentally write.
- **O(seconds) workspaces** — `git clone --reference` borrows objects from the cache; new workspaces don't duplicate the repo's history on disk.
- **Auto-integrates with terminal agents** — one `repocache init` registers the directories, injects a short doc into each agent's always-loaded instructions, and (for Claude Code) installs a `SessionStart` background-sync hook. Supports Claude Code, Codex CLI, Gemini CLI, OpenCode.
- **No wrappers around tools the agent already has** — no `repocache search`, no `repocache branches`. Search with `rg`, list branches with `git`. Repocache only does the things only repocache can do.
- **One binary, one config file** — TOML for the library, JSON sidecar per cache repo, single `flock` lockfile per cache repo. No daemons.
- **Deadlock-free by construction** — fixed lock-acquisition order, `flock` auto-release on process exit, `.git/` always excluded from the read-only chmod. See [SPEC §7.1](./SPEC.md#71-deadlock-freedom).

---

## Quick start

```bash
# Build from source (repo is private; no public install path yet)
git clone https://github.com/AndrewHannigan/repocache
cd repocache && go build -o /usr/local/bin/repocache ./cmd/repocache

# One-time setup: creates dirs + config, prompts to integrate with
# detected agents (Claude Code, Codex CLI, Gemini CLI, OpenCode).
repocache init

# Add a repo to your library
repocache repo add https://github.com/anthropics/claude-code

# Fetch
repocache sync

# Search the cache directly with standard tools
rg "slash command" ~/.local/share/repocache/repos/github.com/anthropics/claude-code/

# Spin up a workspace to make edits
cd "$(repocache workspace new anthropics/claude-code fix-bug)"
# ... edit, commit, push, open PR with `gh` ...
repocache workspace rm anthropics/claude-code fix-bug
```

---

## How it works

Two parallel directory trees on disk:

```
~/.local/share/repocache/
├── repos/                              # cache: full clones, chmod a-w
│   └── <host>/<owner>/<repo>/
└── workspaces/                         # editable clones via --reference
    └── <host>/<owner>/<repo>/<branch>/
```

- **The cache** is a full clone of every tracked repo. After every `sync`, the working tree gets `chmod -R a-w` (excluding `.git/`). Any direct write — by the agent or any other process — gets `Permission denied` from the OS.
- **Workspaces** are independent clones created with `git clone --reference <cache>`. They share object storage with the cache via `.git/objects/info/alternates`, so disk cost is just the working tree + workspace-local refs. The cache's read-only contract is unaffected.

Config lives at `~/.config/repocache/config.toml`:

```toml
[[repo]]
url = "https://github.com/anthropics/claude-code"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "myorg/bar"   # optional override; default derived from URL
```

---

## Supported agents

`repocache init` auto-detects each agent by config-dir presence and, with your confirmation, writes three things per agent:

| Agent | Doc file | Allowed-dir setting | SessionStart bg-sync |
|-------|----------|---------------------|----------------------|
| Claude Code | `~/.claude/CLAUDE.md` ← `@REPOCACHE.md` | `~/.claude/settings.json` → `permissions.additionalDirectories` | Yes |
| Codex CLI | `~/.codex/AGENTS.md` ← `@REPOCACHE.md` | `~/.codex/config.toml` → `sandbox_workspace_write.writable_roots` | No |
| Gemini CLI | `~/.gemini/GEMINI.md` ← `@REPOCACHE.md` | `~/.gemini/settings.json` → `includeDirectories` | No |
| OpenCode | `~/.config/opencode/AGENTS.md` ← `@REPOCACHE.md` | `~/.config/opencode/opencode.json` → `external_directory` | No |

All edits are idempotent and tagged with a `repocache:managed` marker so `repocache uninstall` removes only what repocache added.

**Why `@REPOCACHE.md` and not a skill?** Skills load lazily — the agent only sees them when something triggers. The whole value of repocache is that the agent reaches for it *before* it would otherwise clone something into `/tmp` or fail to find a repo. `@REPOCACHE.md` is loaded every session.

---

## Why `git clone --reference` instead of `git worktree`

Both let multiple working trees share an underlying object store. They are *not* interchangeable.

| Concern | `git worktree` | `git clone --reference` |
|---------|----------------|-------------------------|
| Refs / branches | **Shared** — all worktrees see the same branch namespace | **Independent** — each clone has its own refs |
| Same branch in two places | Forbidden — git refuses | Fine — workspaces are unaware of each other |
| Cache's read-only chmod | Leaks: committing in a worktree updates `<cache>/.git/refs/...` | Doesn't leak: workspace refs live in workspace |
| Workspace deletion | `git worktree remove` (or manual cleanup); fragile | `rm -rf` |
| Disk savings | Yes | Yes (via `objects/info/alternates`) |
| Push / fetch from workspace | Affects shared refs | Affects workspace refs only |

Concretely, worktrees break repocache's contract in several ways:

1. **The read-only invariant leaks.** A worktree of the cache *is* the cache, in a real sense — committing in the worktree mutates `<cache>/.git/refs/heads/...`. The chmod a-w on the working tree doesn't help.
2. **You can't have two agents on the same branch.** Worktrees enforce single-checkout-per-branch. `--reference` clones don't.
3. **Sync becomes risky.** `git fetch` in the cache moves refs that worktrees also see, potentially mid-edit. `--reference` clones don't share refs.
4. **Workspace lifecycle is harder.** Worktree admin metadata in `<cache>/.git/worktrees/` must stay in sync with the worktree dir; a clone has no parent state.

The one cost of `--reference`: object storage is shared, and a `git gc` on the cache *can* prune objects the workspace references via alternates. Repocache mitigates this by setting `gc.auto = 0` on every cache repo and holding a per-repo `flock` so sync and workspace creation can't race.

---

## Commands

```
repocache init [--agents=auto|all|none|<list>] [--no-bg-sync] [--print-agent-doc]
repocache uninstall [--agents=...]
repocache repo {add,rm,list [--json]}
repocache sync [<name>...] [--if-older-than <dur>] [--jobs N] [--json]
repocache workspace {new,list [--json],path,rm [--force]}
repocache help [<topic>]
repocache --version
```

See [SPEC.md §5](./SPEC.md#5-commands) for the exact behavior of each.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Not found (repo or workspace doesn't exist where expected) |
| 3 | Already exists (duplicate config entry, workspace duplicate) |
| 4 | Dirty (workspace has uncommitted or unpushed changes; refused without `--force`) |
| 5 | Locked (couldn't acquire lock within timeout) |
| 6 | Network (git fetch or clone failed) |
| 7 | Config (config invalid, unreadable, or unwritable) |

---

## Authentication

Repocache does not manage credentials. Every git operation defers to whatever `git clone <url>` already works with in your shell:

- HTTPS with a credential helper (`gh auth setup-git`, `git-credential-manager`, OS keychain helpers)
- SSH with your `ssh-agent` for `git@github.com:...` URLs

If `git clone <url>` works at your shell, repocache works.

---

## Limitations & gotchas

- **Removing a repo doesn't free disk.** `repocache repo rm <name>` removes the config entry but leaves the cache on disk. To free disk: `chmod -R u+w ~/.local/share/repocache/repos/<name> && rm -rf ~/.local/share/repocache/repos/<name>`. The chmod step is needed because the cache tree is read-only — even the owner can't delete entries from a non-writable directory without first restoring write.
- **Large repos.** Repocache does full clones. There is no sparse-checkout or partial-clone support yet. If you need to track chromium or llvm-sized repos, you'll feel it.
- **OpenCode project-shadow bug.** When a project-level `AGENTS.md` exists, OpenCode silently ignores the global one — including the `@REPOCACHE.md` line repocache added. Upstream issue; nothing repocache can do from its side. `init` prints a warning when integrating with OpenCode.
- **No MCP server in v1.** The CLI is the only surface. MCP wrappers can be added later if there's demand.

---

## Status

Early alpha. What's implemented:

- ✅ `init` (dirs + config; agent integration coming in next step)
- ✅ `repo add/rm/list`
- ✅ `sync` (with locking, chmod enforcement, parallel fetches, --if-older-than)
- ✅ `workspace new/list/path/rm` (with --reference, dirty/unpushed checks)
- ⏳ Agent integration (Claude / Codex / Gemini / OpenCode) — next
- ⏳ Claude `SessionStart` bg-sync hook — after agents
- ⏳ `help <topic>` long-form docs — last

The shape of the tool is settled; see [SPEC.md](./SPEC.md) for the authoritative behavioral contract.

---

## Documentation

- [SPEC.md](./SPEC.md) — authoritative behavioral spec (commands, locking, deadlock-freedom guarantees, agent integration details)
- `repocache --help` and `repocache <cmd> --help` — flag reference

---

## License

TBD.
