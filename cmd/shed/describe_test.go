package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/config"
	"github.com/AndrewHannigan/shed/pkg/errs"
)

// ptr is a tiny helper so the table tests can pass a *string for "set this
// description" while nil means "show the current one".
func ptr(s string) *string { return &s }

// describe sets a repo's description, a later describe with a new value
// overwrites it, and --clear (modeled as the empty string) removes it — each
// change surviving a reload from disk.
func TestRunDescribeSetOverwriteClear(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget"}},
	}); err != nil {
		t.Fatal(err)
	}

	// A suffix resolves to the repo, just like every other command.
	if err := runDescribe("widget", ptr("the widget service")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := loadDesc(t, "github.com/acme/widget"); got != "the widget service" {
		t.Fatalf("after set, description = %q", got)
	}

	// Setting again overwrites; surrounding whitespace is trimmed.
	if err := runDescribe("widget", ptr("  now something else  ")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := loadDesc(t, "github.com/acme/widget"); got != "now something else" {
		t.Fatalf("after overwrite, description = %q", got)
	}

	// The empty string clears it (what --clear passes).
	if err := runDescribe("widget", ptr("")); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := loadDesc(t, "github.com/acme/widget"); got != "" {
		t.Fatalf("after clear, description = %q, want empty", got)
	}
}

// Showing a description (desc == nil) prints the stored value, and prints a
// set-one hint when there's none — without mutating the config either way.
func TestRunDescribeShow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget", Description: "the widget service"}},
	}); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runDescribe("widget", nil); err != nil {
			t.Fatalf("show: %v", err)
		}
	})
	if !strings.Contains(out, "the widget service") {
		t.Errorf("show should print the description, got:\n%s", out)
	}

	// Clear it, then showing prints the hint, not a blank line.
	if err := runDescribe("widget", ptr("")); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := runDescribe("widget", nil); err != nil {
			t.Fatalf("show empty: %v", err)
		}
	})
	if !strings.Contains(out, "no description") {
		t.Errorf("show with no description should hint how to set one, got:\n%s", out)
	}
}

// An over-long description is rejected before the config is touched, so a bad
// value can never be persisted.
func TestRunDescribeRejectsOverlong(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget"}},
	}); err != nil {
		t.Fatal(err)
	}
	err := runDescribe("widget", ptr(strings.Repeat("x", config.MaxDescriptionLen+1)))
	if err == nil {
		t.Fatal("expected an error for an over-long description")
	}
	if got := loadDesc(t, "github.com/acme/widget"); got != "" {
		t.Fatalf("a rejected description must not be saved, got %q", got)
	}
}

// Describing a repo that isn't tracked is a NotFound, the same code every other
// repo-resolving command returns.
func TestRunDescribeUnknownRepo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{
		Repos: []config.Repo{{URL: "https://github.com/acme/widget"}},
	}); err != nil {
		t.Fatal(err)
	}
	err := runDescribe("nope", ptr("x"))
	var coded *errs.Coded
	if !errors.As(err, &coded) || coded.Code != errs.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

// loadDesc reloads the config from disk and returns the named repo's
// description, failing the test if the repo is missing.
func loadDesc(t *testing.T, name string) string {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := c.FindByName(name)
	if r == nil {
		t.Fatalf("repo %q not found in config", name)
	}
	return r.Description
}
