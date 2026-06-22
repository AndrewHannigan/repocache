package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/forge"
	"github.com/AndrewHannigan/repocache/pkg/workspace"
)

func newGcCmd() *cobra.Command {
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
			return runGc(dryRun, force)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "remove even if there are uncommitted or unpushed changes")
	return cmd
}

func runGc(dryRun, force bool) error {
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
