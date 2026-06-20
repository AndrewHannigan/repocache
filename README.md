# repocache

A CLI that gives terminal coding agents a **read-only local mirror** of your GitHub repositories, plus a way to **create writable workspaces** from that mirror when the agent wants to edit code.

> **Status:** early scaffold. Design is settled; commands are not implemented yet. See [Status](#status).
>
> For the authoritative behavioral spec, see [SPEC.md](./SPEC.md).

---

## Why this exists

Coding agents (Claude Code, Codex CLI, OpenCode, Cursor, etc.) frequently need to:

1. **Read code from repos that aren't the current working directory** — to understand how a library is used elsewhere, to copy a pattern from another project, to look up an API.
2. **Edit those repos and propose changes** — branch, commit, push, open a PR.

Today, agents typically solve (1) by cloning into `/tmp` ad-hoc (wasteful, no caching, no consistency) or by relying on web search and training-data recall (lossy, often wrong). They solve (2) by cloning again, into a different ad-hoc location.

`repocache` is a single tool that gives the agent:

- A **persistent, read-only library** of the repos *you've told it to care about*, kept fresh with one command (`repocache sync`).
- An **editable workspace** for any (repo, branch) pair, created in O(seconds) via `git clone --reference` so it doesn't duplicate object storage on disk.

Agents discover the tool via `repocache init`, which registers the cache and workspace directories with each detected agent and injects a short `REPOCACHE.md` doc into the agent's always-loaded instructions — see [Agent onboarding](#agent-onboarding).

## Quick start

```bash
# Install (build from source for now; repo is private)
git clone https://github.com/AndrewHannigan/repocache
cd repocache && go build -o /usr/local/bin/repocache ./cmd/repocache

# One-time setup: creates dirs + config, and integrates with detected agents
# (Claude Code, Codex CLI, Gemini CLI, OpenCode). Prompts before touching
# their config files. `--no-bg-sync` to skip the Claude SessionStart hook.
repocache init

# Add a repo to your library
repocache repo add https://github.com/anthropics/claude-code

# Pull everything down
repocache sync

# The cache is now searchable directly with standard tools
rg "slash command" ~/.local/share/repocache/repos/github.com/anthropics/claude-code/

# Spin up a workspace to make edits
cd "$(repocache workspace new anthropics/claude-code fix-bug)"
# ... edit, commit, push, open PR with `gh` ...
repocache workspace rm anthropics/claude-code fix-bug
```

## How it works

`repocache` maintains two parallel directory trees on disk:

```
~/.local/share/repocache/
├── repos/                              # the read-only mirror (the "cache")
│   └── <host>/<owner>/<repo>/          # full clone, working tree chmod a-w
└── workspaces/                         # writable workspaces
    └── <host>/<owner>/<repo>/<branch>/ # git clone --reference ../../repos/...
```

- **The cache (`repos/`)** is a full clone of every tracked repo. After every sync the working tree is `chmod -R a-w` so the agent (or any tool) gets a filesystem error if it tries to edit cached files. The `.git` directory stays writable so `repocache sync` can `git fetch` cleanly.
- **Workspaces (`workspaces/`)** are independent clones created with `--reference` against the cache. They share object storage with the cache (cheap on disk) but have their own refs, branches, working tree, and remotes. Edits, commits, pushes all happen here.

Config lives at `~/.config/repocache/config.toml`:

```toml
[[repo]]
url = "https://github.com/anthropics/claude-code"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "bar"   # optional override; default is derived from URL
```

## Why `git clone --reference` instead of `git worktree`

Both mechanisms let you have multiple working trees on disk while sharing the underlying git object store. They are *not* equivalent. `repocache` uses `--reference` deliberately:

| Concern | `git worktree` | `git clone --reference` |
|---------|----------------|-------------------------|
| Refs / branches | **Shared** — all worktrees see the same branch namespace | **Independent** — each clone has its own refs |
| Same branch in two places | Forbidden — git refuses | Fine — workspaces are unaware of each other |
| Cache's `.git` writability | Must stay writable (refs and worktree admin files live there) | Cache `.git` can stay writable for sync, but workspaces don't touch it |
| Read-only enforcement on cache tree | Compatible — but worktree refs still mutate shared `.git/refs` from another tree | Cleaner — cache and workspace are fully decoupled |
| Workspace deletion | `git worktree remove` (or manual cleanup of admin files); fails if the worktree is corrupted | `rm -rf` |
| Disk savings | Yes (no object duplication) | Yes (via `objects/info/alternates`) |
| Push / fetch from workspace | Affects shared refs | Affects workspace refs only |

Concretely, the worktree model breaks `repocache`'s contract in several ways:

1. **The read-only invariant on the cache leaks.** A worktree of the cache *is* the cache, in a real sense — committing in the worktree updates `.git/refs/heads/...` inside the cache. The cache's working tree being `chmod a-w` doesn't matter if refs underneath it are mutating.
2. **You can't have two agents working on the same branch.** Worktrees enforce single-checkout-per-branch. Two parallel workspaces on `main` of the same repo is impossible. With `--reference` clones it's trivial — each workspace has its own `main` ref.
3. **Sync becomes risky.** `git fetch` in the cache moves refs that worktrees also see, potentially mid-edit in another worktree. `--reference` clones don't share refs, so sync only affects the cache.
4. **Workspace lifecycle is harder.** Worktree admin metadata (`.git/worktrees/<name>/`) inside the cache must stay in sync with the worktree dir. A workspace that's just a clone has no parent state — `rm -rf` is enough.

The one cost of `--reference`: object storage is shared, and a `git gc` on the cache *can* prune objects the workspace references via the alternates file. `repocache` mitigates this by setting `gc.auto = 0` on every cache repo, and by holding a per-repo `flock` so sync and workspace creation can't race.

## Commands

> Nothing is implemented yet. This is the target surface. See [SPEC.md](./SPEC.md) for exact behavior of each.

| Command | Purpose |
|---------|---------|
| `repocache init [--agents=auto\|all\|none\|<list>] [--no-bg-sync] [--print-agent-doc]` | Bootstrap dirs + config; integrate with detected agents |
| `repocache uninstall [--agents=...]` | Reverse agent integration; leaves data and config in place |
| `repocache repo add <url> [--name <n>]` | Add a repo to the library |
| `repocache repo rm <name>` | Remove a repo from the library (does not delete cache) |
| `repocache repo list [--json]` | List tracked repos, last sync, disk usage |
| `repocache sync [<name>...] [--if-older-than <dur>] [--jobs N]` | Fetch + update working tree + reapply chmod |
| `repocache workspace new <repo> <branch> [--base <branch>]` | Create a writable workspace via `git clone --reference` |
| `repocache workspace list [--json]` | All workspaces, dirty/unpushed state |
| `repocache workspace path <repo> <branch>` | Print absolute path (for `cd $(...)`) |
| `repocache workspace rm <repo> <branch> [--force]` | Delete workspace; refuses if dirty unless `--force` |
| `repocache help [<topic>]` | Long-form docs on a specific command/topic |
| `repocache --version` | Print version |

Branch listing, content search, PR checkout — `repocache` deliberately does not wrap these. Agents use `git`, `rg`, and `gh` directly against the cache and workspace paths.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Not found (repo / workspace doesn't exist) |
| 3 | Already exists (e.g. workspace duplicate) |
| 4 | Dirty (workspace has uncommitted changes; refused without `--force`) |
| 5 | Locked (another `repocache` process holds the lock) |
| 6 | Network (git fetch failed) |
| 7 | Config (config file invalid or missing) |

### `--json` everywhere

Every list/show command supports `--json` and emits a stable schema for agent consumption. Error envelopes in JSON mode: `{"error": "...", "code": "..."}` to stderr, with the corresponding exit code.

## Agent onboarding

`repocache init` auto-detects installed agents and configures each one. For every agent, three things happen:

1. **Doc injection.** Writes a short `REPOCACHE.md` (~15 lines) and idempotently appends `@REPOCACHE.md` to the agent's always-loaded instructions file. The agent sees this content every session — no lazy skill trigger, no manual paste.
2. **Directory registration.** Adds `~/.local/share/repocache/repos/` and `~/.local/share/repocache/workspaces/` to the agent's allowed filesystem paths. Without this, the agent can't read or edit anywhere under the cache.
3. **Background sync** (Claude Code only in v1). Installs a `SessionStart` hook that runs `repocache __bg-sync` — a backgrounded `repocache sync --if-older-than 1h`. Stale-but-fast first byte; the first session ever just prints a hint to run `sync` manually.

| Agent | Doc file | Settings file | Bg-sync? |
|-------|----------|---------------|----------|
| Claude Code | `~/.claude/CLAUDE.md` | `~/.claude/settings.json` (`permissions.additionalDirectories`) | Yes |
| Codex CLI | `~/.codex/AGENTS.md` | `~/.codex/config.toml` (`sandbox_workspace_write.writable_roots`) | No |
| Gemini CLI | `~/.gemini/GEMINI.md` | `~/.gemini/settings.json` | No |
| OpenCode | `~/.config/opencode/AGENTS.md` | `~/.config/opencode/opencode.json` (`external_directory`) | No |

All edits are idempotent and tagged with a `repocache:managed` marker so `repocache uninstall` reverses them cleanly without touching unrelated user settings.

**OpenCode caveat:** there's an open upstream bug where a project-level `AGENTS.md` silently shadows the global one. In affected projects, the `@REPOCACHE.md` line repocache added won't be loaded. `init` prints a warning when installing for OpenCode.

This pattern (doc injection via `@import`) is borrowed from [RTK](https://github.com/rtk-ai/rtk). Skills-based onboarding (Playwright's model) was rejected because it loads lazily — the agent needs to know about `repocache` *before* it decides to do the wrong thing.

## Authentication

`repocache` does not manage credentials. It defers to whatever `git clone <url>` already works with in your shell:

- HTTPS with a credential helper (`gh auth setup-git`, `git-credential-manager`, etc.)
- SSH with your `ssh-agent` for `git@github.com:...` URLs

If `git clone <url>` works at your shell, `repocache` works.

## Status

This is an early scaffold. What's currently true:

- ✅ Design settled, documented in [SPEC.md](./SPEC.md)
- ✅ Go module scaffolded (`go build ./cmd/repocache`)
- ✅ `--version` flag works
- ❌ No subcommands implemented yet

Implementation order (each step ends with a buildable, working binary):

1. Config loader + paths + subcommand tree
2. `repocache init` (dirs + config only — no agent integration yet)
3. `repocache repo {add,rm,list}`
4. `repocache sync`
5. `repocache workspace {new,list,path,rm}`
6. Claude Code agent integration (doc + dirs)
7. Codex / Gemini / OpenCode agent integration + auto-detect
8. `repocache __bg-sync` + Claude SessionStart hook
9. `repocache help` + polish

## License

TBD.
