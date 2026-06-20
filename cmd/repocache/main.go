package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/errs"
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
		Use:           "repocache",
		Short:         "Read-only mirror of git repos for terminal coding agents",
		Long:          rootLong,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.SetHelpCommand(newHelpCmd())
	cmd.AddCommand(
		newInitCmd(),
		newUninstallCmd(),
		newRepoCmd(),
		newSyncCmd(),
		newWorkspaceCmd(),
		newBgSyncCmd(),
	)
	return cmd
}

const rootLong = `repocache maintains a read-only local mirror of GitHub repos and
creates writable workspaces from it via 'git clone --reference'.

Designed for terminal coding agents (Claude Code, Codex CLI, Gemini CLI,
OpenCode) to search across many repos and edit a few.

See https://github.com/AndrewHannigan/repocache for the full SPEC.`

// notImplemented is a placeholder for command stubs not yet wired up.
func notImplemented(name string) error {
	return &errs.Coded{Code: 1, Err: fmt.Errorf("%s: not implemented yet", name)}
}
