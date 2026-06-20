# repocache

> Give your terminal coding agent a fast, **read-only library** of git repos to search across — and a one-command shortcut to spin up **writable workspaces** from any branch when it wants to edit.

Built for [Claude Code](https://www.anthropic.com/claude-code), [Codex CLI](https://developers.openai.com/codex/), [Gemini CLI](https://github.com/google-gemini/gemini-cli), and [OpenCode](https://opencode.ai).

![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go) ![Status](https://img.shields.io/badge/status-beta-yellow) ![License](https://img.shields.io/badge/license-MIT-green)

- 🔒 **OS-enforced read-only cache** — every repo is checked out `chmod a-w`, so your agent can `rg`/`grep` freely with zero risk of accidental writes.
- ⚡ **Cheap workspaces** — `git clone --reference` shares the cache's object store, so spinning up an editable clone doesn't re-download history.
- 🔄 **Repos never stale** — they refresh in the background at session start, so the agent reasons about current code, not a checkout from weeks ago.
- 🤝 **Auto-integrates with your agents** — one `repocache init` wires up Claude Code, Codex, Gemini CLI, and OpenCode by injecting a short doc into their always-loaded instructions.
- 🧰 **No wrappers** — search with `rg`, list branches with `git`, open PRs with `gh`. Repocache only does what only it can do.
- 📦 **Persistent shared library** — caches each repo once and reuses it across sessions instead of re-cloning into `/tmp`.

---

## Install

```bash
# macOS (Homebrew)
brew install AndrewHannigan/tap/repocache

# Linux / other Unix
curl -fsSL https://raw.githubusercontent.com/AndrewHannigan/repocache/main/install.sh | sh
```

---

## Quickstart

```bash
# integrate with your agents
repocache init

# add a repo, then pull it down (read-only)
repocache repo add https://github.com/anthropics/anthropic-sdk-python
repocache sync

# your agent can now search it; when it wants to edit:
cd "$(repocache workspace new anthropics/anthropic-sdk-python fix-typo)"
```

That's the whole loop. Repocache adds exactly two things to your agent's world:

- A **physically read-only** directory at `~/.local/share/repocache/repos/<host>/<owner>/<repo>/` (chmod a-w), so the agent can `rg`/`grep` freely with zero risk of accidental writes.
- A `workspace new` command that returns the path to an editable clone derived from the cache via `git clone --reference`.

Everything else — searching, branch listing, PR creation — uses tools the agent already knows (`rg`, `git`, `gh`). Repocache doesn't wrap them.

---

## Supported agents

`repocache init` auto-detects each agent by config-dir presence and (in TTY mode) prompts before writing anything:

| Agent | Doc file | Allowed-dir setting | SessionStart bg-sync |
|-------|----------|---------------------|----------------------|
| Claude Code | `~/.claude/CLAUDE.md` ← `@REPOCACHE.md` | `~/.claude/settings.json` → `permissions.additionalDirectories` | Yes |
| Codex CLI | `~/.codex/AGENTS.md` ← `@REPOCACHE.md` | `~/.codex/config.toml` → `sandbox_workspace_write.writable_roots` | Yes (requires trust)¹ |
| Gemini CLI | `~/.gemini/GEMINI.md` ← `@REPOCACHE.md` | `~/.gemini/settings.json` → `includeDirectories` | Yes |
| OpenCode | `~/.config/opencode/AGENTS.md` ← `@REPOCACHE.md` | `~/.config/opencode/opencode.json` → `external_directory` | No² |

¹ Codex requires you to trust new hooks: after `repocache init`, open Codex CLI once and run `/hooks`.
² OpenCode has no `SessionStart` hook yet ([upstream request](https://github.com/sst/opencode/issues/5409)).

All edits are idempotent and tagged with a `repocache:managed` marker, so `repocache uninstall` removes only what repocache added.

**Why `@REPOCACHE.md` and not a skill?** Skills load lazily — the agent only sees them once something triggers. The whole point is that the agent reaches for repocache *before* doing the wrong thing (cloning into `/tmp`, hallucinating a path). `@REPOCACHE.md` loads every session.

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

See [SPEC.md §5](./SPEC.md#5-commands) for exact behavior and the full table of stable exit codes.

---

## Why `git clone --reference`, not `git worktree`

Both share an underlying object store, but they are not interchangeable. A worktree of the cache *is* the cache: committing in it mutates `<cache>/.git/refs/...`, breaking the read-only invariant, and worktrees forbid checking out the same branch twice. `--reference` clones keep independent refs, allow two agents on the same branch, and clean up with plain `rm -rf` — while still borrowing objects for the disk savings. Repocache sets `gc.auto = 0` and holds a per-repo `flock` so sync and workspace creation can't race. Full comparison in [SPEC.md](./SPEC.md).

---

## Authentication

Repocache does not manage credentials. Every git operation defers to whatever `git clone <url>` already works with in your shell — HTTPS credential helpers or `ssh-agent`. If `git clone <url>` works, repocache works.

---

## Limitations & gotchas

- **Large repos.** Full clones only — no sparse-checkout or partial-clone yet. Chromium/llvm-sized repos will feel it.
- **OpenCode project-shadow bug.** When a project-level `AGENTS.md` exists, OpenCode silently ignores the global one (including `@REPOCACHE.md`). `init` prints a warning.
- **No bg-sync for OpenCode.** It lacks a `SessionStart` hook; the other three agents auto-refresh stale repos on session start.

---

## Documentation

- [SPEC.md](./SPEC.md) — full behavioral spec (commands, locking, exit codes, deadlock-freedom, agent integration)
- `repocache --help` and `repocache <cmd> --help` — flag reference

## License

[MIT](./LICENSE) — © 2026 Andrew Hannigan.
