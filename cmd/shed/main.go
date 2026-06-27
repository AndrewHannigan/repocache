package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/errs"
)

// version is set at link time by goreleaser via -ldflags "-X main.version=…".
// Default "dev" is what you see when running `go run` / a bare `go build`.
var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		var coded *errs.Coded
		if errors.As(err, &coded) {
			os.Exit(coded.Code)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "shed",
		Short:         "Read-only store of git repos for terminal coding agents",
		Long:          rootLong,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Record successful "working" commands to the history log. PostRun (not
		// the …E variant) so logging can never alter exit status; cobra runs the
		// closest Persistent hook up the tree, and no subcommand defines one, so
		// this fires for every leaf — on success only (a failed RunE skips it).
		PersistentPostRun: func(c *cobra.Command, _ []string) { recordCommand(c) },
	}
	cmd.SetHelpCommand(newHelpCmd())
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.AddCommand(
		newInitCmd(),
		newUninstallCmd(),
		newAddCmd(),
		newRmCmd(),
		newLsCmd(),
		newSyncCmd(),
		newStatusCmd(),
		newWorkspaceCmd(),
		newPruneCmd(),
		newHistoryCmd(),
		newSessionContextCmd(),
		newBgSyncCmd(),
		newWelcomeTourCmd(),
	)
	return cmd
}

const rootLong = `shed maintains a read-only local store of GitHub repos and
creates writable workspaces from it via 'git clone --reference'.

Designed for terminal coding agents (Claude Code, Cursor, opencode)
to search across many repos and edit a few.`
