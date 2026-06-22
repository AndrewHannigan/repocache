package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/forge"
	"github.com/AndrewHannigan/repocache/pkg/paths"
	"github.com/AndrewHannigan/repocache/pkg/workspace"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Manage writable workspaces derived from cache repos",
	}
	cmd.AddCommand(
		newWorkspaceNewCmd(),
		newWorkspaceListCmd(),
		newWorkspacePathCmd(),
		newWorkspaceRmCmd(),
		newWorkspaceGcCmd(),
	)
	return cmd
}

func newWorkspaceNewCmd() *cobra.Command {
	var base string
	cmd := &cobra.Command{
		Use:   "new <repo> <branch>",
		Short: "Create a workspace via `git clone --reference`",
		Long: `new creates a writable clone of the cache repo at
~/.repocache/workspaces/<repo>/<branch>/ using
'git clone --reference' so it shares object storage with the cache.

If <branch> exists on origin, checks it out. Otherwise creates it off
origin/HEAD (or --base). Prints the workspace path on stdout.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceNew(args[0], args[1], base)
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "branch to fork from when <branch> is new (default: origin/HEAD)")
	return cmd
}

func runWorkspaceNew(name, branch, base string) error {
	if err := cache.RequireGit(); err != nil {
		return errs.Wrap(errs.MissingDep, err)
	}
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	repo, err := c.Resolve(name)
	if err != nil {
		return err
	}
	name, err = repo.ResolvedName()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if workspace.Exists(name, branch) {
		return errs.New(errs.Exists, "workspace already exists at %s", workspace.PathFor(name, branch))
	}
	// Refresh the cache first so the workspace forks from up-to-date code.
	// syncOne clones the repo if it isn't cached yet. If the sync fails but a
	// cache already exists, fall back to it (so `new` still works offline);
	// only hard-fail when there is nothing cached to fork from.
	fmt.Fprintf(os.Stderr, "syncing %s...\n", name)
	if res := syncOne(name, repo.URL, 0); res.Status == "error" {
		if !cache.Exists(name) {
			if res.locked {
				return errs.New(errs.Locked, "could not sync %s: %s", name, res.Error)
			}
			return errs.New(errs.Network, "could not sync %s: %s", name, res.Error)
		}
		fmt.Fprintf(os.Stderr, "warning: could not refresh %s (%s); using existing cache\n", name, res.Error)
	}
	path, err := workspace.New(name, branch, base, repo.URL)
	if err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return errs.Wrap(errs.Network, err)
	}
	fmt.Println(path)
	return nil
}

// resolveWorkspaceName maps a possibly-shorthand repo name to the name a
// workspace lives under on disk, so `path` and `rm` accept the same shorthand
// as `new` (e.g. "repocache" → "github.com/AndrewHannigan/repocache").
//
// It prefers a workspace that already exists under the name as given — so
// exact/full names, and workspaces whose repo is no longer in the config,
// still resolve — and only falls back to config resolution otherwise. On any
// failure it returns the name unchanged, letting the caller surface its normal
// "no workspace at <path>" not-found error.
func resolveWorkspaceName(name, branch string) string {
	if workspace.Exists(name, branch) {
		return name
	}
	c, err := config.Load()
	if err != nil {
		return name
	}
	if full, ok := resolveRepoName(c, name); ok {
		return full
	}
	return name
}

// resolveRepoName resolves name to a repo's full canonical name via the
// config, the same rule `new` uses. ok is false when name doesn't resolve to
// exactly one repo. Pure (takes the config), so it is unit-testable.
func resolveRepoName(c *config.Config, name string) (string, bool) {
	repo, err := c.Resolve(name)
	if err != nil {
		return "", false
	}
	full, err := repo.ResolvedName()
	if err != nil {
		return "", false
	}
	return full, true
}

func newWorkspaceListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspaces with dirty/unpushed state and age",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceList(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

func runWorkspaceList(jsonOut bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	names := make([]string, 0, len(c.Repos))
	for _, r := range c.Repos {
		if n, err := r.ResolvedName(); err == nil {
			names = append(names, n)
		}
	}
	infos, err := workspace.List(names)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(infos)
	}
	if len(infos) == 0 {
		fmt.Println("(no workspaces)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tBRANCH\tDIRTY\tUNPUSHED\tAGE\tPATH")
	for _, i := range infos {
		dirty := "no"
		if i.Dirty {
			dirty = "yes"
		}
		unpushed := "—"
		if i.Unpushed >= 0 {
			unpushed = fmt.Sprintf("%d", i.Unpushed)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			i.Name, i.Branch, dirty, unpushed, relTime(i.Age), paths.Display(i.Path))
	}
	return w.Flush()
}

func newWorkspacePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <repo> <branch>",
		Short: "Print the absolute workspace path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := args[1]
			name := resolveWorkspaceName(args[0], branch)
			if !workspace.Exists(name, branch) {
				return errs.New(errs.NotFound, "no workspace at %s", workspace.PathFor(name, branch))
			}
			fmt.Println(workspace.PathFor(name, branch))
			return nil
		},
	}
}

func newWorkspaceRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <repo> <branch>",
		Short: "Delete a workspace (refuses if dirty or unpushed unless --force)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceRm(args[0], args[1], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete even if there are uncommitted or unpushed changes")
	return cmd
}

func runWorkspaceRm(name, branch string, force bool) error {
	name = resolveWorkspaceName(name, branch)
	if !workspace.Exists(name, branch) {
		return errs.New(errs.NotFound, "no workspace at %s", workspace.PathFor(name, branch))
	}
	path := workspace.PathFor(name, branch)
	if !force {
		dirty, unpushed, err := workspace.CheckClean(path)
		if err != nil {
			return errs.Wrap(errs.Config, err)
		}
		if dirty || unpushed > 0 {
			parts := []string{}
			if dirty {
				parts = append(parts, "uncommitted changes")
			}
			if unpushed > 0 {
				parts = append(parts, fmt.Sprintf("%d unpushed commits", unpushed))
			}
			return errs.New(errs.Dirty,
				"workspace has %s; commit and push, or pass --force to discard",
				joinAnd(parts))
		}
	}
	if err := workspace.Remove(name, branch); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	fmt.Printf("removed %s\n", paths.Display(path))
	return nil
}

func newWorkspaceGcCmd() *cobra.Command {
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Delete workspaces whose branch has a merged PR",
		Long: `gc removes every workspace whose branch has a merged pull request,
reclaiming the ones whose work has already landed. It asks GitHub which
branches are merged via the gh CLI, so gh must be installed and authenticated.

Workspaces with uncommitted or unpushed changes are skipped so local work is
never lost; pass --force to remove them anyway. Use --dry-run to preview.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceGc(dryRun, force)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "remove even if there are uncommitted or unpushed changes")
	return cmd
}

func runWorkspaceGc(dryRun, force bool) error {
	if err := cache.RequireGit(); err != nil {
		return errs.Wrap(errs.MissingDep, err)
	}
	// gc is entirely gh-driven, so fail fast (rather than degrade) when gh
	// can't tell us which branches are merged.
	if err := forge.Available(); err != nil {
		if errors.Is(err, forge.ErrGhMissing) {
			return errs.Wrap(errs.MissingDep, err)
		}
		return errs.Wrap(errs.Network, err)
	}
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	names := make([]string, 0, len(c.Repos))
	for _, r := range c.Repos {
		if n, err := r.ResolvedName(); err == nil {
			names = append(names, n)
		}
	}
	infos, err := workspace.List(names)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if len(infos) == 0 {
		fmt.Println("(no workspaces)")
		return nil
	}

	var pruned, skipped, kept, failed int
	for _, i := range infos {
		host, repo, ok := ghRepoFromName(i.Name)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: cannot derive a GitHub repo from %q\n", i.Branch, i.Name)
			failed++
			continue
		}
		pr, err := forge.MergedPR(host, repo, i.Branch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s %s: could not check PR status: %v\n", repo, i.Branch, err)
			failed++
			continue
		}
		switch decideGc(pr, i.Dirty, i.Unpushed, force) {
		case gcKeep:
			kept++
		case gcSkip:
			fmt.Printf("skipped %s (PR #%d merged, but has %s)\n", i.Branch, pr, localChangesDesc(i))
			skipped++
		case gcPrune:
			if dryRun {
				fmt.Printf("would prune %s (PR #%d merged)\n", i.Branch, pr)
				pruned++
				continue
			}
			if err := workspace.Remove(i.Name, i.Branch); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", i.Branch, err)
				failed++
				continue
			}
			fmt.Printf("pruned %s (PR #%d merged)\n", i.Branch, pr)
			pruned++
		}
	}

	prunedLabel := "pruned"
	if dryRun {
		prunedLabel = "would prune"
	}
	parts := []string{fmt.Sprintf("%d %s", pruned, prunedLabel)}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	parts = append(parts, fmt.Sprintf("%d kept", kept))
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	fmt.Println(strings.Join(parts, ", "))
	return nil
}

// gcAction is what gc decides to do with one workspace.
type gcAction int

const (
	gcKeep  gcAction = iota // no merged PR — leave it alone
	gcSkip                  // merged, but has local work and --force wasn't given
	gcPrune                 // merged and safe to delete
)

// decideGc chooses an action for a workspace given its merged-PR number (0
// when none), dirty flag, and unpushed count (-1 when no upstream, treated as
// "nothing unpushed" since a merged PR means the branch reached the remote).
// Pure, so it is unit-testable.
func decideGc(prNumber int, dirty bool, unpushed int, force bool) gcAction {
	if prNumber == 0 {
		return gcKeep
	}
	if !force && (dirty || unpushed > 0) {
		return gcSkip
	}
	return gcPrune
}

// ghRepoFromName splits a workspace repo name ("host/owner/repo") into the
// GitHub host and the "owner/repo" slug gh expects. ok is false unless the
// name has a host plus an owner/repo path. Pure, so it is unit-testable.
func ghRepoFromName(name string) (host, repo string, ok bool) {
	h, rest, found := strings.Cut(name, "/")
	if !found || h == "" || !strings.Contains(rest, "/") {
		return "", "", false
	}
	return h, rest, true
}

// localChangesDesc describes a workspace's uncommitted/unpushed state for the
// "skipped" message.
func localChangesDesc(i workspace.Info) string {
	parts := []string{}
	if i.Dirty {
		parts = append(parts, "uncommitted changes")
	}
	if i.Unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed commits", i.Unpushed))
	}
	return joinAnd(parts)
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return parts[0] + ", " + joinAnd(parts[1:])
	}
}
