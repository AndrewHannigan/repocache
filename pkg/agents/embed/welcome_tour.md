# shed welcome tour — instructions for the agent

You are giving the user a live, hands-on tour of `shed`. This document is the
script. The user is watching. **Run the commands for real**, one step at a time,
and narrate as you go: say what you're about to do and why, run it, then read
back what happened before moving on. Keep it conversational — you're showing a
colleague around, not reciting a manual.

A few rules for the whole tour:

- **Run real commands.** Don't fabricate output. If a step fails (e.g. `gh`
  isn't authenticated, or there's no network), say so plainly, explain what it
  would have shown, and continue — the tour still makes sense.
- **One step at a time.** Show the command, run it, summarize the result. Don't
  dump every command at once.
- **Clean up at the end** so the tour leaves no mess (see the last step), and
  tell the user exactly what you removed.
- The whole thing should take a few minutes. Keep the narration tight.

Open with a one-line framing: shed keeps a read-only mirror of git repos and
hands you isolated, writable workspaces to edit in — so an agent can search
across many repos and safely make changes in several at once. Then begin.

---

## Step 1 — Add a single repo

> "Let's start by adding a repo to the library."

Run:

```
shed add octocat/Hello-World
```

Point out as it runs:
- The GitHub **shorthand** `owner/repo` is expanded to a full URL automatically
  (a bare `owner` would be treated as a whole org — that's the next step).
- `add` **fetches the repo immediately**, so it's usable right away — no
  separate `shed sync` needed.
- The repo now lives in the **read-only cache** under
  `~/.shed/repos/github.com/octocat/Hello-World/`. We'll prove it's read-only in
  Step 3.

## Step 2 — Add a whole owner

> "You don't have to add repos one at a time — you can track an entire user or
> org, and shed discovers its repos for you."

Run:

```
shed add octocat
```

Point out:
- A single path segment (`octocat`, no `/repo`) is tracked as an **owner**, not a
  repo. On every sync, shed lists that owner's repos via `gh` and adds any new
  ones automatically — so repos created upstream later get picked up without you
  doing anything.
- This is the one place shed uses `gh` (only for discovery). If `gh` isn't
  installed/authenticated, the command still records the owner and warns that
  expansion is skipped until `gh` is available — mention this if you see the
  warning.

Then show the library so the user sees what we've got:

```
shed ls
```

Narrate the table: tracked owners, then repos, with last-sync info.

## Step 3 — Prove the cache is read-only

> "Every repo in the library is a pristine, untouchable mirror. Let me show you
> what happens if I try to edit one directly."

Find a file in the cache (e.g. `~/.shed/repos/github.com/octocat/Hello-World/README`)
and **attempt to modify it in place** — append a line, or `touch` it. It will
**fail with a permission error**. Show the user the actual error.

Explain why this is a feature:
- shed runs `chmod -R a-w` on the cache working tree, so neither you nor the
  agent can accidentally clobber the reference copy. It stays a clean baseline
  that's safe to search and read across many repos.
- So how do you make changes? You don't edit the cache — you open a **workspace**.
  That's the next step.

## Step 4 — Open the first workspace and edit

> "When you want to change something, you ask shed for a workspace: an isolated,
> writable clone."

Run:

```
shed workspace new octocat/Hello-World tour-feature-a
```

Point out:
- It **syncs the repo first**, so the workspace forks from up-to-date code.
- It prints the **absolute path** to the new workspace. `cd` there.
- Under the hood it's a `git clone --reference` against the cache, so it
  **shares object storage** with the mirror — cheap on disk — but has its own
  independent refs and working tree.

In that workspace, make a small, obvious edit (e.g. add a line to the README),
then commit it:

```
git add -A && git commit -m "Tour edit A"
```

## Step 5 — Open a second workspace and edit differently

> "Now the important part — let me open a *second* workspace on the same repo and
> make a *different* change."

Run:

```
shed workspace new octocat/Hello-World tour-feature-b
```

`cd` into this second workspace and make a **different** edit (a different line,
or a different file), then commit:

```
git add -A && git commit -m "Tour edit B"
```

## Step 6 — Show that the two don't step on each other

> "Here's the payoff: these two workspaces are completely isolated."

Demonstrate it concretely:
- In workspace **b**, run `git log --oneline -2` and `git status` — show that it
  has *Tour edit B* but **not** *Tour edit A*, and a clean tree.
- Pop back to workspace **a** and show the reverse — it has *Tour edit A*, not B.

Drive the point home:
- Two writable clones of the same repo, on different branches, with **separate
  working trees and refs** — changes in one are invisible to the other.
- This is what lets an agent juggle several in-flight changes (even across
  multiple repos) in one session without them colliding — and it's why this is
  safer than checking out branches in a single shared clone.
- They still share the underlying object store with the cache via `--reference`,
  so all this isolation costs almost no extra disk.

## Step 7 — Push both, open two PRs

> "Each workspace pushes independently, so we get two clean, separate PRs."

From each workspace, push its branch:

```
git push -u origin tour-feature-a      # from workspace a
git push -u origin tour-feature-b      # from workspace b
```

Then open a PR for each (use the user's normal flow — `gh pr create`, or the
GitHub MCP tools if that's how this session works). Note:
- The workspace's `origin` points at the **upstream repo**, not the cache, so
  `git push` and PR creation just work.
- Two workspaces → two branches → two independent PRs, with no rebasing or
  stashing between them.

> ⚠️ octocat/Hello-World is a public demo repo you very likely can't push to.
> If the push is rejected, that's expected — say so, explain that against a repo
> the user owns it would push and open the PR cleanly, and move on. Don't force it.

## Step 8 — Recap and clean up

Recap the benefits the tour demonstrated, briefly:
- **Read-only mirror** — a pristine baseline that's impossible to clobber.
- **Isolated workspaces** — edit many things at once without collisions.
- **Cheap** — workspaces share object storage with the mirror.
- **Always fresh** — `workspace new` syncs first; owners auto-discover new repos;
  the cache refreshes in the background at session start.
- **Multi-repo by design** — the same flow scales to changes across many repos in
  a single session.

Then clean up what the tour created, and tell the user what you're removing:

```
shed workspace rm octocat/Hello-World tour-feature-a --force
shed workspace rm octocat/Hello-World tour-feature-b --force
```

Ask whether they want to keep the `octocat/Hello-World` repo and the `octocat`
owner in their library or remove them with `shed rm`. Leave that choice to them —
don't remove their library entries without asking.

Close by pointing them at `shed help` and `shed <cmd> --help` for anything they
want to dig into next.
