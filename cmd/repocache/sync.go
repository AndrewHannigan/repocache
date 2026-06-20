package main

import (
	"time"

	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var (
		jobs         int
		ifOlderThan  time.Duration
		jsonOut      bool
	)
	cmd := &cobra.Command{
		Use:   "sync [<name>...]",
		Short: "Fetch tracked repos and refresh their cache working trees",
		Long: `sync fetches each tracked repo (or the named subset), checks out
origin/HEAD detached, and re-applies chmod -R a-w on the working tree
so the cache stays read-only.

With --if-older-than, skip repos synced within the given duration.
Runs in parallel up to --jobs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = jobs
			_ = ifOlderThan
			_ = jsonOut
			_ = args
			return notImplemented("sync")
		},
	}
	cmd.Flags().IntVarP(&jobs, "jobs", "j", 4, "max concurrent fetches")
	cmd.Flags().DurationVar(&ifOlderThan, "if-older-than", 0, "skip repos synced within this duration (e.g. 1h)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit NDJSON results")
	return cmd
}
