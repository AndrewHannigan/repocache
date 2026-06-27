// Package forge talks to a git hosting service ("forge") by shelling out to
// its CLI — `gh` for GitHub, `glab` for GitLab. It does two things shed can't
// do with plain git: discover the repos belonging to an owner (a user, org, or
// group) and report whether a branch has a merged change request (a GitHub pull
// request or a GitLab merge request). This is shed's only runtime dependency
// beyond `git` — everything else syncs with plain `git`, so callers degrade
// gracefully when the CLI is missing or unauthenticated (see the sentinel
// errors below).
//
// Which CLI runs is chosen by host: github.com (and unknown hosts) use `gh`;
// gitlab.com and any host whose name contains "gitlab" use `glab`. That keeps
// every other part of shed — the store, sync, workspaces, the
// <host>/<owner>/<repo> layout — host-agnostic; the forge is the one seam where
// GitHub and GitLab differ.
package forge

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// Sentinel errors callers use to decide how to degrade. ErrCLIMissing means the
// forge CLI is not installed; ErrCLIUnauthed means it is installed but not
// logged in (so discovery, especially of private repos, cannot proceed). The
// wrapped message names the specific CLI (`gh` vs `glab`).
var (
	ErrCLIMissing  = errors.New("forge CLI not found")
	ErrCLIUnauthed = errors.New("forge CLI is not authenticated")
)

// defaultLimit caps how many repos we list per owner. Both CLIs default to 30,
// which silently truncates large orgs/groups, so we ask for a generous ceiling.
const defaultLimit = 1000

// Filter controls which of an owner's repos discovery returns. The zero value
// means: exclude forks, exclude archived, all visibilities.
type Filter struct {
	IncludeForks    bool
	IncludeArchived bool
	Visibility      string // "", "all", "public", or "private"
	Limit           int    // <= 0 means defaultLimit
}

// Repo is one repo discovered under an owner.
type Repo struct {
	Name       string // short name, no owner prefix
	CloneURL   string // chosen to match the owner URL's protocol (https vs ssh)
	IsFork     bool
	IsArchived bool
	Visibility string
}

// provider is one forge's CLI integration. The arg builders and decoders are
// kept pure (no exec) so they unit-test without the CLI installed; exec lives
// only in the dispatchers below.
type provider interface {
	cli() string              // binary name on PATH, e.g. "gh" / "glab"
	label() string            // human name, e.g. "GitHub" / "GitLab"
	changeNoun() string       // "PR" / "MR" — for human-facing messages
	authStatusArgs() []string // args that check auth, e.g. {"auth","status"}
	// hostEnv returns environment overrides to target host, or nil for the
	// CLI's default host. Empty host means "default".
	hostEnv(host string) []string
	// listArgs builds the repo-list argv for login under f.
	listArgs(login string, f Filter) []string
	// decodeRepos parses the CLI's repo-list JSON. f is passed so a CLI that
	// can't filter server-side (glab) can filter client-side here; gh ignores
	// it. wantSSH selects the ssh vs https clone URL.
	decodeRepos(data []byte, f Filter, wantSSH bool) ([]Repo, error)
	// mergedArgs builds the argv that asks for the single newest merged change
	// (PR/MR) whose source branch is branch in repo (an "owner/name" slug).
	mergedArgs(repo, branch string) []string
	// decodeMerged parses that output and returns the change number, or 0.
	decodeMerged(data []byte) (int, error)
}

// providerFor selects the forge integration for a host. github.com and any
// host we don't recognize use gh (GitHub Enterprise is reached with gh too, via
// GH_HOST); gitlab.com and "gitlab"-containing hosts use glab.
func providerFor(host string) provider {
	if isGitLabHost(host) {
		return gitlabProvider{}
	}
	return githubProvider{}
}

// isGitLabHost reports whether host should be served by glab. We match
// gitlab.com exactly and, for self-managed installs, any host whose name
// contains "gitlab" (e.g. gitlab.example.com). Everything else falls to gh.
func isGitLabHost(host string) bool {
	h := strings.ToLower(host)
	return h == "gitlab.com" || strings.Contains(h, "gitlab")
}

// ChangeNoun returns the host's word for a merged change — "PR" for GitHub,
// "MR" for GitLab — so callers can phrase messages correctly.
func ChangeNoun(host string) string { return providerFor(host).changeNoun() }

// Available reports whether the forge CLI for host is usable: installed and
// authenticated. It returns ErrCLIMissing or ErrCLIUnauthed (wrapped, with a
// CLI-specific message) so callers can warn precisely, or nil when ready.
func Available(host string) error {
	p := providerFor(host)
	if _, err := exec.LookPath(p.cli()); err != nil {
		return missingErr(p)
	}
	if out, err := exec.Command(p.cli(), p.authStatusArgs()...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", unauthedErr(p), strings.TrimSpace(string(out)))
	}
	return nil
}

// ListOwnerRepos lists the repos under ownerURL (e.g.
// "https://github.com/octocat" or "https://gitlab.com/mygroup") subject to f.
// On success it returns one entry per repo with a clone URL matching ownerURL's
// protocol. It returns ErrCLIMissing / ErrCLIUnauthed (wrapped) when the CLI
// can't be used, so the caller can skip this owner and continue syncing
// already-known repos.
func ListOwnerRepos(ownerURL string, f Filter) ([]Repo, error) {
	host, login, err := paths.ParseURL(ownerURL)
	if err != nil {
		return nil, err
	}
	p := providerFor(host)
	if _, err := exec.LookPath(p.cli()); err != nil {
		return nil, missingErr(p)
	}

	cmd := exec.Command(p.cli(), p.listArgs(login, f)...)
	if env := p.hostEnv(host); env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, classifyExecErr(p, p.cli()+" repo list", err, stderr.String())
	}
	return p.decodeRepos(out, f, isSSHURL(ownerURL))
}

// MergedChange returns the number of a merged change request (a GitHub PR or a
// GitLab MR) whose source branch is branch in repo (an "owner/name" slug), or 0
// if there is none. host selects the forge. Like ListOwnerRepos it returns
// ErrCLIMissing / ErrCLIUnauthed (wrapped) when the CLI can't be used.
func MergedChange(host, repo, branch string) (int, error) {
	p := providerFor(host)
	if _, err := exec.LookPath(p.cli()); err != nil {
		return 0, missingErr(p)
	}
	cmd := exec.Command(p.cli(), p.mergedArgs(repo, branch)...)
	if env := p.hostEnv(host); env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, classifyExecErr(p, p.cli()+" "+p.changeNoun()+" list", err, stderr.String())
	}
	return p.decodeMerged(out)
}

// missingErr / unauthedErr wrap the sentinels with a CLI-specific message.
func missingErr(p provider) error {
	return fmt.Errorf("%w: %s CLI not found on PATH (needed to expand owners on %s)",
		ErrCLIMissing, p.cli(), p.label())
}

func unauthedErr(p provider) error {
	return fmt.Errorf("%w: %s CLI is not authenticated (run `%s auth login`)",
		ErrCLIUnauthed, p.cli(), p.cli())
}

// classifyExecErr turns a failed CLI invocation (what names it, e.g.
// "gh repo list") into a sentinel where possible so callers can degrade.
// Pure for testability.
func classifyExecErr(p provider, what string, err error, stderr string) error {
	if errors.Is(err, exec.ErrNotFound) {
		return missingErr(p)
	}
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "not logged in"),
		strings.Contains(s, "auth login"),
		strings.Contains(s, "authentication"),
		strings.Contains(s, "requires authentication"),
		strings.Contains(s, "401 unauthorized"):
		return fmt.Errorf("%w: %s", unauthedErr(p), strings.TrimSpace(stderr))
	default:
		return fmt.Errorf("%s failed: %v: %s", what, err, strings.TrimSpace(stderr))
	}
}

// isSSHURL reports whether a git URL uses SSH (scp-style git@host:... or an
// ssh:// scheme), so discovered repos get matching clone URLs.
func isSSHURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "git@") || strings.HasPrefix(rawURL, "ssh://")
}
