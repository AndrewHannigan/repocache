package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/workspace"
)

func TestParseWorkspaceNewName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"basic", "shed workspace new octocat/foo my-branch", "my-branch", true},
		{"ws alias", "shed ws new foo fix-bug", "fix-bug", true},
		{"with base flag", "shed workspace new foo feat --base main", "feat", true},
		{"base flag between args", "shed workspace new foo --base main feat", "feat", true},
		{"shell wrapped", "cd /tmp && FOO=1 shed ws new foo task-1", "task-1", true},
		{"abs path binary", "/usr/local/bin/shed workspace new foo task-2", "task-2", true},
		{"nested branch name", "shed workspace new foo feature/x", "feature/x", true},
		{"not a workspace new", "shed ls", "", false},
		{"workspace but not new", "shed workspace ls", "", false},
		{"only one positional", "shed workspace new foo", "", false},
		{"unrelated mentioning phrase", `echo "workspace new stuff"`, "", false},
		{"option-looking name rejected", "shed workspace new foo -evil", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseWorkspaceNewName(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Errorf("parseWorkspaceNewName(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNormalizeHookInput(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantID  string
		wantCmd string
		wantCWD string
	}{
		{
			"claude shape",
			`{"session_id":"s1","cwd":"/a","tool_input":{"command":"shed ws new r b"}}`,
			"s1", "shed ws new r b", "/a",
		},
		{
			"cursor shape",
			`{"conversation_id":"c1","command":"shed ws new r b","workspace_roots":["/w"]}`,
			"c1", "shed ws new r b", "/w",
		},
		{
			"opencode shape",
			`{"sessionID":"ses_1","command":"shed ws new r b","cwd":"/o"}`,
			"ses_1", "shed ws new r b", "/o",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var in hookInput
			if err := json.Unmarshal([]byte(tt.json), &in); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			id, cmd, cwd := in.normalize()
			if id != tt.wantID || cmd != tt.wantCmd || cwd != tt.wantCWD {
				t.Errorf("normalize() = (%q,%q,%q), want (%q,%q,%q)", id, cmd, cwd, tt.wantID, tt.wantCmd, tt.wantCWD)
			}
		})
	}
}

// recordPendingFromHook writes a pending intent for a matching workspace-new
// command, and nothing for a non-matching one.
func TestRecordPendingFromHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	recordPendingFromHook(strings.NewReader(
		`{"session_id":"sess-123","cwd":"/work/dir","tool_input":{"command":"shed workspace new foo my-task"}}`), "claude")

	got, err := workspace.TakePending("my-task")
	if err != nil {
		t.Fatalf("TakePending: %v", err)
	}
	if got == nil {
		t.Fatal("expected a pending intent for my-task, got none")
	}
	if got.Agent != "claude" || got.SessionID != "sess-123" || got.CWD != "/work/dir" {
		t.Errorf("pending = %+v, want agent=claude id=sess-123 cwd=/work/dir", *got)
	}

	// A non-workspace-new command records nothing.
	recordPendingFromHook(strings.NewReader(
		`{"session_id":"x","cwd":"/y","tool_input":{"command":"shed ls"}}`), "claude")
	if p, _ := workspace.TakePending("ls"); p != nil {
		t.Errorf("expected no pending for a non-workspace-new command, got %+v", *p)
	}
}
