package forge

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestBuildListArgs(t *testing.T) {
	tests := []struct {
		name  string
		login string
		f     Filter
		want  []string
	}{
		{
			name:  "defaults exclude forks and archived",
			login: "octocat",
			f:     Filter{},
			want: []string{"repo", "list",
				"--limit", "1000",
				"--json", "name,url,sshUrl,isFork,isArchived,visibility",
				"--source", "--no-archived",
				"--", "octocat"},
		},
		{
			name:  "include everything, custom limit, private only",
			login: "acme",
			f:     Filter{IncludeForks: true, IncludeArchived: true, Visibility: "private", Limit: 5},
			want: []string{"repo", "list",
				"--limit", "5",
				"--json", "name,url,sshUrl,isFork,isArchived,visibility",
				"--visibility", "private",
				"--", "acme"},
		},
		{
			name:  "visibility all is omitted",
			login: "acme",
			f:     Filter{IncludeForks: true, IncludeArchived: true, Visibility: "all"},
			want: []string{"repo", "list",
				"--limit", "1000",
				"--json", "name,url,sshUrl,isFork,isArchived,visibility",
				"--", "acme"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildListArgs(tt.login, tt.f)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildListArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeRepos(t *testing.T) {
	data := []byte(`[
		{"name":"alpha","url":"https://github.com/acme/alpha","sshUrl":"git@github.com:acme/alpha.git","isFork":false,"isArchived":false,"visibility":"PUBLIC"},
		{"name":"beta","url":"https://github.com/acme/beta","sshUrl":"git@github.com:acme/beta.git","isFork":true,"isArchived":false,"visibility":"PRIVATE"}
	]`)

	t.Run("https clone urls", func(t *testing.T) {
		repos, err := decodeRepos(data, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(repos) != 2 {
			t.Fatalf("got %d repos, want 2", len(repos))
		}
		if repos[0].Name != "alpha" || repos[0].CloneURL != "https://github.com/acme/alpha" {
			t.Fatalf("unexpected first repo: %+v", repos[0])
		}
		if !repos[1].IsFork || repos[1].Visibility != "PRIVATE" {
			t.Fatalf("unexpected second repo: %+v", repos[1])
		}
	})

	t.Run("ssh clone urls", func(t *testing.T) {
		repos, err := decodeRepos(data, true)
		if err != nil {
			t.Fatal(err)
		}
		if repos[0].CloneURL != "git@github.com:acme/alpha.git" {
			t.Fatalf("want ssh url, got %q", repos[0].CloneURL)
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := decodeRepos([]byte("not json"), false); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

func TestClassifyExecErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		stderr string
		want   error // sentinel to errors.Is against; nil means "neither sentinel"
	}{
		{"binary missing", exec.ErrNotFound, "", ErrGhMissing},
		{"not logged in", errors.New("exit status 1"), "To get started with GitHub CLI, please run: gh auth login", ErrGhUnauthed},
		{"requires auth", errors.New("exit status 1"), "HTTP 401: requires authentication", ErrGhUnauthed},
		{"other failure", errors.New("exit status 1"), "could not resolve host", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyExecErr("gh repo list", tt.err, tt.stderr)
			if got == nil {
				t.Fatal("classifyExecErr returned nil")
			}
			switch tt.want {
			case ErrGhMissing:
				if !errors.Is(got, ErrGhMissing) {
					t.Fatalf("want ErrGhMissing, got %v", got)
				}
			case ErrGhUnauthed:
				if !errors.Is(got, ErrGhUnauthed) {
					t.Fatalf("want ErrGhUnauthed, got %v", got)
				}
			default:
				if errors.Is(got, ErrGhMissing) || errors.Is(got, ErrGhUnauthed) {
					t.Fatalf("want a non-sentinel error, got %v", got)
				}
			}
		})
	}
}

func TestBuildPRListArgs(t *testing.T) {
	got := buildPRListArgs("acme/widgets", "feature/login")
	want := []string{
		"pr", "list",
		"--repo=acme/widgets",
		"--head=feature/login",
		"--state", "merged",
		"--json", "number",
		"--limit", "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildPRListArgs() = %v, want %v", got, want)
	}
}

func TestDecodeMergedPR(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    int
		wantErr bool
	}{
		{"merged", `[{"number":42}]`, 42, false},
		{"none", `[]`, 0, false},
		{"garbage", `not json`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeMergedPR([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeMergedPR() err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("decodeMergedPR() = %d, want %d", got, tt.want)
			}
		})
	}
}
