package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/cache"
	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/forge"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func newPruneCmd() *cobra.Command {
	var dryRun, force, yes bool
	var maxAge time.Duration
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete workspaces whose work has already landed",
		Long: `prune removes every workspace whose work has already landed, reclaiming
the ones that are safe to delete. A workspace is reclaimed when its branch
has a merged pull request, or its commits are already contained in the
remote default branch (a merge- or rebase-merge with no PR). The merged-PR
check asks GitHub via the gh CLI, so gh must be installed and authenticated.

With --max-age, also reclaim workspaces whose last activity (newest reflog
entry) is older than the given duration, regardless of merge status.

Workspaces with uncommitted or unpushed changes are skipped so local work is
never lost; pass --force to remove them anyway. Before deleting, prune lists
the workspaces and asks for confirmation; pass --yes to skip the prompt or
--dry-run to preview without deleting.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(dryRun, force, yes, maxAge)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "remove even if there are uncommitted or unpushed changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().DurationVar(&maxAge, "max-age", 0, "also remove workspaces inactive longer than this (e.g. 720h)")
	return cmd
}

// prunePlan is a workspace prune has decided to delete, with the reason for it.
type prunePlan struct {
	info   workspace.Info
	reason string
}

func runPrune(dryRun, force, yes bool, maxAge time.Duration) error {
	if err := cache.RequireGit(); err != nil {
		return errs.Wrap(errs.MissingDep, err)
	}
	// prune leans on gh for the merged-PR check, so fail fast (rather than
	// degrade) when gh can't tell us which branches are merged.
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

	now := time.Now()
	var plans []prunePlan
	var skipped, kept, failed int
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
		// Only consult git when there's no merged PR: a found PR is the
		// stronger signal and gives the clearer message, and the ancestor
		// check is redundant once we know it merged.
		var landed bool
		var defaultBranch string
		if pr == 0 {
			landed, defaultBranch, err = workspace.LandedInDefault(i.Path, i.Branch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s %s: could not check default-branch status: %v\n", repo, i.Branch, err)
			}
		}
		expired := maxAge > 0 && !i.Age.IsZero() && now.Sub(i.Age) > maxAge
		prunable := pr != 0 || landed || expired
		reason := pruneReason(pr, landed, defaultBranch, expired, now.Sub(i.Age))
		switch decidePrune(prunable, i.Dirty, i.Unpushed, force) {
		case pruneKeep:
			kept++
		case pruneSkip:
			fmt.Printf("skipped %s (%s, but has %s)\n", i.Branch, reason, localChangesDesc(i))
			skipped++
		case pruneRemove:
			plans = append(plans, prunePlan{info: i, reason: reason})
		}
	}

	pruned := 0
	switch {
	case dryRun:
		for _, p := range plans {
			fmt.Printf("would prune %s (%s)\n", p.info.Branch, p.reason)
		}
		pruned = len(plans)
	case len(plans) == 0:
		// nothing to delete
	default:
		fmt.Printf("The following %s will be deleted:\n", countNoun(len(plans), "workspace"))
		for _, p := range plans {
			fmt.Printf("  %s (%s)\n", p.info.Branch, p.reason)
		}
		if !yes && !confirmDeletion() {
			fmt.Println("aborted")
			kept += len(plans)
			return nil
		}
		for _, p := range plans {
			if err := workspace.Remove(p.info.Name, p.info.Branch); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", p.info.Branch, err)
				failed++
				continue
			}
			fmt.Printf("pruned %s (%s)\n", p.info.Branch, p.reason)
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

// pruneAction is what prune decides to do with one workspace.
type pruneAction int

const (
	pruneKeep   pruneAction = iota // not reclaimable — leave it alone
	pruneSkip                      // reclaimable, but has local work and --force wasn't given
	pruneRemove                    // reclaimable and safe to delete
)

// decidePrune chooses an action for a workspace. prunable is true when some
// condition (merged PR, landed in the default branch, or age) marks the
// workspace as reclaimable. dirty flags uncommitted changes; unpushed is the
// unpushed-commit count (-1 when no upstream, treated as "nothing unpushed"
// since a landed branch reached the remote). Pure, so it is unit-testable.
func decidePrune(prunable, dirty bool, unpushed int, force bool) pruneAction {
	if !prunable {
		return pruneKeep
	}
	if !force && (dirty || unpushed > 0) {
		return pruneSkip
	}
	return pruneRemove
}

// pruneReason describes why a workspace is being pruned, for status messages.
// Reasons are reported in priority order: a merged PR is the clearest signal,
// then containment in the default branch, then age.
func pruneReason(prNumber int, landed bool, defaultBranch string, expired bool, inactive time.Duration) string {
	switch {
	case prNumber != 0:
		return fmt.Sprintf("PR #%d merged", prNumber)
	case landed:
		if defaultBranch != "" {
			return fmt.Sprintf("merged into %s", defaultBranch)
		}
		return "merged into default branch"
	case expired:
		return fmt.Sprintf("inactive for %s", relDuration(inactive))
	default:
		return ""
	}
}

// confirmDeletion prompts on stderr for a yes/no before prune deletes. When
// stdin isn't a TTY it refuses rather than delete unattended, pointing at the
// --yes / --dry-run escape hatches.
func confirmDeletion() bool {
	if !stdinIsTTY() {
		fmt.Fprintln(os.Stderr, "refusing to delete without confirmation; re-run with --yes (or --dry-run to preview)")
		return false
	}
	fmt.Fprint(os.Stderr, "Delete these workspaces? [y/N] ")
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// countNoun renders "1 workspace" / "3 workspaces" for human-readable counts.
func countNoun(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
