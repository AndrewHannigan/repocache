package repostore

import (
	"errors"
	"testing"
)

func TestRequireGit(t *testing.T) {
	t.Run("missing returns ErrGitMissing", func(t *testing.T) {
		// An empty PATH means LookPath can't find git anywhere.
		t.Setenv("PATH", t.TempDir())
		if err := RequireGit(); !errors.Is(err, ErrGitMissing) {
			t.Fatalf("RequireGit() = %v, want ErrGitMissing", err)
		}
	})

	t.Run("present returns nil", func(t *testing.T) {
		// Uses the real PATH (restored after the previous subtest). Both CI
		// and dev environments have git; skip rather than fail if somehow not.
		if err := RequireGit(); err != nil {
			t.Skipf("git not on PATH in test environment: %v", err)
		}
	})
}
