package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
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
		Short:         "git repo management for terminal coding agents",
		Long:          rootLong,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Gate every real command on `shed init` having run. Like the PostRun
		// below, cobra runs the closest Persistent hook up the tree and no
		// subcommand defines one, so this fires for every leaf. It runs before
		// the command's own RunE, so an uninitialized install fails fast with a
		// clear "run shed init" message instead of behaving like an empty
		// library. Commands that must work pre-init are exempted (see initExempt).
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			if initExempt(c) || paths.Initialized() {
				return nil
			}
			return errs.New(errs.Config, "shed is not initialized; run 'shed init' first")
		},
		// Record successful "working" commands to the history log. PostRun (not
		// the …E variant) so logging can never alter exit status; cobra runs the
		// closest Persistent hook up the tree, and no subcommand defines one, so
		// this fires for every leaf — on success only (a failed RunE skips it).
		PersistentPostRun: func(c *cobra.Command, _ []string) { recordCommand(c) },
	}
	cmd.SetHelpCommand(newHelpCmd())
	// Make a bare `shed`, `shed -h`, and `shed --help` all print the same
	// curated overview that `shed help` prints. Without this, the flag form
	// falls back to Cobra's auto-generated usage — a different topline and
	// layout from `shed help`, which is confusing. Subcommand help
	// (`shed add --help`) still uses Cobra's default rendering.
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c == cmd {
			fmt.Fprint(c.OutOrStdout(), helpTopics["overview"])
			return
		}
		defaultHelp(c, args)
	})
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.AddCommand(
		newInitCmd(),
		newAddCmd(),
		newRmCmd(),
		newLsCmd(),
		newDescribeCmd(),
		newRepoCmd(),
		newOwnerCmd(),
		newSyncCmd(),
		newStatusCmd(),
		newWorkspaceCmd(),
		newPruneCmd(),
		newHistoryCmd(),
		newSessionContextCmd(),
		newOnToolCallCmd(),
		newBgSyncCmd(),
		newWelcomeTourCmd(),
		newResumeCmd(),
	)
	return cmd
}

// initExempt reports whether c may run before `shed init` has set up the config
// and data dirs. `init` itself bootstraps them; `help` documents the tool and
// must work on a fresh install; and the hidden `__*` hook commands are invoked
// by agents (sometimes before any manual init) and must never fail the gate and
// disrupt a session — they already treat a missing store as empty. Cobra routes
// `--help`, `--version`, and bare group commands to help without reaching a
// PersistentPreRun, so those need no entry here.
func initExempt(c *cobra.Command) bool {
	return c.Name() == "init" || c.Name() == "help" || strings.HasPrefix(c.Name(), "__")
}

const rootLong = `shed maintains a read-only local store of GitHub repos and
creates writable workspaces from it via 'git clone --reference'.

Designed for terminal coding agents (Claude Code, Cursor, opencode)
to search across many repos and edit a few.`
