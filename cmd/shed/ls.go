package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func newLsCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List your shed: tracked owners, read-only repos, and writable workspaces",
		Long: `ls shows everything shed is managing for you, in three sections:

  Owners      whole GitHub users/orgs you track; sync auto-adds their repos
  Repos       read-only reference copies your agents read from
  Workspaces  isolated writable clones where agents make and push changes

A repo's "⚠ sync failing" marker means its last fetch failed, so its stored
copy is stale — run 'shed status <repo>' for the error and the fix.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoList(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

type repoRow struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Source     string `json:"source,omitempty"`
	Path       string `json:"path,omitempty"`
	LastSyncAt any    `json:"last_sync_at"`
	LastError  string `json:"last_error,omitempty"`
}

type ownerRow struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	RepoCount int    `json:"repo_count"`
}

func runRepoList(jsonOut bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	rows, owners, err := collectRepoList(c)
	if err != nil {
		return err
	}
	workspaces, err := collectWorkspaces(c)
	if err != nil {
		return err
	}
	if jsonOut {
		if workspaces == nil {
			workspaces = []workspace.Info{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Owners     []ownerRow       `json:"owners"`
			Repos      []repoRow        `json:"repos"`
			Workspaces []workspace.Info `json:"workspaces"`
		}{owners, rows, workspaces})
	}
	// Interactive `ls` shows the empty-workspaces hint so a newcomer who has
	// repos but no workspaces yet learns the concept exists and how to create one.
	return writeLibrary(os.Stdout, owners, rows, workspaces, true)
}

// collectRepoList gathers the repo and owner rows behind `ls`, probing the
// repo store for each tracked repo's last-sync time. The probes are
// deliberately cheap (a stat and a small metadata read, no size walk or git
// subprocess) so this is safe to run on the session-context hot path, where the
// repo count can be large (a tracked owner may pull in dozens). Workspace state
// is gathered separately by collectWorkspaces.
func collectRepoList(c *config.Config) ([]repoRow, []ownerRow, error) {
	rows := make([]repoRow, 0, len(c.Repos))
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			return nil, nil, errs.Wrap(errs.Config, err)
		}
		row := repoRow{Name: name, URL: r.URL, Source: r.Source, LastSyncAt: nil}
		if repostore.Exists(name) {
			row.Path = paths.RepoStorePath(name)
			if meta, err := repostore.LoadMeta(name); err == nil && meta != nil {
				row.LastSyncAt = meta.LastSyncAt.UTC().Format(time.RFC3339)
				row.LastError = meta.LastError
			}
		}
		rows = append(rows, row)
	}
	owners := make([]ownerRow, 0, len(c.Owners))
	for _, o := range c.Owners {
		name, err := o.ResolvedName()
		if err != nil {
			return nil, nil, errs.Wrap(errs.Config, err)
		}
		owners = append(owners, ownerRow{Name: name, URL: o.URL, RepoCount: len(c.ReposForOwner(name))})
	}
	return rows, owners, nil
}

// collectWorkspaces returns the workspaces derived from the library's repos,
// with their dirty/unpushed/age state. Unlike collectRepoList this runs git
// per workspace, but the workspace count is naturally small (one per task in
// flight, reclaimed by `prune`), so the cost stays bounded.
func collectWorkspaces(c *config.Config) ([]workspace.Info, error) {
	names := make([]string, 0, len(c.Repos))
	for _, r := range c.Repos {
		if n, err := r.ResolvedName(); err == nil {
			names = append(names, n)
		}
	}
	infos, err := workspace.List(names)
	if err != nil {
		return nil, errs.Wrap(errs.Config, err)
	}
	return infos, nil
}

// writeLibrary renders the human-readable `ls` overview to out: a captioned
// section per kind of thing shed manages, so a newcomer can tell at a glance
// what each table is. Sections with no rows are omitted, except that
// workspaceHint keeps the Workspaces section (with a "how to create one" hint)
// when there are repos but no workspaces yet — the common first-run state.
func writeLibrary(out io.Writer, owners []ownerRow, repos []repoRow, workspaces []workspace.Info, workspaceHint bool) error {
	if len(owners) == 0 && len(repos) == 0 && len(workspaces) == 0 {
		fmt.Fprintln(out, "(nothing tracked yet — add a repo with `shed add <url>`)")
		return nil
	}
	first := true
	gap := func() {
		if !first {
			fmt.Fprintln(out)
		}
		first = false
	}
	if len(owners) > 0 {
		gap()
		writeOwnersSection(out, owners)
	}
	if len(repos) > 0 {
		gap()
		writeReposSection(out, repos)
	}
	if len(workspaces) > 0 || (workspaceHint && len(repos) > 0) {
		gap()
		writeWorkspacesSection(out, workspaces)
	}
	return nil
}

func writeOwnersSection(out io.Writer, owners []ownerRow) {
	fmt.Fprintln(out, "Owners")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  OWNER\tREPOS")
	for _, o := range owners {
		fmt.Fprintf(w, "  %s\t%d\n", o.Name, o.RepoCount)
	}
	w.Flush()
}

func writeReposSection(out io.Writer, repos []repoRow) {
	fmt.Fprintln(out, "Repos")
	// The "FROM" column only matters when an owner auto-added some repo;
	// hide it otherwise so the common (no-owners) case isn't cluttered with
	// a column of em-dashes.
	showSource := false
	for _, r := range repos {
		if r.Source != "" {
			showSource = true
			break
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if showSource {
		fmt.Fprintln(w, "  NAME\tLAST SYNC\tFROM\tPATH")
	} else {
		fmt.Fprintln(w, "  NAME\tLAST SYNC\tPATH")
	}
	for _, r := range repos {
		if showSource {
			source := "—"
			if r.Source != "" {
				source = r.Source
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", r.Name, lastSyncLabel(r), source, repoPathLabel(r))
		} else {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", r.Name, lastSyncLabel(r), repoPathLabel(r))
		}
	}
	w.Flush()
}

// repoPathLabel renders a repo's stored-copy path with $HOME collapsed to "~",
// matching the Workspaces section's PATH column. A repo that has never synced
// has no copy on disk yet (its Path is empty), shown as "—".
func repoPathLabel(r repoRow) string {
	if r.Path == "" {
		return "—"
	}
	return paths.Display(r.Path)
}

// lastSyncLabel renders a repo's last-sync cell: a relative time (or "never"),
// annotated when the most recent fetch failed so its stored copy is known stale.
func lastSyncLabel(r repoRow) string {
	last := "never"
	if ts, ok := r.LastSyncAt.(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			last = relTime(t)
		}
	}
	if r.LastError != "" {
		last += "  ⚠ sync failing"
	}
	return last
}

func writeWorkspacesSection(out io.Writer, infos []workspace.Info) {
	fmt.Fprintln(out, "Workspaces")
	if len(infos) == 0 {
		fmt.Fprintln(out, "  (none yet — create one with `shed workspace new <repo> <branch>`)")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tREPO\tDIRTY\tUNPUSHED\tAGE\tPATH")
	for _, i := range infos {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			i.Branch, i.Name, dirtyLabel(i.Dirty), unpushedLabel(i.Unpushed), relTime(i.Age), paths.Display(i.Path))
	}
	w.Flush()
}

// recentWorkspaceReposLimit caps how many repos the session-context snapshot
// lists, so a heavily-used shed surfaces only the handful the user is actively
// working in rather than its whole history of workspaces.
const recentWorkspaceReposLimit = 10

// repoActivity pairs a repo with the age of its most recently active workspace
// (the workspace AGE field — reflog-based lastActivity).
type repoActivity struct {
	Name string
	Age  time.Time
}

// recentWorkspaceRepos collapses workspaces to one entry per repo, keeping each
// repo's newest workspace activity, and returns them most-recent first, capped
// at limit (limit <= 0 means no cap). Ranking reuses the same workspace AGE the
// `ls` Workspaces section shows, so "recent" means the same thing in both places.
func recentWorkspaceRepos(infos []workspace.Info, limit int) []repoActivity {
	newest := make(map[string]time.Time, len(infos))
	for _, i := range infos {
		if cur, ok := newest[i.Name]; !ok || i.Age.After(cur) {
			newest[i.Name] = i.Age
		}
	}
	repos := make([]repoActivity, 0, len(newest))
	for name, age := range newest {
		repos = append(repos, repoActivity{Name: name, Age: age})
	}
	// Most recent first; the repo name breaks ties so the order is deterministic
	// (the map iteration above is randomized) and callers can rely on it.
	sort.Slice(repos, func(a, b int) bool {
		if repos[a].Age.Equal(repos[b].Age) {
			return repos[a].Name < repos[b].Name
		}
		return repos[a].Age.After(repos[b].Age)
	})
	if limit > 0 && len(repos) > limit {
		repos = repos[:limit]
	}
	return repos
}

// writeRecentWorkspaceRepos renders the recent-workspace repos as a small table
// (REPO, and how long ago that repo's newest workspace was last active).
func writeRecentWorkspaceRepos(out io.Writer, repos []repoActivity) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  REPO\tLAST WORKSPACE")
	for _, r := range repos {
		fmt.Fprintf(w, "  %s\t%s\n", r.Name, relTime(r.Age))
	}
	w.Flush()
}

// recentWorkspaceReposText renders, for embedding in session context, the repos
// the user has most recently had a workspace in: one row per repo, ranked by its
// newest workspace's age and capped at recentWorkspaceReposLimit. It replaces
// dumping the whole `ls` library, which a tracked owner can balloon to dozens of
// repos — the agent can still run `shed ls` for the full picture. Best-effort:
// returns "" if the library can't be read or no workspace exists yet, so a
// config hiccup never breaks session startup and a workspace-less shed simply
// omits the section.
func recentWorkspaceReposText() string {
	c, err := config.Load()
	if err != nil {
		return ""
	}
	workspaces, err := collectWorkspaces(c)
	if err != nil || len(workspaces) == 0 {
		return ""
	}
	var buf bytes.Buffer
	writeRecentWorkspaceRepos(&buf, recentWorkspaceRepos(workspaces, recentWorkspaceReposLimit))
	return buf.String()
}
