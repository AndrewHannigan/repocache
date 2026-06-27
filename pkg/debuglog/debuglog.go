// Package debuglog provides shed's opt-in diagnostic logger. It is a no-op
// until Enable is called — driven by settings.debug_mode in config.toml — after
// which Log appends timestamped, structured records to ~/.shed/logs/debug.log.
//
// The logger is process-global on purpose: logging is a cross-cutting concern,
// and a global lets any package trace what it's doing with a bare
// debuglog.Log(...) call instead of threading a logger through every signature.
// When debug_mode is off, Log returns immediately, so those call sites cost
// nothing and need no guard.
package debuglog

import (
	"log/slog"
	"os"
	"sync"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// logMaxBytes caps the debug log: at the cap Enable rotates it to a single
// backup so it can't grow without bound across sessions. Matches the bg-sync
// log's rotation policy (see cmd/shed/bg_sync.go).
const logMaxBytes = 5 << 20 // 5 MB

var (
	mu     sync.Mutex
	logger *slog.Logger // nil => disabled; Log is a no-op
)

// Enable opens (rotating first if oversized) ~/.shed/logs/debug.log and installs
// a structured logger that appends to it. It returns an error only when the log
// file cannot be opened; the caller decides whether to surface it. shed keeps
// working regardless — debug logging is a diagnostic aid, never a dependency.
func Enable() error {
	f, err := openLog()
	if err != nil {
		return err
	}
	mu.Lock()
	logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mu.Unlock()
	return nil
}

// Enabled reports whether debug logging is currently active.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return logger != nil
}

// Log appends one debug record: msg followed by optional slog-style key/value
// pairs. It is a no-op when debug logging is disabled, so call sites stay
// unconditional. Safe for concurrent use.
func Log(msg string, args ...any) {
	mu.Lock()
	l := logger
	mu.Unlock()
	if l == nil {
		return
	}
	l.Debug(msg, args...)
}

// openLog opens the debug log for appending, creating the logs dir and rotating
// an oversized existing log to <log>.1 first.
func openLog() (*os.File, error) {
	if err := os.MkdirAll(paths.LogsDir(), 0755); err != nil {
		return nil, err
	}
	path := paths.DebugLogFile()
	if fi, err := os.Stat(path); err == nil && fi.Size() >= logMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}
