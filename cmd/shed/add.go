package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/forge"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
)

// configLockTimeout bounds how long a config-mutating command waits for the
// config (and per-repo store) lock before giving up. Shared by add, rm, and
// sync.
const configLockTimeout = 2 * time.Second

func newAddCmd() *cobra.Command {
	var name string
	var asOwner, asRepo bool
	cmd := &cobra.Command{
		Use:   "add <repo>",
		Short: "Add a repository, or a whole user/org, to the library",
		Long: `add appends a repo to the library. <repo> may be a full git URL
(https://, ssh://, or scp-style git@host:owner/repo) or shorthand:
a bare "owner/repo" or "owner" is expanded against github.com, so
"shed add octocat/Hello-World" and "shed add octocat"
both just work.

Shorthand defaults to GitHub but falls back to GitLab: if "owner/repo"
isn't found on github.com, add resolves it against gitlab.com instead. To
pin a host explicitly, prefix it ("gitlab.com/owner/repo") or pass a full
URL.

If <repo> points at a bare user or org (a single path segment, e.g.
octocat or https://github.com/octocat), it is tracked as an owner instead:
each sync discovers that owner's repos and adds any new ones automatically
(via gh for GitHub, glab for a GitLab group).

Detection is automatic from the shape; force it with --owner / --repo.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoAdd(args[0], name, asOwner, asRepo)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "override the default name (derived from URL)")
	cmd.Flags().BoolVar(&asOwner, "owner", false, "track <repo> as a user/org/group (discover its repos via gh/glab)")
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
	return runRepoAddOne(input, url, overrideName)
}

// runRepoAddOne adds a single repo. input is the user's original argument (used
// to decide whether the GitHub→GitLab shorthand fallback applies); url is its
// normalized form.
func runRepoAddOne(input, url, overrideName string) error {
	// Bare shorthand defaults to GitHub. If the repo isn't there, fall back to
	// GitLab before we commit to a URL — so "shed add owner/repo" keeps the
	// shorthand and just resolves to wherever the repo actually lives.
	if paths.IsBareShorthand(input) {
		url = githubOrGitLabURL(input, url)
	}
	// Validate URL and derive default name up front so we fail before locking.
	defaultName, err := paths.DefaultName(url)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	// Preflight auth while the user is here interactively. If the chosen
	// transport can't authenticate but the other one can, store the working
	// URL instead — this is the common "shorthand expands to HTTPS but I only
	// have SSH set up" case. The name is transport-independent, so swapping the
	// URL never changes effectiveName below.
	url = reachableURL(url)
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
	// Fetch the new repo right away so the store is populated without a
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
	// Surface forge-CLI problems now rather than only at sync time. Advisory
	// only — the entry is already saved and will expand once the CLI becomes
	// available. Which CLI (gh vs glab) depends on the owner's host.
	host, _, _ := paths.ParseURL(url)
	if ferr := forge.Available(host); ferr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n  owner expansion will be skipped until the forge CLI is available and authenticated.\n", ferr)
	}
	// Discover and fetch the owner's repos right away. Scoped to this owner,
	// runSync reconciles it (adding newly discovered repos) and syncs them.
	return runSync([]string{effectiveName}, syncDefaultJobs, 0, false)
}

// githubOrGitLabURL implements GitHub-first, GitLab-fallback resolution for
// bare shorthand. ghURL is input expanded against github.com. If git can reach
// it (public, or private with the user's credentials), GitHub wins — the
// documented default. If GitHub reports the repo simply isn't there, we probe
// the same owner/repo on gitlab.com and switch to it when it exists. Every
// other outcome — no git to probe with, a network blip, or an auth failure
// (the repo may well exist privately on GitHub) — keeps GitHub; a later
// `shed sync` surfaces any real problem with a transport-aware fix.
func githubOrGitLabURL(input, ghURL string) string {
	if repostore.RequireGit() != nil {
		return ghURL // no git to probe with; sync will report it.
	}
	err := repostore.Reachable(ghURL)
	if err == nil {
		return ghURL // found on GitHub
	}
	// Only a genuine "not found" is worth crossing to another host for. Auth or
	// network failures would not be fixed by switching forges, and a private
	// GitHub repo may exist even when the unauthenticated probe can't see it.
	if !isNotFoundError(strings.ToLower(err.Error())) {
		return ghURL
	}
	glURL := paths.ShorthandOnHost(input, "gitlab.com")
	if repostore.Reachable(glURL) == nil {
		fmt.Printf("note: %s was not found on GitHub; resolving to %s instead.\n", ghURL, glURL)
		return glURL
	}
	return ghURL
}

// reachableURL returns a clone URL that authenticates with the user's current
// setup, given the one they asked for. If url works as-is (the common case,
// including public repos that need no auth) it is returned unchanged. If url
// fails specifically on authentication and the other transport (SSH↔HTTPS)
// works, the working URL is returned with a note. Otherwise url is returned
// unchanged — add never blocks: the entry is saved regardless and the
// subsequent sync reports any remaining problem with a protocol-aware fix.
func reachableURL(url string) string {
	if repostore.RequireGit() != nil {
		return url // no git to probe with; sync will report it.
	}
	err := repostore.Reachable(url)
	if err == nil {
		return url
	}
	// Only a credential failure is worth switching transports for. Network or
	// not-found errors would fail the same way over either protocol.
	if !isAuthError(strings.ToLower(err.Error())) {
		return url
	}
	if alt := paths.AlternateProtocolURL(url); alt != "" && repostore.Reachable(alt) == nil {
		fmt.Printf("note: %s could not authenticate, but %s works — adding it over %s instead.\n",
			url, alt, transportLabel(alt))
		return alt
	}
	fmt.Fprintf(os.Stderr, "warning: could not authenticate to %s.\n  %s\n  Adding it anyway; fix auth and run `shed sync`.\n",
		url, authFixHint(url))
	return url
}

// transportLabel names the transport a URL uses, for human-facing notes.
func transportLabel(url string) string {
	if paths.IsSSHURL(url) {
		return "SSH"
	}
	return "HTTPS"
}
