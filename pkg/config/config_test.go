package config

import (
	"errors"
	"testing"

	"github.com/AndrewHannigan/repocache/pkg/errs"
)

func TestResolve(t *testing.T) {
	c := &Config{Repos: []Repo{
		{URL: "https://github.com/AndrewHannigan/blackboard"},
		{URL: "https://github.com/AndrewHannigan/whiteboard"},
		{URL: "https://gitlab.com/other/blackboard"},
		{URL: "git@github.com:foo/bar.git", Name: "myorg/bar"},
	}}

	tests := []struct {
		name     string
		arg      string
		wantURL  string // non-empty => expect this repo's URL
		wantCode int    // non-zero => expect an errs.Coded with this code
	}{
		{name: "exact full name", arg: "github.com/AndrewHannigan/whiteboard",
			wantURL: "https://github.com/AndrewHannigan/whiteboard"},
		{name: "exact name override", arg: "myorg/bar", wantURL: "git@github.com:foo/bar.git"},
		{name: "unambiguous suffix", arg: "whiteboard",
			wantURL: "https://github.com/AndrewHannigan/whiteboard"},
		{name: "two-segment suffix", arg: "AndrewHannigan/blackboard",
			wantURL: "https://github.com/AndrewHannigan/blackboard"},
		{name: "ambiguous suffix", arg: "blackboard", wantCode: errs.NotFound},
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
