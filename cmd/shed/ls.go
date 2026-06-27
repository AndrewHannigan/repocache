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

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
)

func newLsCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List tracked repos with last sync and source",
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
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Repos  []repoRow  `json:"repos"`
			Owners []ownerRow `json:"owners"`
		}{rows, owners})
	}
	return writeRepoTable(os.Stdout, rows, owners)
}

// collectRepoList gathers the rows behind `ls`, probing the store for
// each tracked repo's last-sync time. The probes are deliberately cheap (a
// stat and a small metadata read, no size walk or git subprocess) so this is
// safe to run on the session-context hot path via repoListText.
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

// writeRepoTable renders the human-readable `ls` table to out.
func writeRepoTable(out io.Writer, rows []repoRow, owners []ownerRow) error {
	if len(rows) == 0 && len(owners) == 0 {
		fmt.Fprintln(out, "(no repos tracked; add with `shed add <url>`)")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if len(owners) > 0 {
		fmt.Fprintln(w, "OWNER\tREPOS")
		for _, o := range owners {
			fmt.Fprintf(w, "%s\t%d\n", o.Name, o.RepoCount)
		}
		fmt.Fprintln(w, "")
	}
	fmt.Fprintln(w, "NAME\tLAST SYNC\tSOURCE")
	for _, r := range rows {
		last := "never"
		if ts, ok := r.LastSyncAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				last = relTime(t)
			}
		}
		if r.LastError != "" {
			last += "  ⚠ sync failing"
		}
		source := "—"
		if r.Source != "" {
			source = r.Source
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, last, source)
	}
	return w.Flush()
}

// repoListText renders the `ls` table to a string for embedding in
// session context. Best-effort: returns "" if the library can't be read, so
// a config hiccup never breaks session startup.
func repoListText() string {
	c, err := config.Load()
	if err != nil {
		return ""
	}
	rows, owners, err := collectRepoList(c)
	if err != nil {
		return ""
	}
	if len(rows) == 0 && len(owners) == 0 {
		return "" // nothing tracked yet; the guide already covers adding repos
	}
	var buf bytes.Buffer
	if err := writeRepoTable(&buf, rows, owners); err != nil {
		return ""
	}
	return buf.String()
}
