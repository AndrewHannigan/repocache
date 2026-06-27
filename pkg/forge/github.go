package forge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// githubProvider talks to GitHub via the `gh` CLI. gh filters server-side, so
// the Filter is fully expressed in the argv and decodeRepos ignores it.
type githubProvider struct{}

func (githubProvider) cli() string              { return "gh" }
func (githubProvider) label() string            { return "GitHub" }
func (githubProvider) changeNoun() string       { return "PR" }
func (githubProvider) authStatusArgs() []string { return []string{"auth", "status"} }

// hostEnv targets a GitHub Enterprise host via GH_HOST; github.com is gh's
// default and needs no override.
func (githubProvider) hostEnv(host string) []string {
	if host == "" || host == "github.com" {
		return nil
	}
	return []string{"GH_HOST=" + host}
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

// listArgs builds the `gh repo list` argument vector for login under f.
func (githubProvider) listArgs(login string, f Filter) []string {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	args := []string{
		"repo", "list",
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
	// "--" terminates flags, then login positionally, so an owner that begins
	// with "-" can't be parsed as a gh flag (argument injection).
	return append(args, "--", login)
}

// decodeRepos parses the JSON array gh emits and maps each entry to a Repo,
// selecting the ssh or https clone URL per wantSSH. gh already filtered
// server-side, so f is unused here.
func (githubProvider) decodeRepos(data []byte, _ Filter, wantSSH bool) ([]Repo, error) {
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

// mergedArgs builds the `gh pr list` argv that asks for the single newest
// merged PR whose head branch is branch.
func (githubProvider) mergedArgs(repo, branch string) []string {
	// The "--flag=value" form binds each value to its flag, so a repo or branch
	// beginning with "-" can't be mistaken for a separate flag.
	return []string{
		"pr", "list",
		"--repo=" + repo,
		"--head=" + branch,
		"--state", "merged",
		"--json", "number",
		"--limit", "1",
	}
}

// decodeMerged parses the JSON array gh emits for `pr list` and returns the
// first PR's number, or 0 when the array is empty.
func (githubProvider) decodeMerged(data []byte) (int, error) {
	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(data, &prs); err != nil {
		return 0, fmt.Errorf("parse gh pr list output: %w", err)
	}
	if len(prs) == 0 {
		return 0, nil
	}
	return prs[0].Number, nil
}
