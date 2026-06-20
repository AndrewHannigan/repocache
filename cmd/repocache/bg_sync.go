package main

import (
	"github.com/spf13/cobra"
)

func newBgSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__bg-sync",
		Short:  "(internal) Background sync invoked by SessionStart hooks",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("__bg-sync")
		},
	}
}
