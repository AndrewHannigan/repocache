package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AndrewHannigan/shed/pkg/cache"
	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
	"github.com/AndrewHannigan/shed/pkg/paths"
)

// mkMeta creates a cache repo dir and writes its meta sidecar. Requires an
// isolated HOME (t.Setenv) so it never touches the real library.
func mkMeta(t *testing.T, name string, m cache.Meta) {
	t.Helper()
	if err := os.MkdirAll(paths.CacheRepoPath(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveMeta(name, &m); err != nil {
		t.Fatal(err)
	}
}

func TestLikelyCause(t *testing.T) {
	cases := []struct{ name, err, want string }{
		{"auth", "git fetch: exit status 128 (output: fatal: could not read Username for 'https://github.com')", "gh auth login"},
		{"notfound", "git fetch: exit status 128 (output: ERROR: Repository not found.)", "shed rm"},
		{"network", "git fetch: exit status 128 (output: fatal: unable to access: Could not resolve host: github.com)", "network"},
		{"locked", "locked", "lock"},
		{"disk", "fatal: write error: No space left on device", "disk full"},
		{"unknown", "git fetch: exit status 1 (output: something unexpected)", "see the full output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := likelyCause(tc.err, "github.com/acme/widget")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("likelyCause(%q) = %q, want substring %q", tc.err, got, tc.want)
			}
		})
	}
}

// status on an unknown repo must exit 2 (NotFound), consistent with every
// other repo-resolving command — not the config code 7.
func TestStatusRepoUnknownExitsNotFound(t *testing.T) {
	c := &config.Config{Repos: []config.Repo{{URL: "https://github.com/foo/bar"}}}
	err := runStatusRepo(c, "nope")
	if err == nil {
		t.Fatal("unknown repo should error")
	}
	var coded *errs.Coded
	if !errors.As(err, &coded) || coded.Code != errs.NotFound {
		t.Fatalf("want exit %d (NotFound), got %v", errs.NotFound, err)
	}
}

func TestCollectSyncFailuresOrdersNewestFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Repos: []config.Repo{
		{URL: "https://github.com/acme/ok"},
		{URL: "https://github.com/acme/old-fail"},
		{URL: "https://github.com/acme/new-fail"},
	}}
	mkMeta(t, "github.com/acme/ok", cache.Meta{LastSyncAt: time.Now()})
	mkMeta(t, "github.com/acme/old-fail", cache.Meta{
		LastSyncAt: time.Now().Add(-48 * time.Hour), LastError: "boom", LastErrorAt: time.Now().Add(-10 * time.Hour)})
	mkMeta(t, "github.com/acme/new-fail", cache.Meta{
		LastSyncAt: time.Now().Add(-2 * time.Hour), LastError: "boom", LastErrorAt: time.Now().Add(-1 * time.Hour)})

	got := collectSyncFailures(c)
	if len(got) != 2 {
		t.Fatalf("want 2 failures, got %d: %+v", len(got), got)
	}
	if got[0].Name != "github.com/acme/new-fail" {
		t.Fatalf("want newest failure first, got %q", got[0].Name)
	}
}

func TestSyncHealthBanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := &config.Config{Repos: []config.Repo{
		{URL: "https://github.com/acme/ok"},
		{URL: "https://github.com/acme/broken"},
	}}
	if err := config.Save(c); err != nil {
		t.Fatal(err)
	}
	mkMeta(t, "github.com/acme/ok", cache.Meta{LastSyncAt: time.Now()})
	mkMeta(t, "github.com/acme/broken", cache.Meta{LastSyncAt: time.Now()})

	if b := syncHealthBanner(); b != "" {
		t.Fatalf("expected no banner when all healthy, got:\n%s", b)
	}

	mkMeta(t, "github.com/acme/broken", cache.Meta{
		LastSyncAt: time.Now().Add(-3 * time.Hour), LastError: "git fetch: boom", LastErrorAt: time.Now()})
	b := syncHealthBanner()
	if !strings.Contains(b, "STALE CACHE") || !strings.Contains(b, "1 of 2") {
		t.Fatalf("banner missing expected summary:\n%s", b)
	}
	if !strings.Contains(b, "github.com/acme/broken") {
		t.Fatalf("banner should name the failing repo:\n%s", b)
	}
	if strings.Contains(b, "github.com/acme/ok") {
		t.Fatalf("healthy repo should not appear in banner:\n%s", b)
	}
}

func TestWrapIndent(t *testing.T) {
	lines := wrapIndent("aaaa bbbb cccc dddd", "  ", 12)
	if len(lines) != 2 {
		t.Fatalf("want 2 wrapped lines, got %d: %q", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "  ") {
			t.Fatalf("line missing indent: %q", l)
		}
	}
}
