# Design: mirrors — offline workspace creation and multi-version checkouts

Status: proposal
Author: design discussion (branch `claude/workspace-cached-repo-creation-bi8yna`)

## Goal

Make workspace creation a purely local operation, and allow multiple
read-only checkouts of the same upstream at different refs.

Today `shed workspace new` runs `git clone --reference <store> -- <upstream-url>
<dest>` — the store accelerates the clone via alternates, but the clone itself
still contacts the network. That means workspace creation is slower than it
needs to be and impossible offline, even though every object it needs is
usually already on disk.

The end state:

- `shed workspace new` works instantly and offline (given a previously synced
  mirror).
- A user can keep several read-only checkouts of one upstream — e.g. Airflow
  `main`, the `v2-7-stable` branch, and the `2.7.3` tag — side by side, each
  independently referenceable by agents.
- Sync does one network fetch per upstream, no matter how many checkouts
  derive from it.

## Background: why the previous local-clone attempt was messy

An earlier exploration of clone-from-store failed for a structural reason,
not an incidental one. The store is a **non-bare** clone, so upstream branches
exist only as remote-tracking refs (`refs/remotes/origin/*`). A local
`git clone --branch feature-x <store>` cannot see those — clone only offers a
source repo's *local* branches — so creating a workspace on an arbitrary
branch required init + fetch + checkout by hand, with partial-failure rollback
at each step.

A **mirror** inverts that: a bare repo whose fetch refspec maps upstream
branches to local branches (`+refs/heads/*:refs/heads/*`). Every upstream
branch is a local branch in the mirror, so
`git clone --branch <anything> <mirror> <dest>` is a single git command again
— the same shape as today's network clone, with a local source.

### Bonus: hardlinks replace alternates

Today every workspace is permanently welded to the store through
`.git/objects/info/alternates` (the `--reference` clone has no `--dissociate`).
That is why the store runs `gc.auto=0` forever: a repack in the store would
corrupt every workspace.

A local clone from a path *without* `--reference` hardlinks the object files
instead. Same speed and disk cost (on one filesystem), but each clone is
genuinely independent: the mirror can be repacked, rebuilt, or deleted and
existing checkouts stay valid. Worst case a mirror repack breaks hardlink
sharing and disk usage creeps until old workspaces are pruned — degradation,
not corruption. The alternates coupling, and the `gc.auto=0` fragility that
came with it, disappear entirely.

## The three-tier model

| Tier | What it is | Writable? | Lifetime | Created by |
|---|---|---|---|---|
| **mirror** | bare repo, all upstream branches + tags | network only | permanent, one per upstream URL | derived — never configured directly |
| **repo** | read-only checkout of the mirror at a tracked ref | no (tree locked) | permanent, N per mirror | config (`[[repos]]`) |
| **workspace** | writable clone from the mirror | yes | disposable | agents (`shed workspace new`) |

Only two of the three are user-facing vocabulary. Users add **repos** (things
you read) and agents make **workspaces** (things you write); the **mirror** is
plumbing that surfaces only in `shed sync` output, debug messages, and docs.
If a user tracks one version of one upstream — the overwhelmingly common case
— they never need to know mirrors exist.

Mirrors are therefore **not a config entity**. They are created on demand, one
per unique upstream URL, shared by every repo that points at that URL.

### "Mirror" is colloquial, not `git clone --mirror`

Literal `--mirror` fetches `refs/*`, which on GitHub includes `refs/pull/*` —
enormous bloat for active repos. The mirror is instead a bare clone with an
explicit refspec:

```
fetch = +refs/heads/*:refs/heads/*
```

plus tags, fetched with `--prune`. Mirror-shaped, not `--mirror`.

## Config: the `track` field

`Repo` gains one optional field:

```toml
[[repos]]
url = "https://github.com/apache/airflow"
# track defaults to the upstream default branch

[[repos]]
url = "https://github.com/apache/airflow"
track = "v2-7-stable"          # a branch: advances on every sync

[[repos]]
url = "https://github.com/apache/airflow"
track = "2.7.3"                # a tag: effectively frozen; sync is a no-op
```

One field, accepting a branch or a tag (a commit SHA is cheap to allow later).
The semantics follow from what the ref names:

- **branch** — the repo advances on every sync: detached checkout at the
  mirror's current tip of that branch (exactly today's `origin/HEAD`
  behavior, generalized).
- **tag** — frozen; sync only re-checks-out if the tag itself moved.

In the rare case a branch and a tag share a name, `track` accepts the full
ref form (`heads/2.7.3`, `tags/2.7.3`) as the escape hatch; the bare short
name prefers branches, matching `git clone --branch`.

The existing per-repo `Git` config map is unchanged: it applies per repo
entity and is still seeded into workspaces at clone time via `--config`.

Owner auto-discovery always materializes default-branch repos; a `track`
override is something the user adds by hand afterward, never auto-generated.

## Naming: derived, never required

The user never invents a name. `ResolvedName()` extends today's URL derivation
to include the track:

- default branch → `github.com/apache/airflow` (unchanged)
- tracked ref → `github.com/apache/airflow@v2-7-stable`

The `@ref` convention reads instantly (npm, Docker, Go modules), works with
the existing suffix resolution (`shed workspace new airflow@v2-7-stable
fix-dag`), and bare `airflow` keeps resolving to the default checkout — the
`@`-suffixed entries are distinct, non-competing names in the same global
namespace shared with workspaces.

The optional `name` field stays as an override for anyone who prefers
`airflow-27`, but is never demanded.

### Sanitization: the track portion of a name never nests

Slashes in the `host/owner/repo` part of a name mean directory nesting, as
today. Slashes in the track portion are sanitized to `-` **at name-derivation
time**, so the canonical name is already path-safe and every downstream
consumer (`RepoStorePath`-equivalent, lock files, sync-error keys, workspace
nesting, CLI resolution) keeps working on a single string:

- `track = "release/2.8"` → name `…/airflow@release-2.8` → one leaf dir
- `track = "tags/2.7.3"` → name `…/airflow@tags-2.7.3` → one leaf dir

The mapping is lossy (the dir name doesn't round-trip to the ref), which is
fine because config is the source of truth — name→track lookup always goes
through config, never by parsing paths. The cost is a collision check:
`config.Validate` must reject two repos whose names sanitize identically
(e.g. branches `release/2.8` and `release-2.8`), loudly, at config time.
`checkSafeRelPath` already permits `@`; `ValidateName` needs no loosening.

Because names are derived, **changing `track` is an identity change** —
effectively remove-and-add, leaving the old checkout as garbage. That is the
better contract (a repo pinned to a ref is immutable; its meaning can't
silently shift under agents holding references to it), but sync or a validate
pass should notice on-disk repo dirs with no config entry and offer to prune
them.

## Path layout

```
~/.shed/
├── repos/                                    # user/agent-facing: shed prints these paths
│   └── github.com/apache/
│       ├── airflow/                          # default branch — advances on sync
│       │   └── .git/shed.meta                # per-repo state: checked-out ref
│       ├── airflow@v2-7-stable/              # branch — advances on sync
│       ├── airflow@release-2.8/              # branch "release/2.8", sanitized
│       └── airflow@2.7.3/                    # tag — frozen
├── workspaces/                               # user/agent-facing
│   └── github.com/apache/
│       ├── airflow/fix-dag/                  # workspace off the default repo
│       └── airflow@v2-7-stable/fix-dag/      # same branch name, no collision
├── logs/                                     # user-serviceable when debugging
└── .internal/                                # plumbing — never printed as a destination
    ├── mirrors/
    │   └── github.com/apache/airflow.git/    # bare mirror, one per upstream URL
    │       ├── shed.lock                     # bare repo: sidecars at top level
    │       └── shed.meta                     # LastSyncAt / LastError live here
    ├── sync-errors/                          # was .sync-errors/ — dot dropped
    ├── sessions-pending/                     # was .sessions-pending/
    ├── bg-sync.lock                          # was .bg-sync.lock
    ├── history.jsonl
    └── history-trim                          # was .history-trim
```

Decisions baked in:

- **One `.internal/` bucket instead of per-file dot-prefixes.** The rule: if
  shed ever prints a path for the user or an agent to visit, it's top-level
  (`repos/`, `workspaces/`, `logs/`); everything else lives under
  `.internal/`. This keeps `ls ~/.shed` showing exactly the two-concept
  model (plus logs), matters for agents who `ls` the parent of a repo path
  they were handed, and replaces the accumulating `.sync-errors` /
  `.sessions-pending` / `.bg-sync.lock` dot-convention with one rule — the
  moved entries lose their dots since the bucket already hides them.
  Named `.internal` (singular) after the Go convention, carrying the right
  meaning: works fine, not yours to depend on. `logs/` stays out because it
  exists *for* the user to look at when something's wrong.
- **Mirrors live under `.internal/mirrors/`**, keyed by URL-derived
  `host/owner/repo` with a `.git` suffix — the server-side convention for
  bare repos, visually unmistakable, and it separates "one per upstream"
  from "one per ref". This completes the hiding: mirrors are absent from
  config, from everyday vocabulary, and now from the visible tree. The
  mild cost is longer paths baked into repo git configs (each read-only
  repo's `origin` is its mirror path), invisible in practice.
- **Bare repos have no `.git/` dir**, so `shed.lock` / `shed.meta` sit at the
  mirror's top level. The mirror's meta owns `LastSyncAt` / `LastError`
  (the mirror owns the network); each repo's `.git/shed.meta` records only
  local checkout state. First-clone failure records (`.sync-errors/`) key by
  mirror.
- **Tag vs. branch is invisible in the path**, deliberately: the checkouts
  are structurally identical directories; the behavioral difference comes
  from what `track` resolves to in the mirror.
- **Workspaces keep their exact shape** — `<repo name>/<branch>` — with the
  possibly-`@`-suffixed repo name as the key. Two `fix-dag` workspaces off
  different Airflow checkouts never collide, and the path shows which version
  the work is based on.
- A repo remains exactly one leaf directory whose relative path *is* its
  name, so `PruneEmptyDirs` and orphan-dir detection stay trivial.

## Flows

### Sync

```
per mirror (network, exclusive lock on mirror):
  1. git fetch --prune (+refs/heads/*:refs/heads/*, tags)   ← the only network step
  2. refresh HEAD symref to upstream default branch
     (bare repos don't track it automatically; git ls-remote --symref)
  3. write shed.meta

per derived repo (local, deterministic, retryable):
  4. unlock tree → fetch/checkout track from the mirror → lock tree
  5. write .git/shed.meta
```

The network call is no longer in the middle of a mutated working tree
(today's unlock → network fetch → checkout → relock in `syncOne`). A mirror
fetch is effectively atomic at the ref level; everything after it is local
and rebuildable offline. Two Airflow checkouts cost one fetch.

### Workspace creation

```
1. optional best-effort mirror fetch (same warn-and-proceed-if-stale
   fallback as today; hard-fail only if no mirror exists at all)
2. git clone --branch <branch> [--config k=v ...] -- <mirror-path> <dest>
   (local: objects hardlinked, no --reference, no alternates)
3. git remote set-url origin <upstream-url>
4. new-branch case: git checkout -b <name>, as today
```

Step 3 is the one extra step versus today, and it is benign: a config write
that cannot partially fail in an interesting way. Failure cleanup at any step
is `rm -rf <dest>` — already the teardown model. The workspace's
remote-tracking refs were populated from the mirror and are semantically
upstream's refs; the first real `git fetch` reconciles against the true
remote.

**Base-branch default:** a workspace created via a tracked repo
(`shed workspace new airflow@v2-7-stable my-fix`) bases on that repo's
`track`, not the upstream default — an agent working against the 2.7 checkout
wants to branch off 2.7. This falls out of the data model for free.

Offline creation of a branch that exists upstream but not yet in the mirror
fails with a clear error; acceptable.

### gc policy

The mirror may repack/gc periodically (unlike today's store, where gc meant
corrupting workspaces). Cost of a repack: hardlink sharing with existing
clones breaks and disk creeps until they're pruned. Reasonable default:
leave `gc.auto=0` off the table initially and revisit; the safety property
(degradation, never corruption) holds either way.

## Alternative considered: repos as worktrees of the mirror

The README's "why not `git worktree`" rationale is about *workspaces* — its
strongest arguments (independent repo, own origin, agents may push, plain
`rm -rf` teardown) don't apply to repos, which are shed-owned, read-only, and
never push. So worktrees were considered seriously for the repo tier: a
single object DB with zero duplication, repo state definitionally in sync
with the mirror, and detached-HEAD worktrees sidestep the same-branch-twice
limitation (repos are detached checkouts anyway). Mirror gc is
worktree-aware, so gc stays safe.

Rejected on four grounds:

1. **Hard coupling.** A worktree's `.git` is a pointer file into the mirror's
   `worktrees/` admin area — delete or rebuild the mirror and every repo
   breaks immediately, versus hardlinked clones where the failure mode is
   only "disk sharing degrades". The design's best property is that every
   tier is independently `rm -rf`-able and re-derivable; worktrees carve an
   exception into it.
2. **Sidecars.** With `.git` a file, there is no local dir for
   `shed.meta`/`shed.lock`; they'd have to live in the mirror's per-worktree
   admin dir, fracturing the uniform "sidecar rides inside the thing it
   describes" pattern.
3. **Two mechanisms.** Workspaces must stay clones; repos-as-worktrees means
   two derivation mechanisms with different failure modes, cleanup
   (`git worktree prune` after `rm -rf`), and locking (worktree ops mutate
   mirror admin state → exclusive-lock contention instead of shared reads).
4. **Marginal payoff.** The initial clone hardlinks everything; worktrees
   only eliminate the *incremental* duplication from later fetches (new
   objects copied into repo-local packs), which grows with new commits only
   — and a bloated repo can simply be re-cloned from the mirror, offline.

Revisit if shed targets gigantic monorepos where incremental duplication
across several checkouts is material; worktrees (or alternates, safe here
because shed controls both sides and repos are rebuildable) become the right
lever for the repo tier specifically.

## No migration

shed is unreleased; the new layout lands as *the* layout in one change.
No old-store detection, no dual-layout code, no alternates dissociation.
Existing `~/.shed/repos/` contents from the old scheme are simply invalid —
blow away and re-sync.

This also frees internal naming: `pkg/repostore` splits/renames into a mirror
package and a repo-checkout package, path helpers rename accordingly, and the
README's design-rationale sections ("Why a read-only store…", "Why
`git clone --reference`, not `git worktree`") are rewritten for the mirror
model — the `--reference`/alternates justification no longer describes how it
works. The worktree rejection itself still stands: workspaces remain ordinary
independent repos with plain `rm -rf` teardown.

## Open questions / must-verify

1. **Hardlink behavior across filesystems** — local clone falls back to
   copying when `~/.shed` spans devices; still correct, just slower/bigger.
   No action, but worth a note in docs.
2. **`git clone --branch` with a tag source** — confirm the detached-HEAD
   result for tag-tracked repos matches what the repo checkout step expects,
   and that `--branch <tag>` from a bare local source behaves identically to
   the network case.
3. **Mirror HEAD refresh cadence** — `ls-remote --symref` is an extra network
   round-trip per sync; confirm it's cheap enough to do every sync or gate it
   (e.g. only when the fetch reports ref changes).
4. **Lock ordering** — workspace creation takes a shared lock on the mirror
   (replacing today's shared store lock); repo checkout updates take the repo
   lock; sync takes exclusive mirror then per-repo locks. Verify no path
   acquires in the opposite order.

## Implementation sequence

1. **Paths + config.** New `.internal/` root and `mirrors/` path helpers;
   relocate existing plumbing paths (`sync-errors`, `sessions-pending`,
   `bg-sync.lock`, history files) under it; `Track` field on `Repo`; name
   derivation with `@` + sanitization; `Validate` collision check (name
   uniqueness and sanitized-path uniqueness).
2. **Mirror package.** Bare clone with explicit refspec, fetch, HEAD-symref
   refresh, lock/meta sidecars at top level, `.sync-errors` keyed by mirror.
3. **Repo checkout package.** Create/update a read-only checkout from a
   mirror at `track` (local clone → detached checkout → tree lock);
   per-repo meta.
4. **Sync rewrite.** `syncOne` becomes fetch-mirror-then-update-checkouts;
   one fetch per mirror across N repos; meta split.
5. **Workspace creation rewrite.** Local clone from mirror + `remote set-url`;
   drop `--reference`; base-branch defaulting from the source repo's `track`;
   keep the best-effort-sync-first / stale-fallback behavior.
6. **CLI + resolution.** `@`-suffixed names through `Resolve`, `shed add`
   growing a way to specify `track`, orphan-dir pruning, `shed sync` output
   that mentions mirrors.
7. **Docs.** README model rewrite (repos you read, workspaces you write,
   mirrors as plumbing); embedded agent guide unchanged in vocabulary.
