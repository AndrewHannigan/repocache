package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
)

func newDescribeCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:     "describe <repo> [description]",
		Aliases: []string{"desc"},
		Short:   "Set or show a repo's one-line description",
		Long: `describe attaches a short, human-written summary to a tracked repo.
The description is shown in 'shed ls' and surfaced in the agent
session-context, so an agent starts each session knowing what each repo is for.

  shed repo describe <repo>                show the repo's current description
  shed repo describe <repo> "<text>"       set it (quote text containing spaces)
  shed repo describe <repo> --clear        remove it

The description is capped at 100 characters and must be a single line.
<repo> resolves like every other command: an exact name, or an unambiguous
suffix (e.g. 'shed' for 'github.com/AndrewHannigan/shed').`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A nil desc means "show"; a non-nil desc (possibly "") means "set".
			var desc *string
			switch {
			case clear:
				if len(args) == 2 {
					return errs.New(errs.Config, "pass either a description or --clear, not both")
				}
				empty := ""
				desc = &empty
			case len(args) == 2:
				desc = &args[1]
			}
			return runDescribe(args[0], desc)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the repo's description")
	return cmd
}

// runDescribe shows a repo's description (desc == nil) or sets it (desc != nil,
// where "" clears). Setting takes the config lock; showing is a plain read.
func runDescribe(name string, desc *string) error {
	if desc == nil {
		return showDescription(name)
	}
	// Trim surrounding whitespace so a stray space can't smuggle past the
	// single-line check or leave a description that renders as blank padding.
	newDesc := strings.TrimSpace(*desc)
	if err := config.ValidateDescription(newDesc); err != nil {
		return errs.Wrap(errs.Config, err)
	}
	var resolved string
	err := config.WithLock(configLockTimeout, func(c *config.Config) error {
		r, err := c.Resolve(name)
		if err != nil {
			return err
		}
		resolved, _ = r.ResolvedName()
		r.Description = newDesc
		return config.Save(c)
	})
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			return errs.Wrap(errs.Locked, err)
		}
		return errs.EnsureCoded(err, errs.Config)
	}
	if newDesc == "" {
		fmt.Printf("cleared description for %s\n", resolved)
	} else {
		fmt.Printf("described %s: %s\n", resolved, newDesc)
	}
	return nil
}

// showDescription prints a repo's current description, or a hint to set one
// when it has none.
func showDescription(name string) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	r, err := c.Resolve(name)
	if err != nil {
		return err
	}
	resolved, _ := r.ResolvedName()
	if r.Description == "" {
		fmt.Printf("%s has no description (set one with `shed repo describe %s \"<text>\"`)\n", resolved, name)
		return nil
	}
	fmt.Printf("%s: %s\n", resolved, r.Description)
	return nil
}
