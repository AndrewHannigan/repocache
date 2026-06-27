package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/debuglog"
	"github.com/AndrewHannigan/shed/pkg/errs"
)

// version is set at link time by goreleaser via -ldflags "-X main.version=…".
// Default "dev" is what you see when running `go run` / a bare `go build`.
var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		debuglog.Log("command error", "err", err.Error())
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
		// Turn on debug logging (when settings.debug_mode is set) before any
		// command runs, and trace the invocation. cobra runs the closest
		// Persistent hook up the tree, and no subcommand defines one, so this
		// fires for every leaf — including the hidden hooks and the bg-sync
		// worker, which run in their own processes.
		PersistentPreRun: func(c *cobra.Command, args []string) { initDebugLog(c, args) },
		// Record successful "working" commands to the history log. PostRun (not
		// the …E variant) so logging can never alter exit status; cobra runs the
		// closest Persistent hook up the tree, and no subcommand defines one, so
		// this fires for every leaf — on success only (a failed RunE skips it).
		PersistentPostRun: func(c *cobra.Command, _ []string) {
			recordCommand(c)
			debuglog.Log("command done", "command", c.CommandPath())
		},
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
		newOnToolCallCmd(),
		newBgSyncCmd(),
		newWelcomeTourCmd(),
		newResumeCmd(),
	)
	return cmd
}

// initDebugLog turns on file logging when settings.debug_mode is set in
// config.toml, then traces the command about to run. Best-effort throughout: an
// unreadable config simply leaves logging off, and a failure to open the log
// file is reported once on stderr but never blocks the command.
func initDebugLog(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil || !cfg.Settings.DebugMode {
		return
	}
	if err := debuglog.Enable(); err != nil {
		fmt.Fprintln(os.Stderr, "shed: could not open debug log:", err)
		return
	}
	debuglog.Log("command start", "command", cmd.CommandPath(), "args", args, "version", version)
}

const rootLong = `shed maintains a read-only local store of GitHub repos and
creates writable workspaces from it via 'git clone --reference'.

Designed for terminal coding agents (Claude Code, Cursor, opencode)
to search across many repos and edit a few.`
