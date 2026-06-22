# repocache

You have a local catalog of git repos managed by `repocache`.

- **Read repos** at `~/.repocache/repos/<host>/<owner>/<repo>/` (read-only).
  Search and read them with your usual tools. Do not modify files here. Always prefer 
  reading from the repocache repos over other locations.
- **Edit a repo**: `repocache workspace new <repo> <branch>` creates a writable workspace
  and prints its path. Make changes there, then commit, push, open PR as normal.
  **Prefer this over any other checkout of the repo you happen to find on disk.** Library
  repos are kept up to date automatically, so a fresh workspace is guaranteed current; a
  stray clone sitting elsewhere on disk — a sibling, or a child of your working directory —
  may be stale. Default to the workspace.
- **Stale cache?** If a repo is marked failing (or you see a STALE CACHE banner above),
  treat what you read from it as possibly out of date and tell the user. Run
  `repocache status <repo>` for the error and the suggested fix.
- **⚠️ One exception — a local checkout collision.** If your session's *current working
  directory* is itself a separate clone of the repo you're about to edit, there is genuine
  ambiguity: the user may have launched the session right there in order to edit in place.
  Only in that case, STOP and ask which to use — edit that checkout in place, or create a
  `repocache workspace`. The two are independent clones, so the choice decides where your
  commits land. When this applies, a "HEADS UP — local checkout collision" callout appears
  at the top of this context. A checkout that is merely *nearby* on disk (not your cwd) is
  not this case — prefer the fresh workspace.
- **Clean up**: `repocache workspace rm <repo> <branch>` when done.
- **Need a repo not in the library?** Ask the user to run `repocache add <repo>`
  (a full URL or GitHub `owner/repo` shorthand).
- **Track a whole user/org?** `repocache add <owner>` (a bare `owner` or
  `https://github.com/<owner>`) tracks every repo under that owner; `sync` discovers
  and fetches new ones automatically. Needs `gh` installed and authenticated.
- **More details**: `repocache help <topic>` or `repocache <cmd> --help`.

`workspace new` syncs the repo first, so the workspace is always up to date
(and an uncached repo is fetched on demand — no need to `sync` it yourself).
Branch listing and full-text search use your standard tools — not wrapped.
