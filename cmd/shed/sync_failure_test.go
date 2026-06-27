package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AndrewHannigan/shed/pkg/paths"
	"github.com/AndrewHannigan/shed/pkg/repostore"
)

// TestFinishErrPersistsFailure verifies a failed sync records the error in the
// meta sidecar while preserving the last *successful* sync time, and that the
// next successful sync clears the recorded error.
func TestFinishErrPersistsFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	const name = "github.com/acme/widget"
	if err := os.MkdirAll(paths.RepoStorePath(name), 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate a prior successful sync.
	lastSync := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if err := repostore.SaveMeta(name, &repostore.Meta{LastSyncAt: lastSync}); err != nil {
		t.Fatal(err)
	}

	// A failed sync should persist the error but keep LastSyncAt untouched.
	finishErr(syncResult{Name: name}, time.Now(), errors.New("fetch: connection refused"))
	m, err := repostore.LoadMeta(name)
	if err != nil || m == nil {
		t.Fatalf("load meta: %v", err)
	}
	if m.LastError == "" {
		t.Fatal("expected LastError to be persisted on a failed sync")
	}
	if !m.LastSyncAt.Equal(lastSync) {
		t.Fatalf("LastSyncAt should be preserved on failure: got %v want %v", m.LastSyncAt, lastSync)
	}

	// A subsequent success writes a fresh Meta, which clears the error.
	if err := repostore.SaveMeta(name, &repostore.Meta{LastSyncAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	m, _ = repostore.LoadMeta(name)
	if m.LastError != "" {
		t.Fatalf("expected LastError cleared on success, got %q", m.LastError)
	}
}

// TestFinishErrFirstCloneRecordsStandalone verifies a failure before the store
// exists (a failed first clone) writes no meta sidecar — there's no store dir
// for one — but does record the error in the standalone first-sync store so
// status and the staleness banner still surface it instead of reporting the
// repo healthy.
func TestFinishErrFirstCloneRecordsStandalone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	const name = "github.com/acme/never-cloned"
	finishErr(syncResult{Name: name}, time.Now(), errors.New("authentication failed"))

	// No store dir means no meta sidecar.
	if m, _ := repostore.LoadMeta(name); m != nil {
		t.Fatalf("expected no meta written when store dir absent, got %+v", m)
	}
	// The standalone store must hold the failure.
	fe, err := repostore.LoadFirstSyncError(name)
	if err != nil || fe == nil {
		t.Fatalf("load first-sync error: %v (record=%+v)", err, fe)
	}
	if fe.LastError == "" || fe.LastErrorAt.IsZero() {
		t.Fatalf("expected first-sync error recorded with a timestamp, got %+v", fe)
	}

	// A later successful sync clears the standalone record.
	repostore.ClearFirstSyncError(name)
	if fe, _ := repostore.LoadFirstSyncError(name); fe != nil {
		t.Fatalf("expected first-sync error cleared, got %+v", fe)
	}
}

// TestRepoListMarksFailure verifies the table annotates a repo whose last
// attempt failed, without hiding the last successful sync time and without
// marking healthy repos.
func TestRepoListMarksFailure(t *testing.T) {
	rows := []repoRow{
		{Name: "github.com/acme/ok", LastSyncAt: time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)},
		{Name: "github.com/acme/bad", LastSyncAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339), LastError: "fetch failed"},
	}
	var buf bytes.Buffer
	if err := writeRepoTable(&buf, rows, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "sync failing") {
		t.Fatalf("expected failure marker in table:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "/ok") && strings.Contains(line, "sync failing") {
			t.Fatalf("healthy repo wrongly marked:\n%s", line)
		}
	}
}
