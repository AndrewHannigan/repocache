package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/agents"
	"github.com/AndrewHannigan/repocache/pkg/errs"
)

func newUninstallCmd() *cobra.Command {
	var agentsFlag string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Reverse agent integration (leaves repocache data and config in place)",
		Long: `uninstall removes the entries repocache previously added to each
agent's config (REPOCACHE.md, the @import line, allowed-directory
entries, SessionStart hook). Uses a sidecar state file to know which
entries are repocache's; other entries are preserved.

Does NOT delete ~/.config/repocache/ or ~/.local/share/repocache/.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(agentsFlag)
		},
	}
	cmd.Flags().StringVar(&agentsFlag, "agents", "auto", "which agents to uninstall: auto|all|<comma-separated list>")
	return cmd
}

func runUninstall(flag string) error {
	list, err := agents.SelectByFlag(flag)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if len(list) == 0 {
		fmt.Fprintln(os.Stderr, "no agents selected")
		return nil
	}
	state, err := agents.LoadState()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	for _, a := range list {
		prev := state.Agents[a.Key()]
		fmt.Printf("%s:\n", a.Name())
		if err := a.Uninstall(prev); err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		delete(state.Agents, a.Key())
		fmt.Printf("  removed @REPOCACHE.md, %d directories, %d hooks\n",
			len(prev.AddedPaths), len(prev.AddedHooks))
	}
	if err := agents.SaveState(state); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	return nil
}
