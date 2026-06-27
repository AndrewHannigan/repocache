package main

import (
	"github.com/spf13/cobra"
)

// newRepoCmd groups the library-management commands under a `repo` noun,
// mirroring the `workspace` group. The repo library is the read-only side of the
// shed: the tracked repos (and owners) your agents read from.
//
// `repo add` and `repo rm` are the same commands as top-level `shed add` /
// `shed rm` — the operations are repo-scoped already, so the grouped form just
// reuses them. `repo ls` is deliberately *not* the same as top-level `shed ls`:
// it lists only the library (owners + repos), where `shed ls` also includes the
// writable workspaces.
func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"repos"},
		Short:   "Manage the read-only repo library (list, add, remove tracked repos)",
	}
	cmd.AddCommand(
		newRepoLsCmd(),
		// Reuse the top-level builders verbatim: a fresh command instance per
		// call, so the same definition can live under both root and `repo`.
		newAddCmd(),
		newRmCmd(),
	)
	return cmd
}

func newRepoLsCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List the repo library: tracked owners and read-only repos",
		Long: `ls shows the repo library — the read-only side of the shed:

  Owners  whole GitHub users/orgs you track; sync auto-adds their repos
  Repos   read-only reference copies your agents read from, with last sync

Unlike 'shed ls', this omits workspaces (the writable side) — run
'shed workspace ls' for those. A repo's "⚠ sync failing" marker means its
last fetch failed, so its stored copy is stale — run 'shed status <repo>'
for the error and the fix.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoOnlyList(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}
