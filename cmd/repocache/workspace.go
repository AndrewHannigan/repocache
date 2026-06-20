package main

import (
	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Manage writable workspaces derived from cache repos",
	}
	cmd.AddCommand(
		newWorkspaceNewCmd(),
		newWorkspaceListCmd(),
		newWorkspacePathCmd(),
		newWorkspaceRmCmd(),
	)
	return cmd
}

func newWorkspaceNewCmd() *cobra.Command {
	var base string
	cmd := &cobra.Command{
		Use:   "new <repo> <branch>",
		Short: "Create a workspace via `git clone --reference`",
		Long: `new creates a writable clone of the cache repo at
~/.local/share/repocache/workspaces/<repo>/<branch>/ using
'git clone --reference' so it shares object storage with the cache.

If <branch> exists on origin, checks it out. Otherwise creates it off
origin/HEAD (or --base). Prints the workspace path on stdout.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = base
			_ = args
			return notImplemented("workspace new")
		},
	}
	cmd.Flags().StringVar(&base, "base", "", "branch to fork from when <branch> is new (default: origin/HEAD)")
	return cmd
}

func newWorkspaceListCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspaces with dirty/unpushed state and age",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = jsonOut
			return notImplemented("workspace list")
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

func newWorkspacePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <repo> <branch>",
		Short: "Print the absolute workspace path (for cd $(...))",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return notImplemented("workspace path")
		},
	}
}

func newWorkspaceRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <repo> <branch>",
		Short: "Delete a workspace (refuses if dirty or unpushed unless --force)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = force
			_ = args
			return notImplemented("workspace rm")
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete even if there are uncommitted or unpushed changes")
	return cmd
}
