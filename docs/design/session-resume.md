# Design: `shed resume` — resume an agent session from its workspace

Status: proposal (research branch `claude/session-resume-research`)
Author: research spike

## Goal

Make shed the hub for in-progress agent work. When an agent creates a
workspace mid-session, shed should record *which session* did it, so that
later you can run:

```
shed resume <repo> <branch>
```

and be dropped back into that exact agent session, in the right directory,
ready to continue — across Claude Code, opencode, and Cursor, from one place.

The powerful end state: `shed resume` with no args lists every piece of
in-progress work across every agent (the workspace, its repo/branch, the
agent, how stale it is, dirty/unpushed state), and a single command resumes
any of them.

## Background: how each agent resumes

Researched against the agents' own docs. The two facts that matter for every
agent: **(1) how you resume from the CLI, and (2) whether resume is scoped to
the directory the session was created in.**

| | Claude Code | opencode | Cursor CLI |
|---|---|---|---|
| Resume by id | `claude --resume <id>` (`-r`) | `opencode --session <id>` (`-s`); `opencode run --session <id>` headless | `cursor-agent --resume <chatId>` |
| Continue last | `--continue` / `-c` | `--continue` / `-c` | `--continue` (= `--resume=-1`) |
| ID format | UUID | `ses_<base62>` | UUID |
| **cwd-scoped?** | **Yes** — transcript at `~/.claude/projects/<encoded-cwd>/<id>.jsonl`; wrong dir → `No conversation found` | **Yes** — project/cwd-keyed; worktrees are a known sharp edge | **No** — global store, resumes from anywhere |
| Caller-set id at creation? | Yes (`--session-id <uuid>`) | No | No (`create-chat` returns one) |
| **Min metadata to resume** | **id + cwd** | **id + cwd** | **id** (cwd advisory) |

Conclusion: **session id alone is not sufficient.** For Claude and opencode
the resume is cwd-scoped, so shed must capture *both the session id and the
directory the session was launched in*, and resume with `cd <cwd> && <agent>
<resume-flag> <id>`. For Cursor the id is enough, but shed stores the cwd
anyway so it resumes in the right worktree.

### opencode worktree caveat

opencode has several open bugs around `--continue` / `session list` picking the
wrong session across git worktrees. Since shed *is* a worktree manager, the
design must always resume by **explicit `--session <id>` from the exact dir**,
never `--continue`.

## The hard problem: correlating a workspace to a session

The naive idea — have the agent pass its own session id as a flag to
`shed workspace new --claude-code-session-id <id>` — does not work reliably,
because **no agent exposes its own session id to the shell/bash environment**.
There is no `CLAUDE_SESSION_ID` env var, no opencode/Cursor equivalent the bash
tool can read. Asking the model to pass the flag is best-effort guesswork and
will silently fail most of the time.

We *can* capture the session id at `SessionStart` (shed already runs a hook
there). But that alone doesn't solve correlation: when a later
`shed workspace new` runs, how do we know it belongs to that same session?

### Mechanisms considered

1. **Agent passes a flag** — rejected as primary; the agent can't reliably
   know its own id (see above). Kept only as an explicit override.

2. **Process-tree ancestry** — `shed workspace new` is a descendant of the
   long-lived agent process the `SessionStart` hook also ran under, so the
   common ancestor PID (guarded by start-time against PID reuse) ties them
   together. Works for Claude and Cursor (one process per session) but
   **breaks for opencode**, which runs *one shared server process across many
   concurrent sessions* — same PID, different sessions. Rejected as primary.

3. **Pre-execution hook (chosen).** Every agent has a hook that fires *before*
   a tool/shell command runs and hands the hook **the session id, the command
   string, and the cwd together in one event.** shed installs itself as that
   hook; when it sees its own `shed workspace new …` command, it records the
   workspace↔session link itself. Zero agent cooperation, uniform across all
   three, and it sidesteps opencode's shared-PID problem because the hook
   carries the real per-session id.

### The pre-execution hooks (verified against docs)

| Agent | Hook | Fields delivered together |
|---|---|---|
| Claude | `PreToolUse` (matcher `Bash`) | `session_id`, `cwd`, `tool_input.command` |
| Cursor | `beforeShellExecution` | `command`, `conversation_id`, `cwd` / `workspace_roots` |
| opencode | plugin `tool.execute.before` | `input.sessionID`, `output.args.command` (in-process plugin) |

- Claude: `session_id` is documented as exactly the id `claude --resume`
  consumes.
- Cursor / opencode: the hook-visible id (`conversation_id` / `input.sessionID`)
  is confirmed-by-chain to equal the resume id (same id flows through their
  JSON output and SDK surfaces) but is **not stated byte-for-byte in the docs**
  — needs one empirical round-trip check per agent before we depend on it.
- opencode specifics: the command is on the **second** callback arg
  (`output.args.command`), the id on the **first** (`input`); the plugin must
  read both. opencode's hook is an in-process JS/TS plugin, not a stdin-JSON
  executable like Cursor's.

## Design

### 1. Pre-execution hook (new), installed by `shed init`

Alongside the existing session-context + bg-sync hooks, shed installs a
pre-exec hook per agent:

- Claude: a `PreToolUse` entry with matcher `Bash` running `shed __on-tool-call`
  (reads the hook JSON on stdin).
- Cursor: a `beforeShellExecution` entry running the same subcommand.
- opencode: extend shed's existing plugin with a `tool.execute.before` handler.

The hook is cheap and a no-op for every command that isn't a
`shed workspace new` / `shed ws new`.

### 2. Linking, owned by the hook

When the hook sees a `shed workspace new` command, it has `(session_id, cwd,
command)`. To turn the command into a concrete workspace identity:

- **Primary — parse it.** shed owns its own CLI grammar, so it re-resolves
  `<repo> <branch>` with the same shorthand resolution `workspace new` uses and
  computes the deterministic workspace path. The workspace doesn't exist yet at
  hook time, but the path is deterministic, so the link is keyed to the
  future path and is already in place when `workspace new` creates the dir.
- **Fallback — handshake.** If a command is shell-wrapped in a way that defeats
  tolerant parsing (`cd x && FOO=1 shed ws new …`), `workspace new` itself, on
  success, claims the most recent session the hook recorded for this cwd.

### 3. The link record

Stored as a workspace meta sidecar, mirroring the existing `.git/shed.meta`
pattern on stored repos:

```json
{
  "agent": "claude",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "cwd": "/home/user/projects/foo",
  "linked_at": "2026-06-27T19:40:00Z"
}
```

Last writer wins (a workspace re-touched by a newer session points at the
newer one). `shed prune` drops the link when it prunes the workspace.

### 4. `shed resume`

- **`shed resume`** (no args) → the hub: a table joining workspaces to their
  linked sessions — repo, branch, agent, session age, dirty/unpushed — across
  all agents. Workspaces with no link are shown but marked not-resumable.
- **`shed resume <repo> <branch>`** → read the link, then exec:

  ```
  cd <cwd> && <agent-bin> <resume-flag> <session_id> <args-after-->
  ```

  Resume flags: `claude --resume` · `opencode --session` · `cursor-agent --resume`.

#### Argument contract

```
shed resume <repo> <branch> [shed flags] [-- <args passed straight to the agent>]
```

Everything after `--` is appended to the agent invocation **verbatim** — shed
never interprets it, so each agent's own prompt/print/flag conventions just
work and stay robust as those agents add flags. cobra stops flag parsing at
`--` automatically, so shed's own flags must precede it.

- Interactive: `shed resume foo bar` → `cd <cwd> && claude --resume <id>`
- Non-interactive: `shed resume foo bar -- -p "continue the refactor"` →
  `cd <cwd> && claude --resume <id> -p "continue the refactor"`
- Dry run: `shed resume foo bar --print` emits the command instead of exec'ing
  (note `--print` is before `--`).

This makes automation (cron, CI, a parent agent fanning out resumes) use the
exact same path as interactive use.

### 5. Flags as override only

`shed workspace new` keeps optional `--claude-code-session-id` /
`--opencode-session-id` / `--cursor-chat-id` flags purely as an **explicit
override** for headless / no-hook contexts (e.g. `claude -p` runs, CI), where
the pre-exec hook may not fire. They are not the primary path.

## Open questions / must-verify

1. **Empirically confirm** the hook-visible id == the resume id for Cursor
   (`conversation_id`) and opencode (`input.sessionID`). Claude is documented.
2. **opencode `output.args.command` field name** on the installed version
   (documented for the built-in bash tool; confirm it hasn't drifted).
3. **Claude cwd semantics**: the `PreToolUse` `cwd` should be the session's
   project dir (= the resume dir). Confirm it doesn't drift if the model `cd`s
   mid-session.
4. **Multiple workspaces per session / multiple sessions per workspace**: v1 is
   last-writer-wins. Decide if a small link history is worth it.
5. **Lifecycle**: prune stale links (transcript gone, session too old) — fold
   into `shed prune`.

## Implementation sequence

1. Pre-exec hook subcommand (`shed __on-tool-call`) + linking logic + workspace
   meta sidecar.
2. `shed workspace new` reconciliation/handshake + override flags.
3. `shed resume` command (hub table + exec/`--print` + `--` passthrough).
4. `shed init` wiring: install the pre-exec hook for each agent; extend the
   opencode plugin.
5. Update the embedded guide so agents know `shed resume` exists.
