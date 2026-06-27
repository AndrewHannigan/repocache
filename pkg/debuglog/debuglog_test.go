package debuglog

import (
	"os"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

func TestLogDisabledIsNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetForTest(t)
	// Logging before Enable must not panic, and must not create the log file.
	Log("should-not-write", "k", "v")
	if Enabled() {
		t.Fatal("Enabled() = true before Enable()")
	}
	if _, err := os.Stat(paths.DebugLogFile()); !os.IsNotExist(err) {
		t.Fatalf("debug log created while disabled: %v", err)
	}
}

func TestEnableWritesRecords(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetForTest(t)
	if err := Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !Enabled() {
		t.Fatal("Enabled() = false after Enable()")
	}
	Log("hello", "name", "world")
	data, err := os.ReadFile(paths.DebugLogFile())
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "msg=hello") || !strings.Contains(got, "name=world") {
		t.Fatalf("log missing record, got: %q", got)
	}
}

func TestEnableRotatesOversizedLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resetForTest(t)
	if err := os.MkdirAll(paths.LogsDir(), 0755); err != nil {
		t.Fatal(err)
	}
	path := paths.DebugLogFile()
	if err := os.WriteFile(path, make([]byte, logMaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated backup at %s: %v", path+".1", err)
	}
}

// resetForTest clears the process-global logger so each test starts disabled,
// independent of test ordering.
func resetForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	logger = nil
	mu.Unlock()
}
