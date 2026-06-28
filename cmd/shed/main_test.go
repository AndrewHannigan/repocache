package main

import (
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// The init gate exempts exactly the commands that must run before `shed init`:
// init itself (it does the bootstrapping), help (docs on a fresh install), and
// the hidden __* hook commands (invoked by agents, sometimes before any manual
// init). Every other command is gated.
func TestInitExempt(t *testing.T) {
	root := newRootCmd()

	exempt := []string{"init", "help", "__bg-sync", "__on-tool-call", "__session-context", "__welcome-tour"}
	for _, name := range exempt {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if !initExempt(cmd) {
			t.Errorf("%q should be exempt from the init gate", name)
		}
	}

	gated := []string{"add", "rm", "ls", "sync", "status", "prune", "history", "resume"}
	for _, name := range gated {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if initExempt(cmd) {
			t.Errorf("%q must be gated on init, not exempt", name)
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
