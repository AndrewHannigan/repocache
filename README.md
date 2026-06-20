# repocache

A CLI that gives terminal coding agents a **read-only local mirror** of your GitHub repositories, plus a way to **create writable workspaces** from that mirror when the agent wants to edit code.

> **Status:** early scaffold. Design is settled; commands are not implemented yet. See [Status](#status).

---

## Why this exists

Coding agents (Claude Code, Codex CLI, OpenCode, Cursor, etc.) frequently need to:

1. **Read code from repos that aren't the current working directory** — to understand how a library is used elsewhere, to copy a pattern from another project, to look up an API.
2. **Edit those repos and propose changes** — branch, commit, push, open a PR.

Today, agents typically solve (1) by cloning into `/tmp` ad-hoc (wasteful, no caching, no consistency) or by relying on web search and training-data recall (lossy, often wrong). They solve (2) by cloning again, into a different ad-hoc location.

`repocache` is a single tool that gives the agent:

- A **persistent, read-only library** of the repos *you've told it to care about*, kept fresh with one command (`repocache sync`).
- An **editable workspace** for any (repo, branch) pair, created in O(seconds) via `git clone --reference` so it doesn't duplicate object storage on disk.

Agents discover the tool via an `@REPOCACHE.md` line injected into `~/.claude/CLAUDE.md` (and equivalents for other agents) — see [Agent onboarding](#agent-onboarding).

## Quick start

```bash
# Install (build from source for now; repo is private)
git clone https://github.com/AndrewHannigan/repocache
cd repocache && go build -o /usr/local/bin/repocache ./cmd/repocache

# Initialize directories and an empty config
repocache init

# Add a repo to your library
repocache repo add https://github.com/anthropics/claude-code

# Pull everything down
repocache sync

# The cache is now searchable
rg "slash command" ~/.local/share/repocache/repos/anthropics/claude-code/

# Spin up a workspace to make edits
ws=$(repocache workspace path anthropics/claude-code fix-bug)
repocache workspace new anthropics/claude-code fix-bug
cd "$ws"
# ... edit, commit, push, open PR with `gh` ...
repocache workspace rm anthropics/claude-code fix-bug

# Tell your agents the tool exists
repocache install --claude   # or --codex, --opencode, --all
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

> Most of these are not implemented yet. This is the target surface.

| Command | Purpose |
|---------|---------|
| `repocache init` | Create config + data dirs |
| `repocache repo add <url> [--name <n>]` | Add a repo to the config |
| `repocache repo rm <name>` | Remove a repo from config (does not delete cache) |
| `repocache repo list [--json]` | List tracked repos, last sync, disk usage |
| `repocache sync [<name>...] [--if-older-than <dur>] [--jobs N]` | Fetch + update working tree + reapply chmod |
| `repocache workspace new <repo> <branch> [--base <branch>]` | Create a writable workspace via `git clone --reference` |
| `repocache workspace list [--json]` | All workspaces, dirty state |
| `repocache workspace path <repo> <branch>` | Print absolute path (for `cd $(...)`) |
| `repocache workspace rm <repo> <branch> [--force]` | Delete workspace; refuses if dirty unless `--force` |
| `repocache install [--claude\|--codex\|--opencode\|--all] [--print]` | Inject `@REPOCACHE.md` into the agent's CLAUDE.md (or equivalent) |
| `repocache uninstall ...` | Remove the `@REPOCACHE.md` line and file |
| `repocache help <topic>` | Long-form docs on a specific command/topic |

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

The naive approach — telling the user to paste a paragraph into `~/.claude/CLAUDE.md` — is fragile. The skills approach (Playwright's model) is lazy-loaded, which is wrong for a tool the agent must reach for *before* doing the wrong thing (e.g. cloning into `/tmp` itself).

`repocache install --claude` does what [RTK](https://github.com/rtk-ai/rtk) does:

1. Writes `~/.claude/REPOCACHE.md` — a short (~15 line) doc telling the agent the tool exists, where the cache lives, and the canonical workflow.
2. Idempotently appends `@REPOCACHE.md` to `~/.claude/CLAUDE.md` (creating the file if missing).

Because `CLAUDE.md` is loaded every session, the agent sees the `REPOCACHE.md` content automatically — no skill trigger, no user prompt, no manual paste. `repocache uninstall --claude` cleanly reverses this.

Equivalent integrations are planned for Codex CLI, OpenCode, and Cursor.

## Authentication

`repocache` does not manage credentials. It defers to whatever `git clone <url>` already works with in your shell:

- HTTPS with a credential helper (`gh auth setup-git`, `git-credential-manager`, etc.)
- SSH with your `ssh-agent` for `git@github.com:...` URLs

If `git clone <url>` works at your shell, `repocache` works.

## Status

This is an early scaffold. What's currently true:

- ✅ Design settled, documented in this README
- ✅ Go module scaffolded (`go build ./cmd/repocache`)
- ✅ `--version` flag works
- ❌ No subcommands implemented yet

Next: implement `init`, `repo add`, `repo list`, then `sync`, then `workspace new`.

## License

TBD.
