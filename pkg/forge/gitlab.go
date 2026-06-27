package forge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// gitlabProvider talks to GitLab via the `glab` CLI. Unlike gh, glab's
// `repo list` has no server-side flags for visibility, forks, or excluding
// archived repos, so we ask for the group's projects as JSON and apply the
// Filter client-side in decodeRepos.
//
// Owner discovery targets a GitLab *group* (`glab repo list --group <login>`).
// GitLab's other top-level namespace kind — a personal user namespace — has no
// equivalent glab flag, so tracking a user (rather than a group) as an owner is
// a GitHub-only capability for now; glab returns an error we surface and skip.
type gitlabProvider struct{}

func (gitlabProvider) cli() string              { return "glab" }
func (gitlabProvider) label() string            { return "GitLab" }
func (gitlabProvider) changeNoun() string       { return "MR" }
func (gitlabProvider) authStatusArgs() []string { return []string{"auth", "status"} }

// hostEnv targets a self-managed GitLab host via GITLAB_HOST; gitlab.com is
// glab's default and needs no override.
func (gitlabProvider) hostEnv(host string) []string {
	if host == "" || host == "gitlab.com" {
		return nil
	}
	return []string{"GITLAB_HOST=" + host}
}

// glRepo mirrors the subset of GitLab's project JSON that `glab repo list
// --output json` passes through. path is the repo slug (no namespace);
// http/ssh URLs are the clone URLs; forked_from_project is present only when
// the project is a fork (and only when GitLab includes it — see IsFork below).
type glRepo struct {
	Path       string `json:"path"`
	HTTPURL    string `json:"http_url_to_repo"`
	SSHURL     string `json:"ssh_url_to_repo"`
	Archived   bool   `json:"archived"`
	Visibility string `json:"visibility"`
	ForkedFrom *struct {
		ID int `json:"id"`
	} `json:"forked_from_project"`
}

// listArgs builds the `glab repo list` argv for a group. All filtering happens
// client-side (decodeRepos), so the only knobs here are the group and the page
// size. GitLab caps per_page at 100, which is the practical ceiling per group.
func (gitlabProvider) listArgs(login string, f Filter) []string {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	return []string{
		"repo", "list",
		"--group", login,
		"--per-page", strconv.Itoa(limit),
		"--output", "json",
	}
}

// decodeRepos parses glab's project JSON and applies the Filter client-side,
// since glab can't express it in the argv. Fork filtering is best-effort:
// GitLab omits forked_from_project from list responses unless asked, so a fork
// may slip through; we honor it when present.
func (gitlabProvider) decodeRepos(data []byte, f Filter, wantSSH bool) ([]Repo, error) {
	var gl []glRepo
	if err := json.Unmarshal(data, &gl); err != nil {
		return nil, fmt.Errorf("parse glab repo list output: %w", err)
	}
	wantVis := strings.ToLower(f.Visibility)
	repos := make([]Repo, 0, len(gl))
	for _, g := range gl {
		isFork := g.ForkedFrom != nil
		if isFork && !f.IncludeForks {
			continue
		}
		if g.Archived && !f.IncludeArchived {
			continue
		}
		if wantVis != "" && wantVis != "all" && strings.ToLower(g.Visibility) != wantVis {
			continue
		}
		cloneURL := g.HTTPURL
		if wantSSH && g.SSHURL != "" {
			cloneURL = g.SSHURL
		}
		repos = append(repos, Repo{
			Name:       g.Path,
			CloneURL:   cloneURL,
			IsFork:     isFork,
			IsArchived: g.Archived,
			Visibility: g.Visibility,
		})
	}
	return repos, nil
}

// mergedArgs builds the `glab mr list` argv that asks for the single newest
// merged MR whose source branch is branch. The "--flag=value" form binds each
// value to its flag, so a repo or branch beginning with "-" can't be mistaken
// for a separate flag.
func (gitlabProvider) mergedArgs(repo, branch string) []string {
	return []string{
		"mr", "list",
		"--repo=" + repo,
		"--source-branch=" + branch,
		"--merged",
		"--output", "json",
		"--per-page", "1",
	}
}

// decodeMerged parses the JSON array glab emits for `mr list` and returns the
// first MR's iid (its project-scoped number), or 0 when the array is empty.
func (gitlabProvider) decodeMerged(data []byte) (int, error) {
	var mrs []struct {
		IID int `json:"iid"`
	}
	if err := json.Unmarshal(data, &mrs); err != nil {
		return 0, fmt.Errorf("parse glab mr list output: %w", err)
	}
	if len(mrs) == 0 {
		return 0, nil
	}
	return mrs[0].IID, nil
}
