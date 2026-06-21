package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/forge"
	"github.com/AndrewHannigan/repocache/pkg/paths"
	"github.com/AndrewHannigan/repocache/pkg/workspace"
)

const configLockTimeout = 2 * time.Second

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the library of tracked repositories",
	}
	cmd.AddCommand(newRepoAddCmd(), newRepoRmCmd(), newRepoListCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var name string
	var asOwner, asRepo bool
	cmd := &cobra.Command{
		Use:   "add <repo>",
		Short: "Add a repository, or a whole user/org, to the library",
		Long: `add appends a repo to the library. <repo> may be a full git URL
(https://, ssh://, or scp-style git@host:owner/repo) or GitHub shorthand:
a bare "owner/repo" or "owner" is expanded against github.com, so
"repocache repo add octocat/Hello-World" and "repocache repo add octocat"
both just work.

If <repo> points at a bare user or org (a single path segment, e.g.
octocat or https://github.com/octocat), it is tracked as an owner instead:
each sync discovers that owner's repos via gh and adds any new ones
automatically.

Detection is automatic from the shape; force it with --owner / --repo.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoAdd(args[0], name, asOwner, asRepo)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "override the default name (derived from URL)")
	cmd.Flags().BoolVar(&asOwner, "owner", false, "track <repo> as a user/org (discover its repos via gh)")
	cmd.Flags().BoolVar(&asRepo, "repo", false, "track <repo> as a single repo (default for owner/repo references)")
	return cmd
}

func runRepoAdd(input, overrideName string, asOwner, asRepo bool) error {
	if asOwner && asRepo {
		return errs.New(errs.Config, "--owner and --repo are mutually exclusive")
	}
	// Expand shorthand (e.g. "octocat" or "owner/repo") into a full
	// URL up front so classification, naming, and the stored config entry all
	// use the same canonical form.
	url := paths.NormalizeURL(input)
	isOwner := asOwner
	if !asOwner && !asRepo {
		detected, err := paths.IsOwnerURL(url)
		if err != nil {
			return errs.Wrap(errs.Config, err)
		}
		isOwner = detected
	}
	if isOwner {
		return runOwnerAdd(url, overrideName)
	}
	return runRepoAddOne(url, overrideName)
}

func runRepoAddOne(url, overrideName string) error {
	// Validate URL and derive default name up front so we fail before locking.
	defaultName, err := paths.DefaultName(url)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	effectiveName := defaultName
	if overrideName != "" {
		effectiveName = overrideName
	}

	err = config.WithLock(configLockTimeout, func(c *config.Config) error {
		if c.FindByName(effectiveName) != nil {
			return errs.New(errs.Exists, "repo %q is already in the config", effectiveName)
		}
		if c.FindOwnerByName(effectiveName) != nil {
			return errs.New(errs.Exists, "%q is already tracked as an owner", effectiveName)
		}
		c.Repos = append(c.Repos, config.Repo{URL: url, Name: overrideName})
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return wrapIfNotCoded(err, errs.Config)
	}
	fmt.Printf("added %s\n", effectiveName)
	// Fetch the new repo right away so the cache is populated without a
	// separate `repocache sync`. Scoped to just this repo.
	return runSync([]string{effectiveName}, syncDefaultJobs, 0, false)
}

func runOwnerAdd(url, overrideName string) error {
	defaultName, err := paths.DefaultOwnerName(url)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	effectiveName := defaultName
	if overrideName != "" {
		effectiveName = overrideName
	}

	err = config.WithLock(configLockTimeout, func(c *config.Config) error {
		if c.FindOwnerByName(effectiveName) != nil {
			return errs.New(errs.Exists, "owner %q is already in the config", effectiveName)
		}
		if c.FindByName(effectiveName) != nil {
			return errs.New(errs.Exists, "%q is already tracked as a repo", effectiveName)
		}
		c.Owners = append(c.Owners, config.Owner{URL: url, Name: overrideName})
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return wrapIfNotCoded(err, errs.Config)
	}

	fmt.Printf("added owner %s\n", effectiveName)
	// Surface gh problems now rather than only at sync time. Advisory only —
	// the entry is already saved and will expand once gh becomes available.
	if gherr := forge.Available(); gherr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n  owner expansion will be skipped until gh is available and authenticated.\n", gherr)
	}
	// Discover and fetch the owner's repos right away. Scoped to this owner,
	// runSync reconciles it (adding newly discovered repos) and syncs them.
	return runSync([]string{effectiveName}, syncDefaultJobs, 0, false)
}

func newRepoRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a repository: config entry, cache on disk, and its workspaces",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoRm(args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false,
		"delete even if a workspace has uncommitted or unpushed changes")
	return cmd
}

func runRepoRm(name string, force bool) error {
	// Resolve the name without mutating config yet, so we can run safety
	// checks on the workspaces before deleting anything. A name may refer to
	// a repo or an owner; resolve both and disambiguate.
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	owner, ownerErr := c.ResolveOwner(name)
	repo, repoErr := c.Resolve(name)
	switch {
	case ownerErr == nil && repoErr == nil:
		on, _ := owner.ResolvedName()
		rn, _ := repo.ResolvedName()
		return errs.New(errs.NotFound,
			"%q is ambiguous; matches owner %q and repo %q — use the full name", name, on, rn)
	case ownerErr == nil:
		return runOwnerRm(owner, force)
	case repoErr == nil:
		return runRepoRmOne(repo, force)
	default:
		return repoErr // the repo "not found" message is the friendlier default
	}
}

func runRepoRmOne(r *config.Repo, force bool) error {
	resolved, err := r.ResolvedName()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}

	workspaces, err := workspace.List([]string{resolved})
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if !force {
		if blocked := blockedWorkspaces(resolved, workspaces); len(blocked) > 0 {
			return errs.New(errs.Dirty,
				"refusing to remove %s; these workspaces have unsaved work:\n%s\ncommit and push, or pass --force to discard",
				resolved, strings.Join(blocked, "\n"))
		}
	}

	if err := removeRepoArtifacts(resolved); err != nil {
		return err
	}

	if err := removeConfigEntries(map[string]bool{resolved: true}, nil); err != nil {
		return err
	}

	fmt.Printf("removed %s (config", resolved)
	if len(workspaces) > 0 {
		fmt.Printf(", %s", pluralize(len(workspaces), "workspace"))
	}
	fmt.Println(", cache on disk)")
	return nil
}

// runOwnerRm removes an owner entry and every repo it auto-added (Source ==
// owner), with the same workspace-safety guarantees as a single repo: it
// refuses up front (listing all offenders) if any managed workspace has
// unsaved work and --force wasn't given.
func runOwnerRm(o *config.Owner, force bool) error {
	ownerName, err := o.ResolvedName()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	managed := c.ReposForOwner(ownerName)

	workspaces, err := workspace.List(managed)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if !force {
		if blocked := blockedWorkspaces("", workspaces); len(blocked) > 0 {
			return errs.New(errs.Dirty,
				"refusing to remove owner %s; these workspaces have unsaved work:\n%s\ncommit and push, or pass --force to discard",
				ownerName, strings.Join(blocked, "\n"))
		}
	}

	// Remove on-disk artifacts for each managed repo first, then drop the
	// owner entry plus all its repo entries from config in one transaction.
	for _, resolved := range managed {
		if err := removeRepoArtifacts(resolved); err != nil {
			return err
		}
	}
	removeRepos := make(map[string]bool, len(managed))
	for _, n := range managed {
		removeRepos[n] = true
	}
	if err := removeConfigEntries(removeRepos, map[string]bool{ownerName: true}); err != nil {
		return err
	}

	fmt.Printf("removed owner %s (config", ownerName)
	if len(managed) > 0 {
		fmt.Printf(", %s", pluralize(len(managed), "repo"))
	}
	if len(workspaces) > 0 {
		fmt.Printf(", %s", pluralize(len(workspaces), "workspace"))
	}
	fmt.Println(", caches on disk)")
	return nil
}

// blockedWorkspaces returns one "  <branch>: <reasons>" line per workspace
// that has uncommitted or unpushed work (the reason --force exists).
func blockedWorkspaces(_ string, workspaces []workspace.Info) []string {
	var blocked []string
	for _, ws := range workspaces {
		parts := []string{}
		if ws.Dirty {
			parts = append(parts, "uncommitted changes")
		}
		if ws.Unpushed > 0 {
			parts = append(parts, fmt.Sprintf("%d unpushed commits", ws.Unpushed))
		}
		if len(parts) > 0 {
			blocked = append(blocked, fmt.Sprintf("  %s: %s", ws.Branch, joinAnd(parts)))
		}
	}
	return blocked
}

// removeRepoArtifacts deletes a repo's workspaces and cache from disk (but not
// its config entry). On-disk artifacts are removed before config so a failure
// partway through leaves the entry as a record of remaining cleanup.
func removeRepoArtifacts(resolved string) error {
	if err := workspace.RemoveAllForRepo(resolved); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if err := cache.Remove(resolved, configLockTimeout); err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return errs.Wrap(errs.Config, err)
	}
	return nil
}

// removeConfigEntries drops the named repos and owners from config in a single
// locked transaction.
func removeConfigEntries(repos, owners map[string]bool) error {
	err := config.WithLock(configLockTimeout, func(c *config.Config) error {
		kept := c.Repos[:0]
		for _, r := range c.Repos {
			n, _ := r.ResolvedName()
			if repos[n] {
				continue
			}
			kept = append(kept, r)
		}
		c.Repos = kept
		keptOwners := c.Owners[:0]
		for _, o := range c.Owners {
			n, _ := o.ResolvedName()
			if owners[n] {
				continue
			}
			keptOwners = append(keptOwners, o)
		}
		c.Owners = keptOwners
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return wrapIfNotCoded(err, errs.Config)
	}
	return nil
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func newRepoListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tracked repos with last sync and source",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoList(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

type repoRow struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Source     string `json:"source,omitempty"`
	Path       string `json:"path,omitempty"`
	LastSyncAt any    `json:"last_sync_at"`
}

type ownerRow struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	RepoCount int    `json:"repo_count"`
}

func runRepoList(jsonOut bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	rows, owners, err := collectRepoList(c)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Repos  []repoRow  `json:"repos"`
			Owners []ownerRow `json:"owners"`
		}{rows, owners})
	}
	return writeRepoTable(os.Stdout, rows, owners)
}

// collectRepoList gathers the rows behind `repo list`, probing the cache for
// each tracked repo's last-sync time. The probes are deliberately cheap (a
// stat and a small metadata read, no size walk or git subprocess) so this is
// safe to run on the session-context hot path via repoListText.
func collectRepoList(c *config.Config) ([]repoRow, []ownerRow, error) {
	rows := make([]repoRow, 0, len(c.Repos))
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			return nil, nil, errs.Wrap(errs.Config, err)
		}
		row := repoRow{Name: name, URL: r.URL, Source: r.Source, LastSyncAt: nil}
		if cache.Exists(name) {
			row.Path = paths.CacheRepoPath(name)
			if meta, err := cache.LoadMeta(name); err == nil && meta != nil {
				row.LastSyncAt = meta.LastSyncAt.UTC().Format(time.RFC3339)
			}
		}
		rows = append(rows, row)
	}
	owners := make([]ownerRow, 0, len(c.Owners))
	for _, o := range c.Owners {
		name, err := o.ResolvedName()
		if err != nil {
			return nil, nil, errs.Wrap(errs.Config, err)
		}
		owners = append(owners, ownerRow{Name: name, URL: o.URL, RepoCount: len(c.ReposForOwner(name))})
	}
	return rows, owners, nil
}

// writeRepoTable renders the human-readable `repo list` table to out.
func writeRepoTable(out io.Writer, rows []repoRow, owners []ownerRow) error {
	if len(rows) == 0 && len(owners) == 0 {
		fmt.Fprintln(out, "(no repos tracked; add with `repocache repo add <url>`)")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if len(owners) > 0 {
		fmt.Fprintln(w, "OWNER\tREPOS")
		for _, o := range owners {
			fmt.Fprintf(w, "%s\t%d\n", o.Name, o.RepoCount)
		}
		fmt.Fprintln(w, "")
	}
	fmt.Fprintln(w, "NAME\tLAST SYNC\tSOURCE")
	for _, r := range rows {
		last := "never"
		if ts, ok := r.LastSyncAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				last = relTime(t)
			}
		}
		source := "—"
		if r.Source != "" {
			source = r.Source
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, last, source)
	}
	return w.Flush()
}

// repoListText renders the `repo list` table to a string for embedding in
// session context. Best-effort: returns "" if the library can't be read, so
// a config hiccup never breaks session startup.
func repoListText() string {
	c, err := config.Load()
	if err != nil {
		return ""
	}
	rows, owners, err := collectRepoList(c)
	if err != nil {
		return ""
	}
	if len(rows) == 0 && len(owners) == 0 {
		return "" // nothing tracked yet; the guide already covers adding repos
	}
	var buf bytes.Buffer
	if err := writeRepoTable(&buf, rows, owners); err != nil {
		return ""
	}
	return buf.String()
}

// wrapIfNotCoded ensures we propagate exit codes; if err is already a
// *errs.Coded we return it unchanged.
func wrapIfNotCoded(err error, code int) error {
	var c *errs.Coded
	if errors.As(err, &c) {
		return err
	}
	return errs.Wrap(code, err)
}

func humanSize(b int64) string {
	const k = 1024.0
	switch {
	case b < int64(k):
		return fmt.Sprintf("%d B", b)
	case b < int64(k*k):
		return fmt.Sprintf("%.1f KB", float64(b)/k)
	case b < int64(k*k*k):
		return fmt.Sprintf("%.1f MB", float64(b)/(k*k))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(k*k*k))
	}
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
