package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/AndrewHannigan/shed/pkg/workspace"
)

// writeLibrary renders a captioned section per kind of thing shed manages, so a
// newcomer can tell what each table is.
func TestWriteLibraryCaptionedSections(t *testing.T) {
	owners := []ownerRow{{Name: "octocat", RepoCount: 3}}
	repos := []repoRow{{
		Name:       "github.com/octocat/Hello-World",
		Source:     "octocat",
		LastSyncAt: time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339),
	}}
	workspaces := []workspace.Info{{
		Name: "github.com/octocat/Hello-World", Branch: "fix-typo",
		Path: "/home/u/.shed/workspaces/x", Dirty: true, Unpushed: 2, Age: time.Now(),
	}}

	var buf bytes.Buffer
	if err := writeLibrary(&buf, owners, repos, workspaces, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"Owners", "OWNER", "octocat",
		"Repos", "NAME", "LAST SYNC", "github.com/octocat/Hello-World",
		"Workspaces", "BRANCH", "fix-typo", "DIRTY", "UNPUSHED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// A repo auto-added by an owner shows the ADDED BY column.
	if !strings.Contains(out, "ADDED BY") {
		t.Errorf("expected ADDED BY column when a repo has a source:\n%s", out)
	}
}

// The ADDED BY column is hidden when no repo was auto-added by an owner, so the
// common no-owners case isn't cluttered with a column of em-dashes.
func TestWriteLibraryHidesSourceColumnWhenUnused(t *testing.T) {
	repos := []repoRow{{
		Name:       "github.com/acme/widget",
		LastSyncAt: time.Now().UTC().Format(time.RFC3339),
	}}
	var buf bytes.Buffer
	if err := writeLibrary(&buf, nil, repos, nil, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "ADDED BY") {
		t.Errorf("ADDED BY column should be hidden when no repo has a source:\n%s", out)
	}
	// With no owners and no workspaces, only the Repos section appears.
	if strings.Contains(out, "Owners\n") || strings.Contains(out, "Workspaces\n") {
		t.Errorf("empty owners/workspaces sections should be omitted:\n%s", out)
	}
}

// With repos but no workspaces, the interactive hint (workspaceHint=true) keeps
// the Workspaces section and explains how to create one; the session-context
// path (workspaceHint=false) omits it to stay terse.
func TestWriteLibraryWorkspaceHint(t *testing.T) {
	repos := []repoRow{{Name: "github.com/acme/widget", LastSyncAt: time.Now().UTC().Format(time.RFC3339)}}

	var withHint bytes.Buffer
	if err := writeLibrary(&withHint, nil, repos, nil, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withHint.String(), "Workspaces\n") || !strings.Contains(withHint.String(), "workspace new") {
		t.Errorf("expected workspaces section + creation hint:\n%s", withHint.String())
	}

	var noHint bytes.Buffer
	if err := writeLibrary(&noHint, nil, repos, nil, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(noHint.String(), "Workspaces") {
		t.Errorf("workspaces section should be omitted with no workspaces and hint off:\n%s", noHint.String())
	}
}

// An entirely empty shed shows a single actionable line, not empty headers.
func TestWriteLibraryEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := writeLibrary(&buf, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "nothing tracked yet") {
		t.Errorf("expected empty-shed hint:\n%s", out)
	}
	if strings.Contains(out, "Owners") || strings.Contains(out, "Repos") || strings.Contains(out, "Workspaces") {
		t.Errorf("empty shed should not render section headers:\n%s", out)
	}
}
