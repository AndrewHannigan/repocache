package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/forge"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
)

const syncLockTimeout = 5 * time.Minute

// syncDefaultJobs is the default concurrency for `sync`, also used when
// `add` triggers an implicit sync of the just-added entry.
const syncDefaultJobs = 4

func newSyncCmd() *cobra.Command {
	var (
		jobs        int
		ifOlderThan time.Duration
		jsonOut     bool
	)
	cmd := &cobra.Command{
		Use:   "sync [<name>...]",
		Short: "Fetch tracked repos and refresh their read-only working trees",
		Long: `sync fetches each tracked repo (or the named subset), checks out
origin/HEAD detached, and re-applies chmod -R a-w on the working tree
so the store stays read-only.

With --if-older-than, skip repos synced within the given duration.
Runs in parallel up to --jobs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(args, jobs, ifOlderThan, jsonOut)
		},
	}
	cmd.Flags().IntVarP(&jobs, "jobs", "j", syncDefaultJobs, "max concurrent fetches")
	cmd.Flags().DurationVar(&ifOlderThan, "if-older-than", 0, "skip repos synced within this duration (e.g. 1h)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit NDJSON results")
	return cmd
}

type syncResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // "ok" | "skipped" | "error"
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
	Note       string `json:"note,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`

	// locked marks an error caused by a store-lock timeout (vs a network
	// failure), so callers can classify it without matching on Error text.
	// Not serialized — the message in Error carries the user-facing detail.
	locked bool
}

type syncTarget struct {
	name, url string
	git       map[string]string
}

func runSync(names []string, jobs int, ifOlderThan time.Duration, jsonOut bool) error {
	if err := repostore.RequireGit(); err != nil {
		return errs.Wrap(errs.MissingDep, err)
	}
	if jobs < 1 {
		jobs = 1
	}

	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if len(c.Repos) == 0 && len(c.Owners) == 0 {
		if !jsonOut {
			fmt.Fprintln(os.Stderr, "no repos in config; add with `shed add <url>`")
		}
		return nil
	}

	// Discover repos for any owners in scope and add new ones to config, so
	// repos that appeared upstream since the last sync are picked up and
	// fetched in this same pass. Failures here are warned about and skipped —
	// already-known repos still sync (graceful degradation when gh is absent).
	if owners := ownersInScope(c, names); len(owners) > 0 {
		reconcileOwners(owners, forge.ListOwnerRepos, jsonOut)
		c, err = config.Load() // reload to include newly added repos
		if err != nil {
			return errs.Wrap(errs.Config, err)
		}
	}

	targets, err := resolveSyncTargets(c, names)
	if err != nil {
		return err
	}

	if !jsonOut {
		fmt.Printf("syncing %d repos (jobs=%d)\n", len(targets), jobs)
	}

	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var enc *json.Encoder
	if jsonOut {
		enc = json.NewEncoder(os.Stdout)
	}
	results := make([]syncResult, 0, len(targets))

	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t syncTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			r := syncOne(t.name, t.url, t.git, ifOlderThan)
			mu.Lock()
			results = append(results, r)
			if jsonOut {
				_ = enc.Encode(r)
			} else {
				printSyncLine(r)
			}
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	return summarizeSync(results, len(targets), jsonOut)
}

func resolveSyncTargets(c *config.Config, names []string) ([]syncTarget, error) {
	if len(names) == 0 {
		out := make([]syncTarget, 0, len(c.Repos))
		for _, r := range c.Repos {
			n, err := r.ResolvedName()
			if err != nil {
				return nil, errs.Wrap(errs.Config, err)
			}
			out = append(out, syncTarget{n, r.URL, r.Git})
		}
		return out, nil
	}
	out := make([]syncTarget, 0, len(names))
	seen := make(map[string]bool)
	add := func(name, url string, git map[string]string) {
		if !seen[name] {
			out = append(out, syncTarget{name, url, git})
			seen[name] = true
		}
	}
	for _, name := range names {
		// A name may be a repo or an owner. Resolve both: a repo expands to
		// itself, an owner expands to its managed repos. Matching both (a rare
		// suffix collision — exact names are unique per §4) is ambiguous → exit
		// 2 asking for the full name, identical to `repo rm` (§5.0).
		r, repoErr := c.Resolve(name)
		o, ownerErr := c.ResolveOwner(name)
		switch {
		case repoErr == nil && ownerErr == nil:
			rn, _ := r.ResolvedName()
			on, _ := o.ResolvedName()
			return nil, errs.New(errs.NotFound,
				"%q is ambiguous; matches owner %q and repo %q — use the full name", name, on, rn)
		case repoErr == nil:
			n, err := r.ResolvedName()
			if err != nil {
				return nil, errs.Wrap(errs.Config, err)
			}
			add(n, r.URL, r.Git)
		case ownerErr == nil:
			on, err := o.ResolvedName()
			if err != nil {
				return nil, errs.Wrap(errs.Config, err)
			}
			for _, rn := range c.ReposForOwner(on) {
				if rr := c.FindByName(rn); rr != nil {
					add(rn, rr.URL, rr.Git)
				}
			}
		default:
			return nil, repoErr // repo not-found/ambiguous message is the common case
		}
	}
	return out, nil
}

// ownerLister lists an owner's repos. forge.ListOwnerRepos in production; a
// fake in tests.
type ownerLister func(ownerURL string, f forge.Filter) ([]forge.Repo, error)

// ownersInScope returns the owners a sync invocation should reconcile: all
// owners when no names are given, otherwise just those names that resolve to
// an owner (repo names are handled separately by resolveSyncTargets).
func ownersInScope(c *config.Config, names []string) []config.Owner {
	if len(names) == 0 {
		return c.Owners
	}
	var owners []config.Owner
	seen := make(map[string]bool)
	for _, name := range names {
		o, err := c.ResolveOwner(name)
		if err != nil {
			continue
		}
		on, _ := o.ResolvedName()
		if !seen[on] {
			owners = append(owners, *o)
			seen[on] = true
		}
	}
	return owners
}

func ownerFilter(o config.Owner) forge.Filter {
	return forge.Filter{
		IncludeForks:    o.IncludeForks,
		IncludeArchived: o.IncludeArchived,
		Visibility:      o.Visibility,
	}
}

// reconcileOwners discovers each owner's repos via list and adds any new ones
// to config as Source-tagged entries. It is additive only — it never removes
// repos that disappeared upstream, which would risk deleting a workspace with
// unpushed work. Discovery failures are warned about and skipped so that
// already-known repos still sync (graceful degradation when gh is unavailable).
func reconcileOwners(owners []config.Owner, list ownerLister, jsonOut bool) {
	for _, o := range owners {
		ownerName, err := o.ResolvedName()
		if err != nil {
			warnSync("skipping owner with unparseable URL %q: %v", o.URL, err)
			continue
		}
		added, err := reconcileOwner(o, list)
		if err != nil {
			warnSync("could not expand owner %s: %v", ownerName, err)
			continue
		}
		if len(added) > 0 && !jsonOut {
			fmt.Printf("  owner %s: discovered %s\n", ownerName, pluralize(len(added), "new repo"))
		}
	}
}

// reconcileOwner lists one owner's repos (outside the config lock) and appends
// the new ones under the config lock. Returns the resolved names added.
func reconcileOwner(o config.Owner, list ownerLister) (added []string, err error) {
	repos, err := list(o.URL, ownerFilter(o))
	if err != nil {
		return nil, err
	}
	err = config.WithLock(configLockTimeout, func(c *config.Config) error {
		toAdd := newOwnerRepos(c, o, repos)
		if len(toAdd) == 0 {
			return nil
		}
		c.Repos = append(c.Repos, toAdd...)
		for _, r := range toAdd {
			n, _ := r.ResolvedName()
			added = append(added, n)
		}
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return nil, errs.Wrap(errs.Locked, err)
		}
		return nil, err
	}
	return added, nil
}

// newOwnerRepos returns the Repo entries to append for a discovered set,
// skipping any whose resolved name already exists in c (as a user repo, a
// managed repo, or an owner), is in the owner's exclude list, or is a
// duplicate within the discovered batch. Pure, so the additive/dedupe logic
// is unit-testable without gh or disk.
func newOwnerRepos(c *config.Config, o config.Owner, discovered []forge.Repo) []config.Repo {
	ownerName, err := o.ResolvedName()
	if err != nil {
		return nil
	}
	exclude := make(map[string]bool)
	for _, e := range o.Exclude {
		exclude[e] = true
	}
	var toAdd []config.Repo
	queued := make(map[string]bool)
	for _, d := range discovered {
		if d.CloneURL == "" {
			continue
		}
		name, err := paths.DefaultName(d.CloneURL)
		if err != nil {
			continue
		}
		if c.FindByName(name) != nil || c.FindOwnerByName(name) != nil || queued[name] || exclude[name] {
			continue
		}
		queued[name] = true
		toAdd = append(toAdd, config.Repo{URL: d.CloneURL, Source: ownerName})
	}
	return toAdd
}

// warnSync writes a discovery warning to stderr. It always uses stderr so it
// never corrupts NDJSON results on stdout in --json mode.
func warnSync(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", a...)
}

func syncOne(name, url string, git map[string]string, ifOlderThan time.Duration) syncResult {
	start := time.Now()
	r := syncResult{Name: name}

	if !repostore.Exists(name) {
		if err := repostore.Clone(url, name); err != nil {
			return finishErr(r, start, err)
		}
	}

	lock, err := repostore.AcquireLock(name, true, syncLockTimeout)
	if err != nil {
		if errors.Is(err, repostore.ErrLocked) {
			r.locked = true
			return finishErr(r, start, fmt.Errorf(
				"locked: could not acquire %s within %s (held by another shed process)",
				paths.RepoStoreLockFile(name), syncLockTimeout))
		}
		return finishErr(r, start, err)
	}
	defer lock.Unlock()

	if ifOlderThan > 0 {
		if meta, err := repostore.LoadMeta(name); err == nil && meta != nil {
			if d := time.Since(meta.LastSyncAt); d < ifOlderThan {
				r.Status = "skipped"
				r.Note = fmt.Sprintf("synced %s ago", relDuration(d))
				r.DurationMs = time.Since(start).Milliseconds()
				return r
			}
		}
	}

	// Re-enable write before fetch + checkout (prior sync left the tree chmod a-w).
	// Empty tree (first sync) is fine; UnlockTree is a no-op then.
	if err := repostore.UnlockTree(name); err != nil {
		return finishErr(r, start, fmt.Errorf("chmod u+w: %w", err))
	}
	if err := repostore.Fetch(name); err != nil {
		return finishErr(r, start, err)
	}
	// Reconcile per-repo git config into the store's .git/config so options
	// added to config after the initial clone take effect on the next sync.
	if err := repostore.SetConfig(name, git); err != nil {
		return finishErr(r, start, err)
	}
	// An empty remote (no commits pushed) has no origin/HEAD to check out;
	// leave the tree empty and record a successful, "empty" sync rather than
	// failing every time. A later push makes origin/HEAD resolve normally.
	hasHEAD, err := repostore.RemoteHEADResolves(name)
	if err != nil {
		return finishErr(r, start, err)
	}
	if hasHEAD {
		if err := repostore.CheckoutDetachedHEAD(name); err != nil {
			return finishErr(r, start, err)
		}
	}
	if err := repostore.LockTree(name); err != nil {
		return finishErr(r, start, fmt.Errorf("chmod a-w: %w", err))
	}
	if err := repostore.SaveMeta(name, &repostore.Meta{LastSyncAt: time.Now().UTC()}); err != nil {
		return finishErr(r, start, fmt.Errorf("write meta: %w", err))
	}
	// Success: drop any standalone first-sync failure record from an earlier
	// failed clone so it doesn't keep showing up as stale.
	repostore.ClearFirstSyncError(name)

	r.Status = "ok"
	if !hasHEAD {
		r.Note = "empty"
	}
	if size, err := repostore.Size(name); err == nil {
		r.SizeBytes = size
	}
	r.DurationMs = time.Since(start).Milliseconds()
	return r
}

func finishErr(r syncResult, start time.Time, err error) syncResult {
	r.Status = "error"
	r.Error = err.Error()
	r.DurationMs = time.Since(start).Milliseconds()
	// Persist the failure so `ls`, `status`, and the session-context snapshot
	// can surface it. Best-effort: keep the prior LastSyncAt so the table
	// still shows the last *successful* sync.
	if repostore.Exists(r.Name) {
		m, _ := repostore.LoadMeta(r.Name)
		if m == nil {
			m = &repostore.Meta{}
		}
		m.LastError = err.Error()
		m.LastErrorAt = time.Now().UTC()
		_ = repostore.SaveMeta(r.Name, m)
	} else {
		// A failed first clone leaves no store dir, so there's no meta sidecar
		// to write. Record the error in the standalone store instead — without
		// it, status would report "synced cleanly" and the staleness banner
		// would stay silent, the worst outcome during onboarding.
		_ = repostore.RecordFirstSyncError(r.Name, err.Error())
	}
	return r
}

func printSyncLine(r syncResult) {
	switch r.Status {
	case "ok":
		if r.Note != "" {
			fmt.Printf("  %s  ✓  %s — %s  (%s)\n", r.Name, humanSize(r.SizeBytes), r.Note, formatMs(r.DurationMs))
		} else {
			fmt.Printf("  %s  ✓  %s  (%s)\n", r.Name, humanSize(r.SizeBytes), formatMs(r.DurationMs))
		}
	case "skipped":
		fmt.Printf("  %s  -  skipped (%s)\n", r.Name, r.Note)
	case "error":
		fmt.Printf("  %s  ✗  %s\n", r.Name, r.Error)
	}
}

func summarizeSync(results []syncResult, total int, jsonOut bool) error {
	var ok, skip, errCnt, lockCnt, netCnt int
	for _, r := range results {
		switch r.Status {
		case "ok":
			ok++
		case "skipped":
			skip++
		case "error":
			errCnt++
			if r.locked {
				lockCnt++
			} else {
				netCnt++
			}
		}
	}
	if !jsonOut {
		fmt.Printf("%d of %d ok; %d failed; %d skipped\n", ok, total, errCnt, skip)
	}
	if lockCnt > 0 {
		return errs.New(errs.Locked, "%d repos failed to acquire lock", lockCnt)
	}
	if netCnt > 0 {
		return errs.New(errs.Network, "%d repos failed", netCnt)
	}
	return nil
}
