package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/repostore"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func newRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a repository: config entry, store on disk, and its workspaces",
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
		if blocked := blockedWorkspaces(workspaces); len(blocked) > 0 {
			return errs.New(errs.Dirty,
				"refusing to remove %s; these workspaces have unsaved work:\n%s\ncommit and push, or pass --force to discard",
				resolved, strings.Join(blocked, "\n"))
		}
	}

	if r.Source != "" {
		return rmOwnedRepo(r, resolved, force, workspaces)
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
	fmt.Println(", store on disk)")
	return nil
}

// rmOwnedRepo removes a repo that was auto-added by an owner. Instead of
// silently removing the config entry (which would be re-created on the next
// sync), it adds the repo to the owner's Exclude list so it stays gone.
func rmOwnedRepo(r *config.Repo, resolved string, force bool, workspaces []workspace.Info) error {
	if err := removeRepoArtifacts(resolved); err != nil {
		return err
	}

	ownerName := r.Source
	var lerr error
	if lerr = config.WithLock(configLockTimeout, func(c *config.Config) error {
		for i := range c.Owners {
			on, err := c.Owners[i].ResolvedName()
			if err != nil {
				continue
			}
			if on == ownerName {
				already := false
				for _, e := range c.Owners[i].Exclude {
					if e == resolved {
						already = true
						break
					}
				}
				if !already {
					c.Owners[i].Exclude = append(c.Owners[i].Exclude, resolved)
				}
				break
			}
		}
		kept := c.Repos[:0]
		for _, r := range c.Repos {
			n, _ := r.ResolvedName()
			if n == resolved {
				continue
			}
			kept = append(kept, r)
		}
		c.Repos = kept
		return config.Save(c)
	}); lerr != nil {
		if errors.Is(lerr, config.ErrLocked) {
			return errs.Wrap(errs.Locked, lerr)
		}
		return errs.EnsureCoded(lerr, errs.Config)
	}

	fmt.Printf("removed %s (config", resolved)
	if len(workspaces) > 0 {
		fmt.Printf(", %s", pluralize(len(workspaces), "workspace"))
	}
	fmt.Println(", store on disk)")
	fmt.Printf("  note: %s was auto-added by owner %s — it has been added\n", resolved, ownerName)
	fmt.Printf("        to that owner's exclude list so it won't be re-added on sync.\n")
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
		if blocked := blockedWorkspaces(workspaces); len(blocked) > 0 {
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
	fmt.Println(", stores on disk)")
	return nil
}

// blockedWorkspaces returns one "  <branch>: <reasons>" line per workspace
// that has uncommitted or unpushed work (the reason --force exists).
func blockedWorkspaces(workspaces []workspace.Info) []string {
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

// removeRepoArtifacts deletes a repo's workspaces and store from disk (but not
// its config entry). On-disk artifacts are removed before config so a failure
// partway through leaves the entry as a record of remaining cleanup.
func removeRepoArtifacts(resolved string) error {
	if err := workspace.RemoveAllForRepo(resolved); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if err := repostore.Remove(resolved, configLockTimeout); err != nil {
		if errors.Is(err, repostore.ErrLocked) {
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
		return errs.EnsureCoded(err, errs.Config)
	}
	return nil
}
