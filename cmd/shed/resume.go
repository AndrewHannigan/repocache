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
	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func newResumeCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "resume <name> [-- <args passed to the agent>]",
		Short: "Resume the agent session that created a workspace",
		Long: `resume reopens the agent session (Claude Code, opencode, or Cursor)
that created the named workspace, in the directory that session was launched
in:

    cd <session-cwd> && <agent> --resume <session-id> <args-after-->

<name> is the workspace name (unique across the shed), so no <repo> is needed.
Everything after '--' is forwarded to the agent verbatim, so its own flags work
unchanged — e.g. non-interactively:

    shed resume fix-bug -- -p "continue the refactor"

resume does not change your shell's working directory: the 'cd' happens inside
a subprocess, so when the session ends you are back where you started. Use
--print to emit the command instead of running it.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, passthrough, err := splitResumeArgs(cmd, args)
			if err != nil {
				return err
			}
			return runResume(name, passthrough, printOnly)
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the resume command instead of running it")
	return cmd
}

// splitResumeArgs separates the single positional <name> from the args after
// `--`, which are forwarded to the agent. cobra records the `--` position via
// ArgsLenAtDash; exactly one positional must precede it.
func splitResumeArgs(cmd *cobra.Command, args []string) (name string, passthrough []string, err error) {
	dash := cmd.ArgsLenAtDash()
	positional := args
	if dash >= 0 {
		positional = args[:dash]
		passthrough = args[dash:]
	}
	if len(positional) != 1 {
		return "", nil, errs.New(errs.Config,
			"resume takes exactly one workspace name (got %d); usage: shed resume <name> [-- <agent args>]", len(positional))
	}
	return positional[0], passthrough, nil
}

func runResume(name string, passthrough []string, printOnly bool) error {
	c, err := config.Load()
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	repo, wsPath, found := workspace.LocateByName(repoNames(c), name)
	if !found {
		return errs.New(errs.NotFound, "no workspace named %q (see `shed ls`)", name)
	}
	link, err := workspace.LoadLink(repo, name)
	if err != nil {
		return errs.Wrap(errs.Config, err)
	}
	if link == nil {
		return errs.New(errs.NotFound,
			"workspace %q has no linked session to resume (it wasn't created within a tracked agent session)", name)
	}

	argv, err := resumeArgv(link.Agent, link.SessionID, passthrough)
	if err != nil {
		return err
	}

	if printOnly {
		fmt.Printf("cd %s && %s\n", shellQuote(link.CWD), shellJoin(argv))
		return nil
	}

	if _, err := os.Stat(link.CWD); err != nil {
		return errs.New(errs.NotFound,
			"the session's directory %s no longer exists; cannot resume (workspace is intact at %s)",
			paths.Display(link.CWD), paths.Display(wsPath))
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return errs.New(errs.MissingDep, "%s not found on PATH; is %s installed?", argv[0], link.Agent)
	}

	run := exec.Command(bin, argv[1:]...)
	run.Dir = link.CWD
	run.Stdin = os.Stdin
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Propagate the agent's own exit status.
			return &errs.Coded{Code: exitErr.ExitCode(), Err: fmt.Errorf("%s exited with status %d", link.Agent, exitErr.ExitCode())}
		}
		return errs.Wrap(errs.Config, err)
	}
	return nil
}

// resumeArgv builds the agent invocation (binary + resume flag + session id +
// passthrough) for the recorded agent.
func resumeArgv(agent, sessionID string, passthrough []string) ([]string, error) {
	var base []string
	switch agent {
	case "claude":
		base = []string{"claude", "--resume", sessionID}
	case "opencode":
		base = []string{"opencode", "--session", sessionID}
	case "cursor":
		base = []string{"cursor-agent", "--resume", sessionID}
	default:
		return nil, errs.New(errs.Config, "unknown agent %q recorded for this workspace", agent)
	}
	return append(base, passthrough...), nil
}

// shellJoin renders an argv as a shell-quoted command line for --print output.
func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

// shellQuote single-quotes a string for safe shell reuse, but leaves simple
// tokens (no shell-special characters) unquoted for readability.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
