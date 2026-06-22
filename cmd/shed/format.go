package main

// Shared presentation helpers for human-readable command output. Kept here
// rather than inside any one command's file because they are used across
// add/rm/ls, sync, status, workspace, prune, and session-context.

import (
	"fmt"
	"time"
)

// pluralize renders "1 repo" / "3 repos" for a count and singular noun.
func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// joinAnd renders a slice as "a", "a and b", or "a, b and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return parts[0] + ", " + joinAnd(parts[1:])
	}
}

// humanSize renders a byte count as B/KB/MB/GB.
func humanSize(b int64) string {
	const k = 1024.0
	switch {
	case b < int64(k):
		return fmt.Sprintf("%d B", b)
	case b < int64(k*k):
		return fmt.Sprintf("%.1f KB", float64(b)/k)
	case b < int64(k*k*k):
		return fmt.Sprintf("%.1f MB", float64(b)/(k*k))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(k*k*k))
	}
}

// formatMs renders a millisecond duration as "850ms" / "1.2s".
func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// relTime renders an absolute time as a relative phrase ("3 hr ago"),
// or "never" for the zero time and "just now" for under a minute.
func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// relDuration renders a bare duration ("45s", "3 hr") with no "ago" suffix —
// for phrases like "synced 3 hr ago" where the caller supplies the framing.
func relDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hr", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
