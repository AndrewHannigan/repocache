package config

import (
	"errors"
	"testing"

	"github.com/AndrewHannigan/shed/pkg/errs"
)

func TestResolveOwner(t *testing.T) {
	c := &Config{Owners: []Owner{
		{URL: "https://github.com/octocat"},
		{URL: "https://gitlab.com/acme"},
	}}
	tests := []struct {
		name     string
		arg      string
		wantURL  string
		wantCode int
	}{
		{name: "exact full name", arg: "github.com/octocat", wantURL: "https://github.com/octocat"},
		{name: "unambiguous suffix", arg: "acme", wantURL: "https://gitlab.com/acme"},
		{name: "no match", arg: "nope", wantCode: errs.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := c.ResolveOwner(tt.arg)
			if tt.wantCode != 0 {
				var coded *errs.Coded
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("want code %d, got %v", tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if o.URL != tt.wantURL {
				t.Fatalf("got %q, want %q", o.URL, tt.wantURL)
			}
		})
	}
}

func TestValidateRejectsOwnerRepoNameClash(t *testing.T) {
	c := &Config{
		Repos:  []Repo{{URL: "https://github.com/acme/widgets"}},
		Owners: []Owner{{URL: "https://github.com/acme", Name: "github.com/acme/widgets"}},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for name shared by a repo and an owner")
	}
}

func TestReposForOwner(t *testing.T) {
	owner := "github.com/acme"
	c := &Config{Repos: []Repo{
		{URL: "https://github.com/acme/a", Source: owner},
		{URL: "https://github.com/acme/b", Source: owner},
		{URL: "https://github.com/someone/c"}, // user-added, no source
	}}
	got := c.ReposForOwner(owner)
	want := []string{"github.com/acme/a", "github.com/acme/b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ReposForOwner = %v, want %v", got, want)
	}
}

func TestSourceAndOwnerRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	in := &Config{
		Repos:  []Repo{{URL: "https://github.com/acme/a", Source: "github.com/acme"}},
		Owners: []Owner{{URL: "https://github.com/acme", Visibility: "public"}},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Owners) != 1 || out.Owners[0].URL != "https://github.com/acme" || out.Owners[0].Visibility != "public" {
		t.Fatalf("owner did not round-trip: %+v", out.Owners)
	}
	if len(out.Repos) != 1 || out.Repos[0].Source != "github.com/acme" {
		t.Fatalf("repo source did not round-trip: %+v", out.Repos)
	}
}

func TestResolve(t *testing.T) {
	c := &Config{Repos: []Repo{
		{URL: "https://github.com/octocat/hello-world"},
		{URL: "https://github.com/octocat/whiteboard"},
		{URL: "https://gitlab.com/other/hello-world"},
		{URL: "git@github.com:foo/bar.git", Name: "myorg/bar"},
	}}

	tests := []struct {
		name     string
		arg      string
		wantURL  string // non-empty => expect this repo's URL
		wantCode int    // non-zero => expect an errs.Coded with this code
	}{
		{name: "exact full name", arg: "github.com/octocat/whiteboard",
			wantURL: "https://github.com/octocat/whiteboard"},
		{name: "exact name override", arg: "myorg/bar", wantURL: "git@github.com:foo/bar.git"},
		{name: "unambiguous suffix", arg: "whiteboard",
			wantURL: "https://github.com/octocat/whiteboard"},
		{name: "two-segment suffix", arg: "octocat/hello-world",
			wantURL: "https://github.com/octocat/hello-world"},
		{name: "ambiguous suffix", arg: "hello-world", wantCode: errs.NotFound},
		{name: "not a segment boundary", arg: "board", wantCode: errs.NotFound},
		{name: "no match", arg: "nope", wantCode: errs.NotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := c.Resolve(tt.arg)
			if tt.wantCode != 0 {
				if r != nil {
					t.Fatalf("expected no repo, got %q", r.URL)
				}
				var coded *errs.Coded
				if !errors.As(err, &coded) {
					t.Fatalf("expected *errs.Coded, got %v", err)
				}
				if coded.Code != tt.wantCode {
					t.Fatalf("got exit code %d, want %d", coded.Code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.URL != tt.wantURL {
				t.Fatalf("got %q, want %q", r.URL, tt.wantURL)
			}
		})
	}
}
