package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/paths"
	"github.com/AndrewHannigan/repocache/pkg/workspace"
)

const configLockTimeout = 2 * time.Second

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the library of tracked repositories",
	}
	cmd.AddCommand(newRepoAddCmd(), newRepoRmCmd(), newRepoListCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a repository to the library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoAdd(args[0], name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "override the default name (derived from URL)")
	return cmd
}

func runRepoAdd(url, overrideName string) error {
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
		c.Repos = append(c.Repos, config.Repo{URL: url, Name: overrideName})
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return wrapIfNotCoded(err, errs.Config)
	}
	fmt.Printf("added %s (run `repocache sync` to fetch)\n", effectiveName)
	return nil
}

func newRepoRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a repository: config entry, cache on disk, and its workspaces",
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
	// checks on the workspaces before deleting anything.
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	r, err := c.Resolve(name)
	if err != nil {
		return err
	}
	resolved, err := r.ResolvedName()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}

	workspaces, err := workspace.List([]string{resolved})
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if !force {
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
		if len(blocked) > 0 {
			return errs.New(errs.Dirty,
				"refusing to remove %s; these workspaces have unsaved work:\n%s\ncommit and push, or pass --force to discard",
				resolved, strings.Join(blocked, "\n"))
		}
	}

	// Remove on-disk artifacts first, then the config entry, so a failure
	// partway through leaves the entry as a record of what still needs
	// cleanup rather than orphaning files with no tracked owner.
	if err := workspace.RemoveAllForRepo(resolved); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if err := cache.Remove(resolved, configLockTimeout); err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return errs.Wrap(errs.Config, err)
	}

	err = config.WithLock(configLockTimeout, func(c *config.Config) error {
		for i := range c.Repos {
			if n, _ := c.Repos[i].ResolvedName(); n == resolved {
				c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
				return config.Save(c)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return wrapIfNotCoded(err, errs.Config)
	}

	fmt.Printf("removed %s (config", resolved)
	if len(workspaces) > 0 {
		fmt.Printf(", %s", pluralize(len(workspaces), "workspace"))
	}
	fmt.Println(", cache on disk)")
	return nil
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func newRepoListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tracked repos with last sync, size, branch count",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoList(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

func runRepoList(jsonOut bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	type row struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Path        string `json:"path,omitempty"`
		LastSyncAt  any    `json:"last_sync_at"`
		SizeBytes   int64  `json:"size_bytes"`
		BranchCount int    `json:"branch_count"`
	}
	rows := make([]row, 0, len(c.Repos))
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			return errs.Wrap(errs.Config, err)
		}
		r := row{Name: name, URL: r.URL, LastSyncAt: nil}
		if cache.Exists(name) {
			r.Path = paths.CacheRepoPath(name)
			if size, err := cache.Size(name); err == nil {
				r.SizeBytes = size
			}
			if bc, err := cache.BranchCount(name); err == nil {
				r.BranchCount = bc
			}
			if meta, err := cache.LoadMeta(name); err == nil && meta != nil {
				r.LastSyncAt = meta.LastSyncAt.UTC().Format(time.RFC3339)
			}
		}
		rows = append(rows, r)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Println("(no repos tracked; add with `repocache repo add <url>`)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tLAST SYNC\tSIZE\tBRANCHES")
	for _, r := range rows {
		last := "never"
		if ts, ok := r.LastSyncAt.(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				last = relTime(t)
			}
		}
		size := "—"
		if r.SizeBytes > 0 {
			size = humanSize(r.SizeBytes)
		}
		branches := "—"
		if r.BranchCount > 0 {
			branches = fmt.Sprintf("%d", r.BranchCount)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, last, size, branches)
	}
	return w.Flush()
}

// wrapIfNotCoded ensures we propagate exit codes; if err is already a
// *errs.Coded we return it unchanged.
func wrapIfNotCoded(err error, code int) error {
	var c *errs.Coded
	if errors.As(err, &c) {
		return err
	}
	return errs.Wrap(code, err)
}

func humanSize(b int64) string {
	const k = 1024.0
	switch {
	case b < int64(k):
		return fmt.Sprintf("%d B", b)
	case b < int64(k*k):
		return fmt.Sprintf("%.1f KB", float64(b)/k)
	case b < int64(k*k*k):
		return fmt.Sprintf("%.1f MB", float64(b)/(k*k))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(k*k*k))
	}
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
