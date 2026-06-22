package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/cache"
	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [<repo>]",
		Short: "Report sync health; with a repo, show its error and the likely fix",
		Long: `Report which tracked repos failed their most recent sync.

With no argument, lists every repo whose last sync attempt failed (their
cached copies are stale). With a repo, prints the full captured error, when
it happened, and a suggested fix.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return errs.Wrap(errs.Config, err)
			}
			if len(args) == 1 {
				return runStatusRepo(c, args[0])
			}
			return runStatusSummary(c)
		},
	}
}

// syncFailure describes a repo whose most recent sync attempt failed.
type syncFailure struct {
	Name        string
	LastSyncAt  time.Time // last *successful* sync; zero if never
	LastErrorAt time.Time
	LastError   string
}

// collectSyncFailures returns every tracked repo whose meta records a failed
// most-recent attempt (LastError set), newest failure first. Shared by the
// `status` summary and the session-context staleness banner.
func collectSyncFailures(c *config.Config) []syncFailure {
	var out []syncFailure
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			continue
		}
		meta, err := cache.LoadMeta(name)
		if err != nil || meta == nil || meta.LastError == "" {
			continue
		}
		out = append(out, syncFailure{
			Name:        name,
			LastSyncAt:  meta.LastSyncAt,
			LastErrorAt: meta.LastErrorAt,
			LastError:   meta.LastError,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastErrorAt.After(out[j].LastErrorAt) })
	return out
}

func runStatusSummary(c *config.Config) error {
	fails := collectSyncFailures(c)
	if len(fails) == 0 {
		fmt.Printf("All %d tracked repos synced cleanly.\n", len(c.Repos))
		return nil
	}
	fmt.Printf("%d of %d repos have a failing sync:\n", len(fails), len(c.Repos))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, f := range fails {
		fmt.Fprintf(w, "  %s\t%s\n", f.Name, lastGoodSync(f.LastSyncAt))
	}
	w.Flush()
	fmt.Println("Run `shed status <repo>` for the full error and suggested fix.")
	return nil
}

func runStatusRepo(c *config.Config, arg string) error {
	r, err := c.Resolve(arg)
	if err != nil {
		return err // already coded errs.NotFound (exit 2), like every other command
	}
	name, err := r.ResolvedName()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	meta, err := cache.LoadMeta(name)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}

	fmt.Println(name)
	if meta == nil {
		fmt.Printf("  not synced yet — run `shed sync %s`\n", name)
		return nil
	}
	fmt.Printf("  last good sync:  %s\n", stampLine(meta.LastSyncAt))
	if meta.LastError == "" {
		fmt.Println("  status:          ok")
		return nil
	}
	if !meta.LastErrorAt.IsZero() {
		fmt.Printf("  last attempt:    failed %s ago (%s)\n",
			relDuration(time.Since(meta.LastErrorAt)), meta.LastErrorAt.Format(stampFmt))
	}
	fmt.Println("  error:")
	for _, line := range wrapIndent(meta.LastError, "    ", 76) {
		fmt.Println(line)
	}
	fmt.Printf("  likely cause: %s\n", likelyCause(meta.LastError, name))
	return nil
}

const stampFmt = "2006-01-02 15:04 MST"

// lastGoodSync renders the age of the last successful sync for summary rows.
func lastGoodSync(t time.Time) string {
	if t.IsZero() {
		return "never synced successfully"
	}
	return "last good sync " + relTime(t)
}

// stampLine renders an absolute+relative timestamp for the detail view.
func stampLine(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%s   (%s)", relTime(t), t.Format(stampFmt))
}

// likelyCause maps a captured sync error to a one-line suggested fix. Pure
// string heuristics over git's output — best-effort, never authoritative.
func likelyCause(errText, name string) string {
	low := strings.ToLower(errText)
	sync := fmt.Sprintf("`shed sync %s`", name)
	switch {
	case containsAny(low, "could not read username", "authentication failed", "permission denied", "403 forbidden", "terminal prompts disabled", "invalid credentials"):
		return "authentication — run `gh auth login`, then " + sync
	case containsAny(low, "repository not found", "not found", "404", "does not exist"):
		return fmt.Sprintf("repo may be renamed or deleted upstream — verify it exists, or remove it with `shed rm %s`", name)
	case containsAny(low, "could not resolve host", "connection refused", "connection timed out", "timed out", "network is unreachable", "temporary failure in name resolution"):
		return "network — likely transient; check your connection and re-run " + sync
	case strings.Contains(low, "locked"):
		return "another sync held the lock — re-run " + sync + " shortly"
	case containsAny(low, "no space left", "disk full"):
		return "disk full — free space, then re-run " + sync
	default:
		return "re-run " + sync + "; see the full output above"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// wrapIndent splits s on existing newlines, then word-wraps each segment to
// width columns, prefixing every output line with indent.
func wrapIndent(s, indent string, width int) []string {
	var out []string
	for _, seg := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		words := strings.Fields(seg)
		if len(words) == 0 {
			continue
		}
		line := words[0]
		for _, wd := range words[1:] {
			if len(line)+1+len(wd) > width {
				out = append(out, indent+line)
				line = wd
				continue
			}
			line += " " + wd
		}
		out = append(out, indent+line)
	}
	return out
}
