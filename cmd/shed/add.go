package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/forge"
	"github.com/AndrewHannigan/shed/pkg/paths"
)

// configLockTimeout bounds how long a config-mutating command waits for the
// config (and per-repo cache) lock before giving up. Shared by add, rm, and
// sync.
const configLockTimeout = 2 * time.Second

func newAddCmd() *cobra.Command {
	var name string
	var asOwner, asRepo bool
	cmd := &cobra.Command{
		Use:   "add <repo>",
		Short: "Add a repository, or a whole user/org, to the library",
		Long: `add appends a repo to the library. <repo> may be a full git URL
(https://, ssh://, or scp-style git@host:owner/repo) or GitHub shorthand:
a bare "owner/repo" or "owner" is expanded against github.com, so
"shed add octocat/Hello-World" and "shed add octocat"
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
		return errs.EnsureCoded(err, errs.Config)
	}
	fmt.Printf("added %s\n", effectiveName)
	// Fetch the new repo right away so the cache is populated without a
	// separate `shed sync`. Scoped to just this repo.
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
		return errs.EnsureCoded(err, errs.Config)
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
