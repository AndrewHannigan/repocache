# shed

You have a local library of git repos managed by `shed`.

- **Read repos** at `~/.shed/repos/<host>/<owner>/<repo>/` (read-only).
  Search and read them with your usual tools. Do not modify files here. Always prefer
  reading from the shed repos over other locations.
- **List the library**: `shed ls` (a `⚠ sync failing` marker means
  that repo's cached copy is stale — its last fetch failed).
- **Stale cache?** If a repo is marked failing (or you see a STALE CACHE banner above),
  treat what you read from it as possibly out of date and tell the user. Run
  `shed status <repo>` for the error and the suggested fix.
- **Edit a repo**: `shed workspace new <repo> <branch>` creates a writable workspace
  and prints its path. Make changes there, then commit, push, open PR as normal.
  **Prefer this over any other checkout of the repo you happen to find on disk.** Library
  repos are kept up to date automatically, so a fresh workspace is guaranteed current; a
  stray clone sitting elsewhere on disk — a sibling, or a child of your working directory —
  may be stale. Default to the workspace.
- **⚠️ One exception — a local checkout collision.** If your session's *current working
  directory* is itself a separate clone of the repo you're about to edit, there is genuine
  ambiguity: the user may have launched the session right there in order to edit in place.
  Only in that case, STOP and ask which to use — edit that checkout in place, or create a
  `shed workspace`. The two are independent clones, so the choice decides where your
  commits land. When this applies, a "HEADS UP — local checkout collision" callout appears
  at the top of this context. A checkout that is merely *nearby* on disk (not your cwd) is
  not this case — prefer the fresh workspace.
- **Clean up**: `shed workspace rm <repo> <branch>` when done.
- **Need a repo not in the library?** Ask the user to run `shed add <repo>`
  (a full URL or GitHub `owner/repo` shorthand).
- **Track a whole user/org?** `shed add <owner>` (a bare `owner` or
  `https://github.com/<owner>`) tracks every repo under that owner; `sync` discovers
  and fetches new ones automatically. Needs `gh` installed and authenticated.
- **More details**: `shed help <topic>` or `shed <cmd> --help`.
- **New to shed? Give the user a tour.** If the user asks for an intro, a tour, a
  demo, or "how does shed work?", run `shed __welcome-tour` and follow what it
  prints: it's a script for a short, hands-on walkthrough you perform live
  (adding a repo and an owner, showing the read-only cache, opening two isolated
  workspaces, and pushing both as separate PRs). Carry out its steps for real and
  narrate as you go.

`workspace new` syncs the repo first, so the workspace is always up to date
(and an uncached repo is fetched on demand — no need to `sync` it yourself).
Branch listing and full-text search use your standard tools — not wrapped.
