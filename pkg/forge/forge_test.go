package forge

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestProviderFor(t *testing.T) {
	tests := []struct {
		host string
		cli  string
	}{
		{"github.com", "gh"},
		{"", "gh"},
		{"github.example.com", "gh"}, // enterprise GitHub → gh
		{"gitlab.com", "glab"},
		{"GitLab.com", "glab"},         // case-insensitive
		{"gitlab.example.com", "glab"}, // self-managed GitLab
		{"git.acme.com", "gh"},         // unknown → gh default
	}
	for _, tt := range tests {
		if got := providerFor(tt.host).cli(); got != tt.cli {
			t.Errorf("providerFor(%q).cli() = %q, want %q", tt.host, got, tt.cli)
		}
	}
}

func TestChangeNoun(t *testing.T) {
	if got := ChangeNoun("github.com"); got != "PR" {
		t.Errorf("ChangeNoun(github) = %q, want PR", got)
	}
	if got := ChangeNoun("gitlab.com"); got != "MR" {
		t.Errorf("ChangeNoun(gitlab) = %q, want MR", got)
	}
}

func TestGitHubListArgs(t *testing.T) {
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
			got := githubProvider{}.listArgs(tt.login, tt.f)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("listArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitHubDecodeRepos(t *testing.T) {
	data := []byte(`[
		{"name":"alpha","url":"https://github.com/acme/alpha","sshUrl":"git@github.com:acme/alpha.git","isFork":false,"isArchived":false,"visibility":"PUBLIC"},
		{"name":"beta","url":"https://github.com/acme/beta","sshUrl":"git@github.com:acme/beta.git","isFork":true,"isArchived":false,"visibility":"PRIVATE"}
	]`)

	t.Run("https clone urls", func(t *testing.T) {
		repos, err := githubProvider{}.decodeRepos(data, Filter{}, false)
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
		repos, err := githubProvider{}.decodeRepos(data, Filter{}, true)
		if err != nil {
			t.Fatal(err)
		}
		if repos[0].CloneURL != "git@github.com:acme/alpha.git" {
			t.Fatalf("want ssh url, got %q", repos[0].CloneURL)
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := (githubProvider{}).decodeRepos([]byte("not json"), Filter{}, false); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

func TestGitHubMergedArgs(t *testing.T) {
	got := githubProvider{}.mergedArgs("acme/widgets", "feature/login")
	want := []string{
		"pr", "list",
		"--repo=acme/widgets",
		"--head=feature/login",
		"--state", "merged",
		"--json", "number",
		"--limit", "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergedArgs() = %v, want %v", got, want)
	}
}

func TestGitHubDecodeMerged(t *testing.T) {
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
			got, err := githubProvider{}.decodeMerged([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeMerged() err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("decodeMerged() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGitHubHostEnv(t *testing.T) {
	p := githubProvider{}
	if env := p.hostEnv("github.com"); env != nil {
		t.Errorf("github.com should need no env override, got %v", env)
	}
	if env := p.hostEnv("github.acme.com"); !reflect.DeepEqual(env, []string{"GH_HOST=github.acme.com"}) {
		t.Errorf("enterprise host env = %v, want GH_HOST override", env)
	}
}

func TestGitLabListArgs(t *testing.T) {
	got := gitlabProvider{}.listArgs("mygroup", Filter{})
	want := []string{"repo", "list", "--group", "mygroup", "--per-page", "1000", "--output", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listArgs() = %v, want %v", got, want)
	}
}

func TestGitLabDecodeReposFiltersClientSide(t *testing.T) {
	data := []byte(`[
		{"path":"alpha","http_url_to_repo":"https://gitlab.com/grp/alpha.git","ssh_url_to_repo":"git@gitlab.com:grp/alpha.git","archived":false,"visibility":"public"},
		{"path":"beta","http_url_to_repo":"https://gitlab.com/grp/beta.git","ssh_url_to_repo":"git@gitlab.com:grp/beta.git","archived":true,"visibility":"private"},
		{"path":"gamma","http_url_to_repo":"https://gitlab.com/grp/gamma.git","ssh_url_to_repo":"git@gitlab.com:grp/gamma.git","archived":false,"visibility":"private","forked_from_project":{"id":7}}
	]`)

	t.Run("zero filter drops archived and forks", func(t *testing.T) {
		repos, err := gitlabProvider{}.decodeRepos(data, Filter{}, false)
		if err != nil {
			t.Fatal(err)
		}
		// beta (archived) and gamma (fork) excluded; only alpha remains.
		if len(repos) != 1 || repos[0].Name != "alpha" {
			t.Fatalf("got %+v, want only alpha", repos)
		}
		if repos[0].CloneURL != "https://gitlab.com/grp/alpha.git" {
			t.Fatalf("unexpected clone url: %q", repos[0].CloneURL)
		}
	})

	t.Run("include forks and archived, ssh urls", func(t *testing.T) {
		repos, err := gitlabProvider{}.decodeRepos(data, Filter{IncludeForks: true, IncludeArchived: true}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(repos) != 3 {
			t.Fatalf("got %d repos, want 3", len(repos))
		}
		if repos[0].CloneURL != "git@gitlab.com:grp/alpha.git" {
			t.Fatalf("want ssh url, got %q", repos[0].CloneURL)
		}
		if !repos[2].IsFork {
			t.Fatalf("gamma should be detected as a fork: %+v", repos[2])
		}
	})

	t.Run("visibility filter", func(t *testing.T) {
		repos, err := gitlabProvider{}.decodeRepos(data, Filter{IncludeForks: true, IncludeArchived: true, Visibility: "private"}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range repos {
			if r.Visibility != "private" {
				t.Fatalf("expected only private repos, got %+v", r)
			}
		}
		if len(repos) != 2 { // beta + gamma
			t.Fatalf("got %d private repos, want 2", len(repos))
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := (gitlabProvider{}).decodeRepos([]byte("not json"), Filter{}, false); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

func TestGitLabMergedArgs(t *testing.T) {
	got := gitlabProvider{}.mergedArgs("grp/sub/widgets", "feature/login")
	want := []string{
		"mr", "list",
		"--repo=grp/sub/widgets",
		"--source-branch=feature/login",
		"--merged",
		"--output", "json",
		"--per-page", "1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergedArgs() = %v, want %v", got, want)
	}
}

func TestGitLabDecodeMerged(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    int
		wantErr bool
	}{
		{"merged", `[{"iid":7}]`, 7, false},
		{"none", `[]`, 0, false},
		{"garbage", `not json`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gitlabProvider{}.decodeMerged([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeMerged() err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("decodeMerged() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGitLabHostEnv(t *testing.T) {
	p := gitlabProvider{}
	if env := p.hostEnv("gitlab.com"); env != nil {
		t.Errorf("gitlab.com should need no env override, got %v", env)
	}
	if env := p.hostEnv("gitlab.acme.com"); !reflect.DeepEqual(env, []string{"GITLAB_HOST=gitlab.acme.com"}) {
		t.Errorf("self-managed host env = %v, want GITLAB_HOST override", env)
	}
}

func TestClassifyExecErr(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		stderr string
		want   error // sentinel to errors.Is against; nil means "neither sentinel"
	}{
		{"binary missing", exec.ErrNotFound, "", ErrCLIMissing},
		{"not logged in", errors.New("exit status 1"), "To get started with GitHub CLI, please run: gh auth login", ErrCLIUnauthed},
		{"requires auth", errors.New("exit status 1"), "HTTP 401: requires authentication", ErrCLIUnauthed},
		{"other failure", errors.New("exit status 1"), "could not resolve host", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyExecErr(githubProvider{}, "gh repo list", tt.err, tt.stderr)
			if got == nil {
				t.Fatal("classifyExecErr returned nil")
			}
			switch tt.want {
			case ErrCLIMissing:
				if !errors.Is(got, ErrCLIMissing) {
					t.Fatalf("want ErrCLIMissing, got %v", got)
				}
			case ErrCLIUnauthed:
				if !errors.Is(got, ErrCLIUnauthed) {
					t.Fatalf("want ErrCLIUnauthed, got %v", got)
				}
			default:
				if errors.Is(got, ErrCLIMissing) || errors.Is(got, ErrCLIUnauthed) {
					t.Fatalf("want a non-sentinel error, got %v", got)
				}
			}
		})
	}
}
