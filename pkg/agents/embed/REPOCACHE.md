# repocache

You have a local library of git repos managed by `repocache`.

- **Read repos** at `~/.local/share/repocache/repos/<host>/<owner>/<repo>/` (read-only).
  Search and read them with your usual tools. Do not modify files here.
- **List the library**: `repocache repo list`
- **Edit a repo**: `repocache workspace new <repo> <branch>` creates a writable workspace
  and prints its path. Make changes there, then commit, push, open PR with `gh` as normal.
- **Library repo also checked out locally?** If the same repo exists as a separate
  working-directory checkout (e.g. under `~/src`), don't assume — ask the user whether
  to edit that checkout in place or create a `repocache workspace`.
- **Clean up**: `repocache workspace rm <repo> <branch>` when done.
- **Need a repo not in the library?** Ask the user to run `repocache repo add <url>`.
- **More details**: `repocache help <topic>` or `repocache <cmd> --help`.

If `workspace new` says "not in cache", run `repocache sync <repo>` first.
Branch listing and full-text search use your standard tools — not wrapped.
