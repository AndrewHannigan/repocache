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
		Short:         "Read-only mirror of git repos for terminal coding agents",
		Long:          rootLong,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
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
		newSessionContextCmd(),
		newBgSyncCmd(),
	)
	return cmd
}

const rootLong = `shed maintains a read-only local mirror of GitHub repos and
creates writable workspaces from it via 'git clone --reference'.

Designed for terminal coding agents (Claude Code, Codex CLI, Cursor,
opencode) to search across many repos and edit a few.`
