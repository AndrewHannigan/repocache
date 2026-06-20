package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/paths"
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
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a repository from the library (does not delete cache on disk)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoRm(args[0])
		},
	}
}

func runRepoRm(name string) error {
	var removed bool
	err := config.WithLock(configLockTimeout, func(c *config.Config) error {
		for i, r := range c.Repos {
			n, err := r.ResolvedName()
			if err != nil {
				continue
			}
			if n == name {
				c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
				removed = true
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
	if !removed {
		return errs.New(errs.NotFound, "repo %q is not in the config", name)
	}
	fmt.Printf("removed %s from config\n", name)
	if cache.Exists(name) {
		fmt.Printf("cache still on disk at %s (rm -rf to free)\n",
			paths.Display(paths.CacheRepoPath(name)))
	}
	return nil
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
