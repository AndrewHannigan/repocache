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

### Object sharing: hardlinks for workspaces, worktrees for repos

Today every workspace is permanently welded to the store through
`.git/objects/info/alternates` (the `--reference` clone has no `--dissociate`).
That is why the store runs `gc.auto=0` forever: a repack in the store would
corrupt every workspace.

The new design picks a mechanism per tier by two questions: does the tier
hold user work (then it must be independent of the mirror), and is it
long-lived (then git itself must know it depends on the mirror, so
maintenance respects it)?

- **Workspaces** hold user work — corruption is unacceptable, so they get a
  plain local clone, no `--reference`. Git hardlinks the object files: same
  speed and near-zero disk (on one filesystem), but fully independent — the
  mirror can be repacked, rebuilt, or deleted and workspaces stay valid.
  Worst case sharing degrades and disk creeps until old workspaces are
  pruned — acceptable precisely because workspaces are ephemeral.
- **Repos** are long-lived, shed-owned, read-only, and never push — so they
  are **detached worktrees of the mirror** (`git worktree add --detach`).
  One object DB, zero duplication ever, and — decisively — **gc-safety by
  construction**: the mirror's gc counts every worktree's HEAD as a
  reachability root, so it can never prune objects a repo's checkout still
  needs. The dependency is declared to git rather than managed by shed: no
  `.keep` bookkeeping, no defensive gc scheduling, no broken-repo re-derive
  path. (See "gc: owned by shed, run in prune" for the full accounting.)

## The three-tier model

| Tier | What it is | Writable? | Lifetime | Created by |
|---|---|---|---|---|
| **mirror** | bare repo, all upstream branches + tags | network only | permanent, one per upstream URL | derived — never configured directly |
| **repo** | read-only detached worktree of the mirror at a tracked ref | no (tree locked) | permanent, N per mirror | config (`[[repos]]`) |
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

**One repo per `(url, track)` is an invariant**, enforced by
`config.Validate` even under explicit `name` overrides. Two repos of the
same upstream at the same ref would be identical read-only trees — pure
duplication with no use — and forbidding them is also what lets repos be
worktrees without ever meeting git's same-branch-twice restriction.

The existing per-repo `Git` config map is unchanged in meaning: it applies
per repo entity and is still seeded into workspaces at clone time via
`--config`. On repos — worktrees sharing the mirror's config file — per-repo
values are set via `extensions.worktreeConfig`, so they never leak to the
mirror or sibling repos.

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
│       │   └── .git                          # a FILE → worktree of the mirror
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
  mild cost is absolute mirror paths baked into each repo's `.git` pointer
  file, invisible in practice.
- **Bare repos have no `.git/` dir**, so `shed.lock` / `shed.meta` sit at the
  mirror's top level. The mirror's meta owns `LastSyncAt` / `LastError`
  (the mirror owns the network). A repo, being a worktree, needs almost no
  state of its own — its checkout state *is* its HEAD and its identity is
  in config; anything left (a per-repo error record, say) lives in the
  mirror's `worktrees/<id>/` admin dir, which is `.internal` plumbing
  anyway. First-clone failure records (`sync-errors/`) key by mirror.
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
  4. unlock tree → git checkout --detach <track, BY REF NAME> → lock tree
     (a worktree reads the mirror's refs directly: no fetch, no objects
      move; creating a repo is the same operation from zero, via
      git worktree add --detach)
```

The network call is no longer in the middle of a mutated working tree
(today's unlock → network fetch → checkout → relock in `syncOne`). A mirror
fetch is effectively atomic at the ref level; everything after it is local
and rebuildable offline. Two Airflow checkouts cost one fetch.

#### What a branch-tracked repo looks like from inside

Detached HEAD raised a UX worry: a user who knows the repo "tracks main"
cd's in and sees a bare SHA. The affordances mostly answer it, given one
rule — **sync detaches by ref name, never raw SHA** (git records the detach
point):

- `git status` → `HEAD detached at main`
- `git log -1` → tip commit decorated `(HEAD, main)` — a worktree shares
  the mirror's refs, and the mirror's `main` is a real local branch at
  that commit
- shell prompts are the one uncontrolled surface: branch-aware prompts
  typically render detached HEAD as a short SHA. Acceptable — the first
  orienting command a confused user runs answers in the word "main", and
  `shed ls` should surface TRACK/SYNCED per repo as the authoritative
  answer.

Two rejected alternatives for non-detached repos (both trade the design's
sync properties for a prompt label):

- **Shed-owned per-repo branches** (`shed/<name>`, force-moved each sync,
  checked out non-detached — upstream never fetches into that name, so no
  fetch refusal). Rejected: ref-namespace pollution, `checkout -B`
  choreography every sync, per-repo branch names to dodge the
  one-checkout-per-branch rule, and the visible label (`shed/airflow`)
  explains no more than `detached at main`.
- **Pull-through-worktree**: check out the real branch, exclude
  checked-out branches from the mirror fetch (dynamic negative refspecs),
  and `git pull` inside each worktree — allowed, since pull moves the ref
  and working tree together. Rejected: pull has no local source to merge
  from (the mirror has no separate remote-tracking refs), so each
  branch-tracked repo fetches the network itself — breaking one-fetch-
  per-upstream, fragmenting ref consistency across per-repo fetch times,
  and re-interleaving network with tree mutation. Plus force-push needs a
  reset path and tag repos stay detached anyway (two modes). Nothing is
  saved: the tree unlock and the per-repo update step exist either way.

### Workspace creation

```
1. optional best-effort mirror fetch (same warn-and-proceed-if-stale
   fallback as today; hard-fail only if no mirror exists at all)
2. git clone --branch <branch> --config gc.auto=0 [--config k=v ...] \
     -- <mirror-path> <dest>
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

**Workspaces derive from the mirror, never from a repo.** The tiers are
siblings, not a chain: a repo is a worktree — no object store of its own,
one detached ref — so it cannot serve as a clone source, and routing user
work through disposable plumbing would be wrong even if it could. The repo
named on the CLI is a resolution and defaults source only — it selects the
mirror, supplies the base-branch default, and seeds the per-repo `Git`
config.

**Base-branch default:** a workspace created via a tracked repo
(`shed workspace new airflow@v2-7-stable my-fix`) bases on that repo's
`track`, not the upstream default — an agent working against the 2.7 checkout
wants to branch off 2.7. This falls out of the data model for free.

Offline creation of a branch that exists upstream but not yet in the mirror
fails with a clear error; acceptable.

### gc: owned by shed, run in prune

`gc.auto=0` is pinned on every tier. That is not turning gc off — it is
transferring the responsibility for running it from git's per-repo
heuristics to shed, which knows the sharing topology. An auto-gc firing at
a moment of git's choosing is exactly what the topology can't tolerate:
inside a workspace it would rewrite the hardlinked packs and privatize the
entire shared base (a ~20 GB disk event on a big monorepo — not corruption,
but a shock); inside the mirror it would run outside shed's lock
discipline. So shed runs gc itself.

**It runs in `shed prune`, not sync.** Sync is frequent, fast, and should
stay network-focused; prune is the explicit, comparatively rare "reclaim
disk" moment, which the user is already watching. Prune's ordering is
deliberate:

1. remove landed/aged workspaces (as today)
2. `git gc` each mirror, under its exclusive lock
3. `git worktree prune` each mirror (clears admin records of removed repos)

Removing workspaces first matters: a mirror repack writes new inodes, and
surviving hardlinked workspaces keep the old packs alive (still one shared
copy *among themselves*) — repacking after removal minimizes that
un-shared remainder. No chmod dance is needed anywhere: gc writes only the
mirror's own object/ref store, never a repo's locked working tree.

Per tier:

- **Mirror** — the only tier with garbage to collect. Worktree HEADs (the
  repos) are gc reachability roots, so collection can never prune objects a
  repo's checkout needs — safe by construction, at any time, with no
  defensive scheduling. A conservative prune expiry remains cheap insurance.
- **Repos** — no object store of their own; nothing to collect, ever.
- **Workspaces** — shed never gc's them, and `gc.auto=0` (seeded at
  creation) means git doesn't either. A long-lived workspace therefore
  drifts: loose objects from agent commits accumulate, and a mirror repack
  un-shares its base. Accepted deliberately — workspaces are ephemeral, the
  bloat is temporary by definition, and prune deletes them wholesale.

The underlying asymmetry that shaped all of this: **clone hardlinks, fetch
copies.** A local-path clone hardlinks pack files (workspace creation is
~free even at 20 GB), but fetch always writes new private packs — which is
why long-lived advancing checkouts must not be fetching clones. Repos avoid
fetching entirely: as worktrees they read the mirror's refs directly, and a
tag-pinned repo touches nothing at all.

Sizing intuition, large monorepo: 20 GB of objects, ~100 MB/week of packed
churn, three advancing repo checkouts. The base is stored once (mirror),
plus at most one aging shared copy across surviving workspaces after a
repack; repos add zero, permanently. Hardlink-cloned repos would instead
grow ~5 GB/year per advancing checkout.

## Alternatives considered for the repo tier

Three mechanisms were weighed for the long-lived read-only checkouts. The
design initially chose (2) and was revised to (3) when the gc analysis was
worked through.

1. **Hardlink clones (like workspaces).** Zero cost at clone time, but
   *fetch copies*: an advancing checkout accumulates the full upstream
   churn privately, forever (~5 GB/year per checkout on a busy monorepo),
   plus one pack per fetch. Fine for ephemeral workspaces, wrong for
   permanent repos. (`.keep`-marking the base packs was considered to make
   this safe under gc; rejected as hand-reimplementing the dependency
   tracking worktrees give natively — which packs to mark, when removal is
   safe.)
2. **`--shared` clones (alternates to the mirror).** Zero duplication and a
   real `.git` dir — but the dependency is *invisible to git*: the mirror's
   gc doesn't know the repo exists, so a force-pushed upstream branch plus
   prune could remove objects a repo's detached checkout still references.
   Living with that means defensive gc scheduling, broken-repo detection,
   and a re-derive path — shed hand-managing what git can't see.
3. **Detached worktrees of the mirror (chosen).** Same zero duplication,
   but the dependency is *declared*: worktree HEADs are gc reachability
   roots, so the mirror can be collected at any time with no corruption
   race and no bookkeeping. The costs are real but small and entirely
   shed-internal: `.git` is a pointer file (per-repo state shrinks to
   nearly nothing; leftovers live in the mirror's `worktrees/<id>/` admin
   dir), per-repo git config needs `extensions.worktreeConfig`, removal is
   `git worktree remove` rather than bare `rm -rf`, and worktree operations
   serialize on the mirror's exclusive lock.

Two worktree limitations are non-issues here, one by design and one by
invariant:

- **Detached HEADs are mandatory, not a preference.** Git refuses to fetch
  into a branch that is checked out in *any* worktree — a repo worktree
  sitting on `main` would make the mirror's own `+refs/heads/*` fetch fail
  for `main`. Detached checkouts keep every branch fetchable; sync moves
  the repo afterward with `checkout --detach`.
- **Same-branch-twice never arises.** Two repos of one upstream at the same
  `track` would be identical read-only trees — pure duplication with no
  use. The derived `@track` naming already collides them at config time,
  and `config.Validate` rejects duplicate `(url, track)` pairs even under
  explicit `name` overrides. The constraint costs nothing because the
  thing it forbids is worthless.

The README's "why not `git worktree`" rationale is re-scoped, not reversed:
it argued from user work, pushing, and independent teardown — all true of
workspaces, none true of repos.

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
works. The README's worktree rejection is re-scoped rather than reversed: it
still holds for workspaces (user work, independence, plain `rm -rf`
teardown), while repos — shed-owned read-only checkouts — are now exactly
worktrees.

## Open questions / must-verify

1. **Hardlink behavior across filesystems** — local clone falls back to
   copying when `~/.shed` spans devices; still correct, just slower/bigger.
   No action, but worth a note in docs.
2. **`git clone --branch` with a tag** — a workspace based off a
   tag-tracked repo clones with `--branch <tag>`; confirm the detached-HEAD
   + `checkout -b` sequence behaves identically from a bare local source as
   over the network.
3. **Mirror HEAD refresh cadence** — `ls-remote --symref` is an extra network
   round-trip per sync; confirm it's cheap enough to do every sync or gate it
   (e.g. only when the fetch reports ref changes).
4. **Lock ordering** — workspace creation takes a shared lock on the mirror
   (replacing today's shared store lock); worktree add/remove/update and gc
   mutate mirror state and take the exclusive lock. Verify no path acquires
   locks in conflicting order and that repo updates during sync serialize
   acceptably.
5. **Worktree mechanics on the installed git** — `git worktree add --detach`
   from a bare repo; repeated `checkout --detach` updates; gc (including
   `--aggressive` and `--prune=now`) treating worktree HEADs as reachability
   roots; `git worktree remove` on a chmod-locked tree.
6. **Worktree config + tree locking** — the read-only chmod walk currently
   excludes the `.git` dir; with worktrees it must exclude (and never follow)
   the `.git` pointer file. Verify `extensions.worktreeConfig` keeps per-repo
   `Git` config out of the mirror and sibling repos.

## Implementation sequence

1. **Paths + config.** New `.internal/` root and `mirrors/` path helpers;
   relocate existing plumbing paths (`sync-errors`, `sessions-pending`,
   `bg-sync.lock`, history files) under it; `Track` field on `Repo`; name
   derivation with `@` + sanitization; `Validate` collision check (name
   uniqueness and sanitized-path uniqueness).
2. **Mirror package.** Bare clone with explicit refspec, fetch, HEAD-symref
   refresh, lock/meta sidecars at top level, `.sync-errors` keyed by mirror.
3. **Repo worktree package.** Create a read-only checkout as a detached
   worktree of the mirror (`git worktree add --detach` → tree lock); update
   = unlock → `checkout --detach <track tip>` → lock; removal via
   `git worktree remove` (+ prune); per-repo git config via
   `extensions.worktreeConfig`.
4. **Sync rewrite.** `syncOne` becomes fetch-mirror-then-update-worktrees;
   one fetch per mirror across N repos; meta moves to the mirror.
5. **Workspace creation rewrite.** Local clone from mirror (`gc.auto=0`
   seeded) + `remote set-url`; drop `--reference`; base-branch defaulting
   from the source repo's `track`; keep the best-effort-sync-first /
   stale-fallback behavior.
6. **Prune gains gc.** After workspace removal: `git gc` per mirror under
   its exclusive lock, then `git worktree prune`; orphan repo-dir detection
   (dirs with no config entry).
7. **CLI + resolution.** `@`-suffixed names through `Resolve`, `shed add`
   growing a way to specify `track`, `shed sync` output that mentions
   mirrors.
8. **Docs.** README model rewrite (repos you read, workspaces you write,
   mirrors as plumbing); embedded agent guide unchanged in vocabulary.
