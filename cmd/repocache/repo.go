package main

import (
	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the library of tracked repositories",
	}
	cmd.AddCommand(newRepoAddCmd(), newRepoRmCmd(), newRepoListCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a repository to the library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = name
			_ = args[0]
			return notImplemented("repo add")
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "override the default name (derived from URL)")
	return cmd
}

func newRepoRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a repository from the library (does not delete cache on disk)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args[0]
			return notImplemented("repo rm")
		},
	}
}

func newRepoListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tracked repos with last sync, size, branch count",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = jsonOut
			return notImplemented("repo list")
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}
