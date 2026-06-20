package main

import (
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	var agents string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Reverse agent integration (leaves repocache data and config in place)",
		Long: `uninstall removes the entries repocache previously added to each
agent's config (REPOCACHE.md, the @import line, allowed-directory
entries, SessionStart hook). Uses a sidecar state file to know which
entries are repocache's; other entries are preserved.

Does NOT delete ~/.config/repocache/ or ~/.local/share/repocache/.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = agents
			return notImplemented("uninstall")
		},
	}
	cmd.Flags().StringVar(&agents, "agents", "auto", "which agents to uninstall: auto|all|<comma-separated list>")
	return cmd
}
