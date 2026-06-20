# repocache

> Give your terminal coding agent a fast, **read-only library** of git repos to search across — and a one-command shortcut to spin up **writable workspaces** from any branch when it wants to edit.

Built for [Claude Code](https://www.anthropic.com/claude-code), [Codex CLI](https://developers.openai.com/codex/), [Gemini CLI](https://github.com/google-gemini/gemini-cli), and [OpenCode](https://opencode.ai).

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go) ![Status](https://img.shields.io/badge/status-beta-yellow)

---

## Install

Requires Go 1.22+ and `gh` (the repo is currently private):

```bash
gh repo clone AndrewHannigan/repocache /tmp/repocache \
  && (cd /tmp/repocache && go build -o /usr/local/bin/repocache ./cmd/repocache)
```

Or build by hand:

```bash
git clone https://github.com/AndrewHannigan/repocache
cd repocache && go build -o /usr/local/bin/repocache ./cmd/repocache
```

---

## Try it (90 seconds)

```bash
# 1. Bootstrap: creates dirs + integrates with every detected agent
#    (writes a 12-line REPOCACHE.md, registers the cache+workspace
#     dirs as allowed paths, sets up the @import in CLAUDE.md /
#     AGENTS.md / GEMINI.md as appropriate).
repocache init

# 2. Add a repo to your library
repocache repo add https://github.com/anthropics/anthropic-sdk-python

# 3. Pull it down — full clone, working tree marked chmod a-w
repocache sync

# 4. Your agent now sees it. Try this in your terminal agent:
#    "search the anthropic-sdk-python repo for prompt caching examples"

# 5. When the agent wants to edit, it'll run:
cd "$(repocache workspace new anthropics/anthropic-sdk-python fix-typo)"
#    ... edit, commit, push, open PR with gh ...

# 6. Clean up when done
repocache workspace rm anthropics/anthropic-sdk-python fix-typo
```

That's the whole loop. Repocache only adds two things to your agent's world:

- A directory at `~/.local/share/repocache/repos/<host>/<owner>/<repo>/` that's **physically read-only** (chmod a-w), so the agent can `rg`/`grep` to its heart's content with zero risk of accidental writes.
- A `workspace new` command that returns the path to an editable clone derived from the cache via `git clone --reference`.

Everything else — searching, branch listing, PR creation — uses tools the agent already knows (`rg`, `git`, `gh`). Repocache doesn't wrap them.

---

## Why use it

If you've ever watched an agent:

- **Clone a repo into `/tmp` just to grep it once** — repocache caches it persistently and shares it across sessions.
- **Accidentally edit a file it shouldn't have** — the cache is OS-enforced read-only.
- **Forget which repo a file came from** — the directory layout (`<host>/<owner>/<repo>/`) is self-describing.
- **Burn 10 minutes re-cloning a 2 GB monorepo** when starting a workspace — `git clone --reference` borrows the object store, so a fresh workspace lands in seconds even for huge repos.
- **Need to be told over and over where things live** — `repocache init` injects a 12-line `@REPOCACHE.md` into every supported agent's always-loaded instructions. The agent learns the workflow once and never forgets.

---

## Highlights

- **Read-only mirror, enforced by the OS.** Every `sync` re-applies `chmod -R a-w` on each cache tree. Agent can search but can't accidentally write.
- **O(seconds) workspaces.** `git clone --reference` borrows objects from the cache — new workspaces don't duplicate repo history on disk.
- **Auto-integrates with 4 terminal agents.** One `repocache init` registers the cache+workspace directories with each detected agent's allowed-paths setting and injects a short doc into its always-loaded instructions.
- **No wrappers around tools the agent already has.** No `repocache search`, no `repocache branches`. Search with `rg`, list branches with `git`. Repocache only does the things only repocache can do.
- **One binary, one config file.** TOML for the library, JSON sidecar per cache repo, single `flock` lockfile per cache repo. No daemons.
- **Deadlock-free by construction.** Fixed lock-acquisition order, `flock` auto-release on process exit, `.git/` always excluded from the read-only chmod. See [SPEC §7.1](./SPEC.md#71-deadlock-freedom).

---

## Supported agents

`repocache init` auto-detects each agent by config-dir presence and (in TTY mode) prompts before writing anything:

| Agent | Doc file | Allowed-dir setting | SessionStart bg-sync |
|-------|----------|---------------------|----------------------|
| Claude Code | `~/.claude/CLAUDE.md` ← `@REPOCACHE.md` | `~/.claude/settings.json` → `permissions.additionalDirectories` | Yes (coming next) |
| Codex CLI | `~/.codex/AGENTS.md` ← `@REPOCACHE.md` | `~/.codex/config.toml` → `sandbox_workspace_write.writable_roots` | No |
| Gemini CLI | `~/.gemini/GEMINI.md` ← `@REPOCACHE.md` | `~/.gemini/settings.json` → `includeDirectories` | No |
| OpenCode | `~/.config/opencode/AGENTS.md` ← `@REPOCACHE.md` | `~/.config/opencode/opencode.json` → `external_directory` | No |

All edits are idempotent and tagged with a `repocache:managed` marker so `repocache uninstall` removes only what repocache added, preserving any other entries you've set.

**Why `@REPOCACHE.md` and not a skill?** Skills load lazily — the agent only sees them when something triggers. The whole value of repocache is the agent reaches for it *before* doing the wrong thing (cloning into `/tmp`, hallucinating a path). `@REPOCACHE.md` loads every session.

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

The worktree model breaks repocache's contract in several specific ways:

1. **The read-only invariant leaks.** A worktree of the cache *is* the cache, in a real sense — committing in the worktree mutates `<cache>/.git/refs/heads/...`. The chmod a-w on the working tree doesn't help.
2. **You can't have two agents on the same branch.** Worktrees enforce single-checkout-per-branch. `--reference` clones don't.
3. **Sync becomes risky.** `git fetch` in the cache moves refs that worktrees also see, potentially mid-edit.

Repocache mitigates the one cost of `--reference` (object pruning on `git gc` in the cache could orphan workspaces) by setting `gc.auto = 0` on every cache repo and holding a per-repo `flock` so sync and workspace creation can't race.

---

## Layout on disk

```
~/.config/repocache/
└── config.toml                              # your tracked repos

~/.local/share/repocache/
├── repos/<host>/<owner>/<repo>/             # cache (chmod a-w)
└── workspaces/<host>/<owner>/<repo>/<br>/   # editable (git clone --reference)
```

Config example:

```toml
[[repo]]
url = "https://github.com/anthropics/anthropic-sdk-python"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "myorg/bar"   # optional override; default derived from URL
```

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

See [SPEC.md §5](./SPEC.md#5-commands) for exact behavior. Stable exit codes:

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Not found |
| 3 | Already exists |
| 4 | Dirty (workspace has uncommitted/unpushed changes) |
| 5 | Locked (couldn't acquire lock within timeout) |
| 6 | Network (git fetch/clone failed) |
| 7 | Config (invalid, unreadable, or unwritable) |

---

## Authentication

Repocache does not manage credentials. Every git operation defers to whatever `git clone <url>` already works with in your shell:

- HTTPS with a credential helper (`gh auth setup-git`, `git-credential-manager`, OS keychain helpers)
- SSH with your `ssh-agent` for `git@github.com:...` URLs

If `git clone <url>` works in your shell, repocache works.

---

## Limitations & gotchas

- **Removing a repo doesn't free disk by itself.** `repocache repo rm <name>` removes the config entry but leaves the cache on disk. To free the disk: `chmod -R u+w ~/.local/share/repocache/repos/<name> && rm -rf ~/.local/share/repocache/repos/<name>`. The chmod is necessary because the cache tree is read-only.
- **Large repos.** Repocache does full clones. No sparse-checkout or partial-clone yet. Chromium/llvm-sized repos will feel it.
- **OpenCode project-shadow bug.** When a project-level `AGENTS.md` exists, OpenCode silently ignores the global one — including `@REPOCACHE.md`. Upstream issue; `init` prints a warning.
- **No MCP server in v1.** The CLI is the only surface. MCP wrappers can be added later.

---

## Status

What works today:

- ✅ `init` with agent integration for all four supported agents
- ✅ `repo add` / `rm` / `list` (TOML config + cache stat)
- ✅ `sync` (parallel fetches, locking, chmod enforcement, `--if-older-than`)
- ✅ `workspace new` / `list` / `path` / `rm` (`git clone --reference`, dirty/unpushed checks)
- ✅ `uninstall` (precise reversal via sidecar state file)

What's coming next:

- ⏳ Claude Code `SessionStart` bg-sync hook
- ⏳ `repocache help <topic>` long-form docs

See [SPEC.md](./SPEC.md) for the authoritative behavioral contract.

---

## Documentation

- [SPEC.md](./SPEC.md) — full behavioral spec (commands, locking, deadlock-freedom guarantees, agent integration details)
- `repocache --help` and `repocache <cmd> --help` — flag reference
