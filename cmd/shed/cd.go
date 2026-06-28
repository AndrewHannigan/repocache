package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func newCdCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "cd <name>",
		Short: "Open a shell in a repo or workspace by name",
		Long: `cd opens an interactive shell in a stored repo or a workspace,
located by a single name.

Repo names and workspace names share one namespace — a workspace can never have
the same name as a repo, and a repo can never have the same name as a workspace
(enforced when each is created) — so one name is always unambiguous:

    shed cd projects        # a repo: read-only  ~/.shed/repos/.../projects
    shed cd my-workspace    # a workspace: writable ~/.shed/workspaces/...

The name matches a repo the same way the rest of shed does: an exact name, or an
unambiguous trailing path segment (so "projects" finds "github.com/you/projects").

cd prints a note saying whether it opened the read-only repo or the write-ready
workspace, and where. The shell runs as a subprocess, so quitting it (exit or
Ctrl-D) returns you to where you started — like 'shed resume', cd cannot change
your parent shell's directory. Pass --path to print the resolved path instead,
for use as 'cd "$(shed cd --path <name>)"'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCd(args[0], printOnly)
		},
	}
	cmd.Flags().BoolVar(&printOnly, "path", false, "print the resolved path instead of opening a shell")
	return cmd
}

func runCd(name string, printOnly bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}

	_, wsPath, wsFound := workspace.LocateByName(repoNames(c), name)
	repoMatches := repoNamesMatching(c, name)

	// Repo and workspace names share one namespace (enforced at creation time by
	// the guards in `add` and `workspace new`), so a healthy library has at most
	// one match. If somehow both match — a library that predates the guards —
	// refuse rather than silently pick one.
	if wsFound && len(repoMatches) > 0 {
		return errs.New(errs.Exists,
			"%q matches both a workspace and the repo %s; rename one (see `shed ls`)", name, repoMatches[0])
	}

	switch {
	case wsFound:
		return enterCd(wsPath, "the write-ready workspace "+paths.Display(wsPath), printOnly)
	case len(repoMatches) == 1:
		repoName := repoMatches[0]
		if !repostore.Exists(repoName) {
			return errs.New(errs.NotFound,
				"repo %s is not synced yet; run `shed sync %s` first", repoName, name)
		}
		p := paths.RepoStorePath(repoName)
		return enterCd(p, "the read-only repo "+paths.Display(p), printOnly)
	case len(repoMatches) > 1:
		return errs.New(errs.NotFound,
			"name %q is ambiguous; matches repos: %s", name, strings.Join(repoMatches, ", "))
	default:
		return errs.New(errs.NotFound, "no repo or workspace named %q (see `shed ls`)", name)
	}
}

// enterCd opens an interactive subshell in dir after printing a note that names
// the target (label already includes the display path). Like `shed resume`, the
// directory change lives in the subprocess: when the shell exits you are back
// where you started, because a subprocess cannot change its parent shell's
// working directory. With printOnly it writes the bare path to stdout instead
// (for `cd "$(shed cd --path <name>)"` and other scripting) and opens no shell.
func enterCd(dir, label string, printOnly bool) error {
	if printOnly {
		fmt.Println(dir)
		return nil
	}
	fmt.Fprintf(os.Stderr, "shed: changing to %s\n", label)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	run := exec.Command(shell)
	run.Dir = dir
	run.Stdin = os.Stdin
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Propagate the shell's own exit status (e.g. you ran `exit 3`).
			return &errs.Coded{Code: exitErr.ExitCode(), Err: fmt.Errorf("shell exited with status %d", exitErr.ExitCode())}
		}
		return errs.Wrap(errs.Config, err)
	}
	return nil
}

// repoNamesMatching returns the resolved names of every stored repo that the
// name n selects under `shed cd` — the same exact-then-trailing-"/"-segment rule
// config.Resolve uses. More than one match means n is an ambiguous repo
// reference. Shared by the conflict guards in `add` and `workspace new` so the
// notion of "this name already belongs to a repo" stays identical to what cd
// resolves.
func repoNamesMatching(c *config.Config, n string) []string {
	var out []string
	for i := range c.Repos {
		rn, err := c.Repos[i].ResolvedName()
		if err != nil {
			continue
		}
		if rn == n || strings.HasSuffix(rn, "/"+n) {
			out = append(out, rn)
		}
	}
	return out
}

// workspaceNamesShadowedBy returns existing workspace names that would resolve
// to the repo repoName under `shed cd` — i.e. names that, once a repo named
// repoName exists, would be ambiguous between the workspace and the repo. It is
// the `add`-side mirror of repoNamesMatching: there a candidate workspace name
// is tested against existing repos; here a candidate repo name is tested against
// existing workspaces.
func workspaceNamesShadowedBy(c *config.Config, repoName string) []string {
	infos, err := workspace.List(repoNames(c))
	if err != nil {
		return nil
	}
	var out []string
	for _, i := range infos {
		w := i.Branch // the workspace name
		if repoName == w || strings.HasSuffix(repoName, "/"+w) {
			out = append(out, w)
		}
	}
	return out
}
