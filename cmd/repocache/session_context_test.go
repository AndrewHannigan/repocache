package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/agents"
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
	if env.HookSpecificOutput.AdditionalContext != string(agents.DocContent) {
		t.Errorf("additionalContext does not match the embedded guide")
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("output should be newline-terminated")
	}
}
