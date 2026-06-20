package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

func newInitCmd() *cobra.Command {
	var (
		agents        string
		noBgSync      bool
		printAgentDoc bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap dirs + config; integrate with detected agents",
		Long: `init creates the repocache config and data directories if missing
and, in TTY mode, prompts to install repocache integration into each
detected agent's config (CLAUDE.md / AGENTS.md / GEMINI.md, plus the
agent's allowed-directory list, plus a SessionStart bg-sync hook for
Claude Code).

Idempotent. Re-run after upgrading to refresh the embedded
REPOCACHE.md content for each integrated agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printAgentDoc {
				// Agent doc not yet implemented; placeholder.
				fmt.Println("# (REPOCACHE.md content will be bundled in step 6)")
				return nil
			}
			if err := runInit(); err != nil {
				return err
			}
			// Agent integration (--agents, --no-bg-sync) is implemented in steps 6–8.
			if agents != "none" {
				fmt.Fprintln(os.Stderr, "\nnote: agent integration (--agents) is not implemented yet")
				fmt.Fprintln(os.Stderr, "      will be wired up in a follow-up step.")
			}
			_ = noBgSync
			return nil
		},
	}
	cmd.Flags().StringVar(&agents, "agents", "auto", "agent integration: auto|all|none|<comma-separated list>")
	cmd.Flags().BoolVar(&noBgSync, "no-bg-sync", false, "skip the Claude Code SessionStart bg-sync hook")
	cmd.Flags().BoolVar(&printAgentDoc, "print-agent-doc", false, "print the embedded REPOCACHE.md to stdout and exit")
	return cmd
}

// runInit bootstraps directories and the config file. Idempotent.
func runInit() error {
	steps := []struct {
		path  string
		isDir bool
	}{
		{paths.ConfigDir(), true},
		{paths.DataDir(), true},
		{paths.ReposDir(), true},
		{paths.WorkspacesDir(), true},
		{paths.LogsDir(), true},
		{paths.ConfigFile(), false},
	}
	for _, s := range steps {
		var (
			created bool
			err     error
		)
		if s.isDir {
			created, err = ensureDir(s.path)
		} else {
			created, err = ensureConfigFile(s.path)
		}
		if err != nil {
			return errs.Wrap(errs.Config, err)
		}
		status := "exists "
		if created {
			status = "created"
		}
		fmt.Printf("%s  %s\n", status, paths.Display(s.path))
	}
	return nil
}

func ensureDir(p string) (created bool, err error) {
	if _, err := os.Stat(p); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(p, 0755); err != nil {
		return false, err
	}
	return true, nil
}

func ensureConfigFile(p string) (created bool, err error) {
	if _, err := os.Stat(p); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(p, config.EmptyTemplate(), 0644); err != nil {
		return false, err
	}
	return true, nil
}
