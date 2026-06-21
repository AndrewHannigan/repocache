package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/agents"
	"github.com/AndrewHannigan/repocache/pkg/config"
)

func TestPrintSessionContext(t *testing.T) {
	var buf bytes.Buffer
	if err := printSessionContext(&buf); err != nil {
		t.Fatalf("printSessionContext: %v", err)
	}

	// Must be valid JSON in the envelope all three agents accept.
	var env sessionContextEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if env.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", env.HookSpecificOutput.HookEventName)
	}
	// The body leads with the embedded guide, then appends a live library
	// snapshot (which may be empty in a clean test environment).
	if !strings.HasPrefix(env.HookSpecificOutput.AdditionalContext, string(agents.DocContent)) {
		t.Errorf("additionalContext should start with the embedded guide")
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("output should be newline-terminated")
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
