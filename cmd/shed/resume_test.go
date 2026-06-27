package main

import (
	"strings"
	"testing"
)

func TestResumeArgv(t *testing.T) {
	tests := []struct {
		name        string
		agent       string
		passthrough []string
		want        string
		wantErr     bool
	}{
		{"claude", "claude", nil, "claude --resume id1", false},
		{"opencode", "opencode", nil, "opencode --session id1", false},
		{"cursor", "cursor", nil, "cursor-agent --resume id1", false},
		{"with passthrough", "claude", []string{"-p", "go on"}, "claude --resume id1 -p go on", false},
		{"unknown agent", "bogus", nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resumeArgv(tt.agent, "id1", tt.passthrough)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resumeArgv err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if strings.Join(got, " ") != tt.want {
				t.Errorf("resumeArgv = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"/home/u/dir", "/home/u/dir"},
		{"with space", "'with space'"},
		{"it's", `'it'\''s'`},
		{"a&b", "'a&b'"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
