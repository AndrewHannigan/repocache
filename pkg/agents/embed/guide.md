# repocache

You have a local library of git repos managed by `repocache`.

- **Read repos** at `~/.repocache/repos/<host>/<owner>/<repo>/` (read-only).
  Search and read them with your usual tools. Do not modify files here.
- **List the library**: `repocache repo list` (a `⚠ sync failing` marker means
  that repo's cached copy is stale — its last fetch failed).
- **Stale cache?** If a repo is marked failing (or you see a STALE CACHE banner above),
  treat what you read from it as possibly out of date and tell the user. Run
  `repocache status <repo>` for the error and the suggested fix.
- **Edit a repo**: `repocache workspace new <repo> <branch>` creates a writable workspace
  and prints its path. Make changes there, then commit, push, open PR as normal.
  But first — check for a local checkout collision (next bullet).
- **⚠️ Before editing, check for a local checkout collision.** If the repo you're about to
  edit is *also* checked out somewhere else on disk — a separate clone (e.g. under `~/src`),
  which may even be your current working directory — then STOP and ask the user which to
  use: edit that checkout in place, or create a `repocache workspace`. Do not assume: the
  two are independent clones, so the choice decides where your commits actually land. This
  applies even when the existing checkout is the directory you were launched in.
- **Clean up**: `repocache workspace rm <repo> <branch>` when done.
- **Need a repo not in the library?** Ask the user to run `repocache repo add <repo>`
  (a full URL or GitHub `owner/repo` shorthand).
- **Track a whole user/org?** `repocache repo add <owner>` (a bare `owner` or
  `https://github.com/<owner>`) tracks every repo under that owner; `sync` discovers
  and fetches new ones automatically. Needs `gh` installed and authenticated.
- **More details**: `repocache help <topic>` or `repocache <cmd> --help`.

If `workspace new` says "not in cache", run `repocache sync <repo>` first.
Branch listing and full-text search use your standard tools — not wrapped.
