package main

import (
	"github.com/spf13/cobra"
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
			_ = agents
			_ = noBgSync
			_ = printAgentDoc
			return notImplemented("init")
		},
	}
	cmd.Flags().StringVar(&agents, "agents", "auto", "agent integration: auto|all|none|<comma-separated list>")
	cmd.Flags().BoolVar(&noBgSync, "no-bg-sync", false, "skip the Claude Code SessionStart bg-sync hook")
	cmd.Flags().BoolVar(&printAgentDoc, "print-agent-doc", false, "print the embedded REPOCACHE.md to stdout and exit")
	return cmd
}
