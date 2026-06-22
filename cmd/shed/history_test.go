package main

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/shed/pkg/history"
	"github.com/AndrewHannigan/shed/pkg/paths"
)

// findCmd resolves a command path (e.g. "workspace", "new") against a real root
// command, so shouldRecord is exercised through the actual command tree.
func findCmd(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	root := newRootCmd()
	c, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("Find(%v): %v", path, err)
	}
	return c
}

func TestShouldRecord(t *testing.T) {
	record := [][]string{
		{"add"}, {"rm"}, {"gc"}, {"init"}, {"uninstall"},
		{"workspace", "new"}, {"workspace", "rm"},
	}
	skip := [][]string{
		{"sync"}, {"ls"}, {"status"}, {"history"},
		{"workspace", "ls"}, {"workspace", "path"},
		{"__bg-sync"}, {"__session-context"},
		{}, // bare root
	}
	for _, p := range record {
		if !shouldRecord(findCmd(t, p...)) {
			t.Errorf("shouldRecord(%v) = false, want true", p)
		}
	}
	for _, p := range skip {
		if shouldRecord(findCmd(t, p...)) {
			t.Errorf("shouldRecord(%v) = true, want false", p)
		}
	}
}

// recentHistoryText is empty when nothing is recorded, and renders a neutral
// fenced section listing recent commands when there is history.
func TestRecentHistoryText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(paths.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}

	if got := recentHistoryText(); got != "" {
		t.Errorf("empty history should render nothing, got:\n%s", got)
	}

	for _, args := range [][]string{
		{"add", "octocat/Hello-World"},
		{"workspace", "new", "shed", "feat-x"},
	} {
		if err := history.Record(args); err != nil {
			t.Fatal(err)
		}
	}

	out := recentHistoryText()
	for _, want := range []string{
		"recent `shed` commands",
		"shed add octocat/Hello-World",
		"shed workspace new shed feat-x",
		"```",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recentHistoryText missing %q:\n%s", want, out)
		}
	}
}

// The session-context body appends the recent-history section when there is
// history, and omits it (no dangling header) when there is none.
func TestSessionContextBodyIncludesHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty library: no `ls` snapshot
	if err := os.MkdirAll(paths.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(sessionContextBody(), "recent `shed` commands") {
		t.Errorf("body should have no history section when nothing is recorded")
	}

	if err := history.Record([]string{"workspace", "new", "shed", "feat-x"}); err != nil {
		t.Fatal(err)
	}
	body := sessionContextBody()
	if !strings.Contains(body, "recent `shed` commands") ||
		!strings.Contains(body, "shed workspace new shed feat-x") {
		t.Errorf("body should include the recent-history section:\n%s", body)
	}
}
