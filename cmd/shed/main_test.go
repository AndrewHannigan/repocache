package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// The init gate exempts exactly the commands that must run before `shed init`:
// init itself (it does the bootstrapping), help (docs on a fresh install), and
// the hidden __* hook commands (invoked by agents, sometimes before any manual
// init). Every other command is gated. Commands are built directly rather than
// via root.Find because cobra adds the help command lazily during Execute.
func TestInitExempt(t *testing.T) {
	exempt := []*cobra.Command{
		newInitCmd(),
		newHelpCmd(),
		newBgSyncCmd(),
		newOnToolCallCmd(),
		newSessionContextCmd(),
		newWelcomeTourCmd(),
	}
	for _, c := range exempt {
		if !initExempt(c) {
			t.Errorf("%q should be exempt from the init gate", c.Name())
		}
	}

	gated := []*cobra.Command{
		newAddCmd(),
		newRmCmd(),
		newLsCmd(),
		newSyncCmd(),
		newStatusCmd(),
		newPruneCmd(),
		newHistoryCmd(),
		newResumeCmd(),
	}
	for _, c := range gated {
		if initExempt(c) {
			t.Errorf("%q must be gated on init, not exempt", c.Name())
		}
	}
}

// A real command run before `shed init` fails fast with a message pointing at
// `shed init`, instead of silently behaving like an empty library.
func TestInitGateBlocksUninitialized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if paths.Initialized() {
		t.Fatal("a fresh temp env should not look initialized")
	}

	root := newRootCmd()
	root.SetArgs([]string{"ls"})
	err := root.Execute()
	if err == nil {
		t.Fatal("`shed ls` before init should be blocked by the gate")
	}
	if !strings.Contains(err.Error(), "shed init") {
		t.Errorf("gate error should point at `shed init`, got: %v", err)
	}
}

// Once the config file and data dir init creates exist, the gate lets a command
// through (the help command, exempt by name, is unaffected either way).
func TestInitGateAllowsAfterInit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// runInit is the real bootstrap; suppress its progress output.
	var initErr error
	captureStdout(t, func() { initErr = runInit() })
	if initErr != nil {
		t.Fatalf("runInit: %v", initErr)
	}
	if !paths.Initialized() {
		t.Fatal("after runInit the env should look initialized")
	}

	root := newRootCmd()
	root.SetArgs([]string{"ls"})
	if err := root.Execute(); err != nil {
		t.Fatalf("`shed ls` after init should pass the gate, got: %v", err)
	}
}
