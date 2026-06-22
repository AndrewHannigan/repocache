// Package forge discovers the repos belonging to an owner (a GitHub user or
// org) by shelling out to the `gh` CLI. This is shed's only runtime
// dependency beyond `git`, and it is used *only* for owner discovery — once a
// repo has been discovered it syncs with plain `git`, so callers degrade
// gracefully when `gh` is missing or unauthenticated (see the sentinel errors
// below).
package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// Sentinel errors callers use to decide how to degrade. ErrGhMissing means the
// gh binary is not installed; ErrGhUnauthed means it is installed but not
// logged in (so discovery, especially of private repos, cannot proceed).
var (
	ErrGhMissing  = errors.New("gh CLI not found on PATH (needed to expand owners)")
	ErrGhUnauthed = errors.New("gh CLI is not authenticated (run `gh auth login`)")
)

// defaultLimit caps how many repos we list per owner. gh defaults to 30, which
// silently truncates large orgs, so we ask for a generous ceiling instead.
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

// ghRepo mirrors the JSON object `gh repo list --json ...` emits.
type ghRepo struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	SSHURL     string `json:"sshUrl"`
	IsFork     bool   `json:"isFork"`
	IsArchived bool   `json:"isArchived"`
	Visibility string `json:"visibility"`
}

// Available reports whether gh is usable: installed and authenticated. It
// returns ErrGhMissing or ErrGhUnauthed so callers can warn precisely, or nil
// when gh is ready. Used by `add` to warn early; ListOwnerRepos does its
// own check so it never lists twice.
func Available() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return ErrGhMissing
	}
	if out, err := exec.Command("gh", "auth", "status").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", ErrGhUnauthed, strings.TrimSpace(string(out)))
	}
	return nil
}

// ListOwnerRepos lists the repos under ownerURL (e.g.
// "https://github.com/octocat") subject to f. On success it returns one
// entry per repo with a clone URL matching ownerURL's protocol. It returns
// ErrGhMissing / ErrGhUnauthed (wrapped) when gh can't be used, so the caller
// can skip this owner and continue syncing already-known repos.
func ListOwnerRepos(ownerURL string, f Filter) ([]Repo, error) {
	host, login, err := paths.ParseURL(ownerURL)
	if err != nil {
		return nil, err
	}
	if strings.Contains(login, "/") {
		return nil, fmt.Errorf("%q is a repo URL, not an owner URL", ownerURL)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, ErrGhMissing
	}

	cmd := exec.Command("gh", buildListArgs(login, f)...)
	// Target enterprise hosts by setting GH_HOST; github.com is gh's default.
	if host != "" && host != "github.com" {
		cmd.Env = append(os.Environ(), "GH_HOST="+host)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, classifyExecErr(err, stderr.String())
	}
	return decodeRepos(out, isSSHURL(ownerURL))
}

// buildListArgs builds the `gh repo list` argument vector for login under f.
// Kept pure (no exec) so it can be unit-tested.
func buildListArgs(login string, f Filter) []string {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	args := []string{
		"repo", "list", login,
		"--limit", strconv.Itoa(limit),
		"--json", "name,url,sshUrl,isFork,isArchived,visibility",
	}
	if !f.IncludeForks {
		args = append(args, "--source") // sources only (non-forks)
	}
	if !f.IncludeArchived {
		args = append(args, "--no-archived")
	}
	if v := strings.ToLower(f.Visibility); v != "" && v != "all" {
		args = append(args, "--visibility", v)
	}
	return args
}

// decodeRepos parses the JSON array gh emits and maps each entry to a Repo,
// selecting the ssh or https clone URL per wantSSH. Pure for testability.
func decodeRepos(data []byte, wantSSH bool) ([]Repo, error) {
	var ghRepos []ghRepo
	if err := json.Unmarshal(data, &ghRepos); err != nil {
		return nil, fmt.Errorf("parse gh repo list output: %w", err)
	}
	repos := make([]Repo, 0, len(ghRepos))
	for _, g := range ghRepos {
		cloneURL := g.URL
		if wantSSH && g.SSHURL != "" {
			cloneURL = g.SSHURL
		}
		repos = append(repos, Repo{
			Name:       g.Name,
			CloneURL:   cloneURL,
			IsFork:     g.IsFork,
			IsArchived: g.IsArchived,
			Visibility: g.Visibility,
		})
	}
	return repos, nil
}

// classifyExecErr turns a failed `gh repo list` into a sentinel where possible
// so callers can degrade. Pure for testability.
func classifyExecErr(err error, stderr string) error {
	if errors.Is(err, exec.ErrNotFound) {
		return ErrGhMissing
	}
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "not logged in"),
		strings.Contains(s, "gh auth login"),
		strings.Contains(s, "authentication"),
		strings.Contains(s, "requires authentication"):
		return fmt.Errorf("%w: %s", ErrGhUnauthed, strings.TrimSpace(stderr))
	default:
		return fmt.Errorf("gh repo list failed: %v: %s", err, strings.TrimSpace(stderr))
	}
}

// isSSHURL reports whether a git URL uses SSH (scp-style git@host:... or an
// ssh:// scheme), so discovered repos get matching clone URLs.
func isSSHURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "git@") || strings.HasPrefix(rawURL, "ssh://")
}
