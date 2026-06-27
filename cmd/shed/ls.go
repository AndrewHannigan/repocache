package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/cache"
	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
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

A repo's "⚠ sync failing" marker means its last fetch failed, so its cached
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
// cache for each tracked repo's last-sync time. The probes are deliberately
// cheap (a stat and a small metadata read, no size walk or git subprocess) so
// this is safe to run on the session-context hot path, where the repo count
// can be large (a tracked owner may pull in dozens). Workspace state is
// gathered separately by collectWorkspaces.
func collectRepoList(c *config.Config) ([]repoRow, []ownerRow, error) {
	rows := make([]repoRow, 0, len(c.Repos))
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			return nil, nil, errs.Wrap(errs.Config, err)
		}
		row := repoRow{Name: name, URL: r.URL, Source: r.Source, LastSyncAt: nil}
		if cache.Exists(name) {
			row.Path = paths.CacheRepoPath(name)
			if meta, err := cache.LoadMeta(name); err == nil && meta != nil {
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
	// The "ADDED BY" column only matters when an owner auto-added some repo;
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
		fmt.Fprintln(w, "  NAME\tLAST SYNC\tADDED BY")
	} else {
		fmt.Fprintln(w, "  NAME\tLAST SYNC")
	}
	for _, r := range repos {
		if showSource {
			source := "—"
			if r.Source != "" {
				source = r.Source
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\n", r.Name, lastSyncLabel(r), source)
		} else {
			fmt.Fprintf(w, "  %s\t%s\n", r.Name, lastSyncLabel(r))
		}
	}
	w.Flush()
}

// lastSyncLabel renders a repo's last-sync cell: a relative time (or "never"),
// annotated when the most recent fetch failed so its cache is known stale.
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
	fmt.Fprintln(w, "  REPO\tBRANCH\tDIRTY\tUNPUSHED\tAGE\tPATH")
	for _, i := range infos {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			i.Name, i.Branch, dirtyLabel(i.Dirty), unpushedLabel(i.Unpushed), relTime(i.Age), paths.Display(i.Path))
	}
	w.Flush()
}

// repoListText renders the `ls` overview to a string for embedding in session
// context, so the agent starts each session knowing which repos and workspaces
// already exist without having to run `shed ls` itself. Best-effort: returns ""
// if the library can't be read, so a config hiccup never breaks session
// startup. The empty-workspaces hint is suppressed (workspaceHint=false) to
// keep the snapshot terse for an agent.
func repoListText() string {
	c, err := config.Load()
	if err != nil {
		return ""
	}
	rows, owners, err := collectRepoList(c)
	if err != nil {
		return ""
	}
	workspaces, err := collectWorkspaces(c)
	if err != nil {
		workspaces = nil // best-effort; still show repos/owners
	}
	if len(rows) == 0 && len(owners) == 0 && len(workspaces) == 0 {
		return "" // nothing tracked yet; the guide already covers adding repos
	}
	var buf bytes.Buffer
	if err := writeLibrary(&buf, owners, rows, workspaces, false); err != nil {
		return ""
	}
	return buf.String()
}
