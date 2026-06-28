package repostore

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneFetchProgress verifies that passing a progress writer streams git's
// meter through it, while passing nil stays silent — the two modes Clone/Fetch
// switch between for a single interactive sync vs. a parallel/piped one.
func TestCloneFetchProgress(t *testing.T) {
	if err := RequireGit(); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	t.Setenv("HOME", t.TempDir())

	// A tiny source repo to clone from, with one commit so there are objects to
	// receive (git prints no "Receiving objects" line for an empty remote).
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	// -b main keeps the default branch deterministic across init.defaultBranch.
	runGit(t, src, "init", "-b", "main")
	runGit(t, src, "config", "user.email", "t@t")
	runGit(t, src, "config", "user.name", "t")
	commit(t, src, "README", "hi\n", "init")

	name := "github.com/foo/bar"
	// A file:// URL forces git's real transport instead of the local hardlink
	// fast path, so the clone actually counts/receives objects and prints the
	// meter we want to prove is streamed (a plain local-path clone skips it).
	url := "file://" + src

	t.Run("progress writer receives the meter", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Clone(url, name, &buf); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		// "Receiving objects" appears only with --progress on a non-TTY transfer,
		// so it doubles as proof the flag is being passed when progress != nil.
		if got := buf.String(); !strings.Contains(got, "Receiving objects") {
			t.Fatalf("progress writer got %q, want it to mention receiving objects", got)
		}
	})

	t.Run("nil progress stays quiet but still succeeds", func(t *testing.T) {
		commit(t, src, "README", "hi again\n", "more")
		// With nil the stderr goes nowhere; the contract we can assert is that the
		// quiet path (the old CombinedOutput behavior) still fetches cleanly.
		if err := Fetch(name, nil); err != nil {
			t.Fatalf("Fetch(nil): %v", err)
		}
	})

	t.Run("fetch streams to the progress writer", func(t *testing.T) {
		commit(t, src, "README", "hi once more\n", "again")
		var buf bytes.Buffer
		if err := Fetch(name, &buf); err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		// A fetch carrying new objects writes a meter; require only that something
		// reached the writer so the test isn't brittle across git versions that
		// word it differently.
		if buf.Len() == 0 {
			t.Fatal("progress writer got nothing from fetch, want git's meter output")
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
	}
}

func commit(t *testing.T, dir, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", msg)
}
