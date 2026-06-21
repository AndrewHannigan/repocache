package paths

import "testing"

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare owner is github shorthand", "octocat", "https://github.com/octocat"},
		{"owner/repo shorthand", "octocat/Hello-World", "https://github.com/octocat/Hello-World"},
		{"leading slash is trimmed", "/octocat/Hello-World", "https://github.com/octocat/Hello-World"},
		{"trailing slash is trimmed", "octocat/", "https://github.com/octocat"},
		{"surrounding whitespace is trimmed", "  octocat  ", "https://github.com/octocat"},
		{"host without scheme gets https", "github.com/octocat", "https://github.com/octocat"},
		{"non-github host without scheme", "gitlab.com/foo/bar", "https://gitlab.com/foo/bar"},
		{"host:port without scheme", "localhost:8080/foo/bar", "https://localhost:8080/foo/bar"},
		{"https URL is unchanged", "https://github.com/octocat/Hello-World", "https://github.com/octocat/Hello-World"},
		{"https owner URL is unchanged", "https://github.com/octocat", "https://github.com/octocat"},
		{"ssh URL is unchanged", "ssh://git@github.com/foo/bar.git", "ssh://git@github.com/foo/bar.git"},
		{"git scheme is unchanged", "git://github.com/foo/bar.git", "git://github.com/foo/bar.git"},
		{"scp-style remote is unchanged", "git@github.com:foo/bar.git", "git@github.com:foo/bar.git"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeURL(tt.input); got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Shorthand must round-trip through the same parsing the add path uses: an
// expanded bare owner classifies as an owner, an expanded owner/repo as a repo.
func TestNormalizeURLClassification(t *testing.T) {
	owner := NormalizeURL("octocat")
	isOwner, err := IsOwnerURL(owner)
	if err != nil {
		t.Fatalf("IsOwnerURL(%q) returned error: %v", owner, err)
	}
	if !isOwner {
		t.Errorf("expanded bare owner %q should classify as an owner", owner)
	}
	if got, err := DefaultOwnerName(owner); err != nil || got != "github.com/octocat" {
		t.Errorf("DefaultOwnerName(%q) = %q, %v; want github.com/octocat", owner, got, err)
	}

	repo := NormalizeURL("octocat/Hello-World")
	isOwner, err = IsOwnerURL(repo)
	if err != nil {
		t.Fatalf("IsOwnerURL(%q) returned error: %v", repo, err)
	}
	if isOwner {
		t.Errorf("expanded owner/repo %q should classify as a repo", repo)
	}
	if got, err := DefaultName(repo); err != nil || got != "github.com/octocat/Hello-World" {
		t.Errorf("DefaultName(%q) = %q, %v; want github.com/octocat/Hello-World", repo, got, err)
	}
}
