package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/agents"
	"github.com/AndrewHannigan/repocache/pkg/config"
)

// The envelope agents (claude, antigravity) get the JSON envelope, wrapped in
// <repocache-session-context> tags, carrying the guide as additionalContext.
func TestPrintSessionContextEnvelopeAgents(t *testing.T) {
	for _, agent := range []string{"claude", "antigravity"} {
		t.Run(agent, func(t *testing.T) {
			// Isolate from the real user config so the snapshot and the
			// collision-detection both see an empty library.
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			var buf bytes.Buffer
			if err := printSessionContext(&buf, agent); err != nil {
				t.Fatalf("printSessionContext: %v", err)
			}

			// Output is wrapped in <repocache-session-context>...</> tags so it
			// can be extracted unambiguously from surrounding hook output.
			out := strings.TrimSuffix(buf.String(), "\n")
			if !strings.HasPrefix(out, "<repocache-session-context>") || !strings.HasSuffix(out, "</repocache-session-context>") {
				t.Fatalf("output should be wrapped in <repocache-session-context> tags:\n%s", buf.String())
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(out, "<repocache-session-context>"), "</repocache-session-context>")

			// The wrapped content must be the valid JSON envelope these agents accept.
			var env struct {
				HookSpecificOutput struct {
					HookEventName     string `json:"hookEventName"`
					AdditionalContext string `json:"additionalContext"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal([]byte(inner), &env); err != nil {
				t.Fatalf("wrapped output is not valid JSON: %v\n%s", err, inner)
			}
			if env.HookSpecificOutput.HookEventName != "SessionStart" {
				t.Errorf("hookEventName = %q, want SessionStart", env.HookSpecificOutput.HookEventName)
			}
			// The body leads with the embedded guide, then appends a live
			// library snapshot (which may be empty in a clean test environment).
			if !strings.HasPrefix(env.HookSpecificOutput.AdditionalContext, string(agents.DocContent)) {
				t.Errorf("additionalContext should start with the embedded guide")
			}
			if !strings.HasSuffix(buf.String(), "\n") {
				t.Errorf("output should be newline-terminated")
			}
		})
	}
}

// The plain-text agents (codex, opencode) get the raw Markdown body — no
// envelope, no delimiter tags. Codex injects plain stdout as developer
// context; opencode's plugin pushes the text into the system prompt directly.
func TestPrintSessionContextPlainTextAgents(t *testing.T) {
	for _, agent := range []string{"codex", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			var buf bytes.Buffer
			if err := printSessionContext(&buf, agent); err != nil {
				t.Fatalf("printSessionContext: %v", err)
			}
			out := buf.String()
			if strings.Contains(out, "<repocache-session-context>") || strings.Contains(out, "hookSpecificOutput") {
				t.Errorf("%s output must be raw body, not the hook envelope:\n%s", agent, out)
			}
			if !strings.HasPrefix(out, string(agents.DocContent)) {
				t.Errorf("%s output should start with the embedded guide:\n%s", agent, out)
			}
			if !strings.HasSuffix(out, "\n") {
				t.Errorf("output should be newline-terminated")
			}
		})
	}
}

// An unknown --agent value is a clear error, not a silent default.
func TestPrintSessionContextUnknownAgent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var buf bytes.Buffer
	if err := printSessionContext(&buf, "nope"); err == nil {
		t.Errorf("expected error for unknown agent, got output:\n%s", buf.String())
	}
}

// With a configured library, the body appends a live `repo list` snapshot.
func TestSessionContextBodyIncludesLibrary(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := sessionContextBody()
	if !strings.HasPrefix(body, string(agents.DocContent)) {
		t.Fatalf("body should start with the embedded guide")
	}
	for _, want := range []string{"repocache repo list", "NAME", "acme/widget"} {
		if !strings.Contains(body, want) {
			t.Errorf("body should contain %q\n%s", want, body)
		}
	}
}

// With no library configured, the body is just the guide (no snapshot noise).
func TestSessionContextBodyEmptyLibrary(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	body := sessionContextBody()
	if !strings.HasPrefix(body, string(agents.DocContent)) {
		t.Fatalf("body should start with the embedded guide")
	}
	if strings.Contains(body, "repocache repo list`):") {
		t.Errorf("empty library should not append a snapshot section\n%s", body)
	}
}

// collisionWarning fires when the working directory's origin matches a library
// repo, regardless of URL protocol, and names the repo for `workspace new`.
func TestCollisionWarning(t *testing.T) {
	repos := []config.Repo{
		{URL: "git@github.com:octocat/hello-world.git"}, // ssh form
		{URL: "https://github.com/acme/widget"},
	}

	// https working-dir origin matches the ssh-form library entry.
	w := collisionWarning("/home/u/src/hello-world", "https://github.com/octocat/hello-world.git", repos)
	for _, want := range []string{
		"local checkout collision",
		"/home/u/src/hello-world",
		"workspace new github.com/octocat/hello-world <branch>",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("warning missing %q:\n%s", want, w)
		}
	}

	// A repo not in the library produces no warning.
	if w := collisionWarning("/home/u/src/other", "https://github.com/octocat/other", repos); w != "" {
		t.Errorf("expected no warning for unlisted repo, got:\n%s", w)
	}

	// The workspace command uses the library's resolved (custom) name.
	named := []config.Repo{{URL: "https://github.com/octocat/hello-world", Name: "myrepo"}}
	if w := collisionWarning("/x", "https://github.com/octocat/hello-world", named); !strings.Contains(w, "workspace new myrepo <branch>") {
		t.Errorf("warning should use the resolved library name:\n%s", w)
	}
}
