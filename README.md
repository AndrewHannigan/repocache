# shed

![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go) ![Status](https://img.shields.io/badge/status-beta-yellow) ![License](https://img.shields.io/badge/license-MIT-green)

**Stop your coding agents from stepping on each other.**

Run Claude Code, Cursor, and opencode against the same repos and they clobber each other's checkouts, pile up stale workspaces, and quietly branch off out-of-date code. **shed** is one standard system all your agents share to manage git repos and workspaces — read-only reference repos, isolated writable workspaces, every one built from the latest code.

<!-- TODO(visual hook): drop a demo GIF/asciinema here — two agents working the same
     repo through shed: each gets its own fresh `shed workspace new`, neither touches the
     read-only store, then `shed prune` cleans up. This is the "10-second, read-no-text"
     hook; keep it above the fold. -->
<!-- ![shed in action](docs/demo.gif) -->

- 🤝 **One system, every agent** — All agents manage repos and workspaces the same way, so parallel sessions never step on each other in the same repo.
- ✍️ **Isolated writable workspaces** — `shed workspace new` gives each session its own clone off the pristine repo; agents edit there, never in your reference copy or each other's.
- 🌱 **Never a stale branch** — every workspace is created from the freshly-synced repo, so an agent never unintentionally works on out-of-date code.
- 🧹 **One-command cleanup** — workspaces pile up fast; `shed prune` reclaims the ones whose work has already landed (merged PR or merged into the default branch) and leaves anything unpushed untouched.
- 🔁 **Pick up where you left off** — `shed resume <workspace>` reopens the exact agent session that created a workspace — same agent, same session id, same directory — so a half-finished task is one command away.
- 🔒 **Reference repos, stored once and never clobbered** — every repo lives read-only in `~/.shed` (`chmod a-w`), refreshed in the background and reused across sessions instead of re-cloned into `/tmp`.
- 🧰 **Searchable out of the box** — agents run `rg`, `grep`, `git`, and `gh` across the entire catalog directly.
- ⚙️ **Zero agent setup** — one `shed init` wires up each agent to use shed automatically — no path hallucinations.

---

## Install

```bash
# macOS (Homebrew)
brew install AndrewHannigan/tap/shed

# Linux / other Unix
curl -fsSL https://raw.githubusercontent.com/AndrewHannigan/shed/main/install.sh | sh
```

---

## Quickstart

```bash
# integrate with your agents
shed init

# add a repo (github shorthand works)
shed add octocat/Hello-World

# now run claude, cursor-agent, or opencode — your agent knows how to use it
```

That's it. Now any of your agents have a consistent system for working with your repo catalog — reading the read-only copy, and carving off an isolated, up-to-date workspace the moment they need to make changes:

```text
You:   "Fix the broken link in octocat/Hello-World's README"
Agent: reads ~/.shed/repos/github.com/octocat/Hello-World   (read-only, always fresh)
       → shed workspace new                                 (isolated, off the latest)
       → edits there, opens a PR                            (store + other agents untouched)
```

Once branches land, reclaim the workspaces they left behind:

```bash
shed prune          # remove workspaces whose work is already merged
```

Need to get back into one? `shed resume <workspace>` relaunches the agent session that
created it — in its original working directory — so you can continue the task instead of
re-explaining it.

> **Who runs what.** `shed add` / `shed rm` curate the library — run them yourself,
> or let your agent run them when it needs a repo. The `shed workspace` commands are
> best left to the agent: it creates a workspace the moment it needs to make a change
> and tears it down when done. You generally don't pre-create workspaces — a stale,
> hand-made one just risks the agent branching off the wrong base, which is exactly
> what shed exists to avoid. Set up the library; let the agent manage its own scratch space.

---

## Commands

| Command | What it does |
|---------|--------------|
| `shed init` | Bootstrap dirs + integrate with detected agents (`--uninstall` reverses it) |
| `shed add <repo\|owner>` | Add a repo — or a whole user/org — to the library |
| `shed rm <name>…` | Remove tracked repos or owners (and their stores/workspaces) |
| `shed ls` | List owners, repos, and workspaces — everything shed manages |
| `shed sync [<name>…]` | Fetch tracked repos and re-apply the read-only chmod (usually automatic) |
| `shed status` | Report sync health; show a repo's error and the likely fix |
| `shed workspace new <repo> <branch>` | Create a writable clone off the freshly-synced store; prints its path |
| `shed workspace ls` | List workspaces with dirty/unpushed state and age |
| `shed workspace path <name>` | Print a workspace's absolute path (names are globally unique) |
| `shed workspace rm <name>` | Delete a workspace (refuses dirty/unpushed work without `--force`) |
| `shed prune` | Delete workspaces whose work has already landed |
| `shed resume <name>` | Reopen the agent session that created a workspace |
| `shed history` | Show recent shed commands |
| `shed help [topic]` | Long-form docs on a command or concept |

Curate the library yourself (`add`/`rm`/`ls`); leave the `workspace` commands to the agent.

---

## Supported agents

`shed init` auto-detects each agent by config-dir presence and (in TTY mode) prompts before writing anything:

| Agent | Config dir | Allowed-dir setting | SessionStart hooks |
|-------|-----------|---------------------|--------------------|
| Claude Code | `~/.claude/` | `settings.json` → `permissions.additionalDirectories` | session-context + bg-sync |
| Cursor CLI | `~/.cursor/` | n/a² | session-context + bg-sync (`hooks.json`)² |
| opencode | `~/.config/opencode/` | n/a¹ | plugin (see below)¹ |

¹ opencode has no SessionStart shell hook and no path allowlist. Instead, `init` drops a plugin at `~/.config/opencode/plugin/shed.js`, auto-loaded at startup; it runs `shed __bg-sync` and injects the guide into the model's system prompt via opencode's `experimental.chat.system.transform` hook. `shed init --uninstall` deletes the file.

² Cursor's hooks live in `~/.cursor/hooks.json` under `hooks.sessionStart` (a flatter, camelCase shape than Claude's). shed adds two `sessionStart` entries — `shed __session-context --agent cursor` and `shed __bg-sync`. The session-context one prints a `{"additional_context":"…"}` JSON object that Cursor injects into the conversation. Cursor has no per-directory allowlist (like opencode, the `chmod a-w` on `repos/` enforces read-only), so no paths are registered. If a hand-rolled `~/.cursor/plugins/local/shed` plugin is present, `init` removes it so the guide isn't injected twice.

All edits are idempotent and recorded in a sidecar state file, so `shed init --uninstall` removes only what shed added.

**Why a SessionStart hook and not a static doc or a skill?** Skills load lazily — the agent only sees them once something triggers, but the whole point is that the agent reaches for shed *before* doing the wrong thing (cloning into `/tmp`, hallucinating a path). The `shed __session-context` hook injects the guide into context at the start of every session. Because that text is generated by the binary rather than written to a file, it's always current — there's nothing to drift after an upgrade.

---

## Layout on disk

```
~/.config/shed/
└── config.toml                              # your tracked repos

~/.shed/
├── repos/<host>/<owner>/<repo>/             # store (chmod a-w)
└── workspaces/<host>/<owner>/<repo>/<br>/   # editable (git clone --reference)
```

Config example:

```toml
[[repo]]
url = "https://github.com/octocat/Hello-World"

[[repo]]
url = "git@github.com:foo/bar.git"
name = "myorg/bar"   # optional override; default derived from URL

# Per-repo git config. Reconciled into the cache on every sync and seeded into
# new workspaces at clone time — forwarded verbatim, so any git option works.
[[repo]]
url = "https://github.com/myorg/widgets"
git = { "user.email" = "me@work.com", "core.hooksPath" = ".githooks" }

# Track a whole user/org. sync discovers its repos via gh and adds new ones
# automatically (as [[repo]] entries tagged with source = this owner).
[[owner]]
url = "https://github.com/octocat"
# include_forks = false      # default
# include_archived = false   # default
# visibility = "all"         # all|public|private
```

---

## Why a read-only store + writable workspaces

The natural first instinct is "just keep a normal clone of each repo and let agents work in it." That breaks down the moment you have more than one thing going on:

- **One clone has one working tree and one `HEAD`.** Two agents — or one agent on two tasks — can't both use it. One has to stash, switch branches, and pray; the other clobbers it. Splitting a *read-only reference* from *N disposable workspaces* gives every task its own tree and refs, so they run in parallel without colliding.
- **The reference stays trustworthy.** Because the store is `chmod a-w`, it's never half-edited, never parked on some branch an agent forgot to leave, never carrying stray uncommitted changes. So searching and reading across the catalog always reflects real upstream code, and every new workspace forks from a known-good, current copy — never a stale branch by accident.
- **Mistakes are cheap.** An agent literally can't corrupt the source of truth. Workspaces are throwaway: if one goes sideways, delete it (or `shed prune`) and the pristine copy is untouched.

So read-only isn't the goal in itself — it's what makes the *writable* workspaces safe to hand out freely. You get a stable baseline to read from **and** isolated, always-fresh scratch space to write in, instead of having to trade one for the other.

## Why `git clone --reference`, not `git worktree`

A worktree modifies state in the originating repo's `./git/`, breaking the read-only invariant that keeps agents from modifying the repo store directly. `--reference` clones keep independent refs and clean up with plain `rm -rf` — while still borrowing objects for the disk savings. Shed sets `gc.auto = 0` and holds a per-repo `flock` so sync and workspace creation can't race.

---

## Authentication

Shed does not manage credentials. Every git operation defers to whatever `git clone <url>` already works with in your shell — HTTPS credential helpers or `ssh-agent`. If `git clone <url>` works, shed works.

**Picking a transport.** GitHub shorthand (`shed add owner/repo`) expands to HTTPS. If that can't authenticate but the SSH form can — the common "I only have an `ssh-agent` set up" case — `shed add` detects this during a preflight check and stores the working SSH URL instead, telling you it did. To force a transport, pass a full URL (`git@github.com:owner/repo.git` or `https://github.com/owner/repo`).

**When auth fails.** Sync failures — including a repo's very first clone — are recorded and surfaced, never silently dropped: `shed status` reports them and the session-start hook warns your agent that the store is stale. The suggested fix is transport-aware (load your SSH key vs. `gh auth login` / a credential helper).

---

## Documentation

- `shed help` — curated overview of every command
- `shed help <topic>` — long-form prose docs on a command or concept (topics: `agents`, `auth`, `concepts`, `history`, `init`, `library`, `locking`, `owner`, `prune`, `sync`, `workspace`)
- `shed --help` and `shed <cmd> --help` — flag reference

## License

[MIT](./LICENSE) — © 2026 Andrew Hannigan.
