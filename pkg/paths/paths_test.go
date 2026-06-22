package paths

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestValidateName(t *testing.T) {
	valid := []string{
		"github.com/octocat/Hello-World",
		"github.com/octocat",
		"example.com/group/sub/repo",
	}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{
		"",
		"..",
		"github.com/../../../etc/passwd",
		"../../../../tmp/pwn",
		"/etc/passwd",
		"github.com//octocat",
		`github.com\octocat`,
	}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", n)
		}
	}
}

func TestValidateBranch(t *testing.T) {
	valid := []string{"main", "feature/login", "release/v1.2.3"}
	for _, b := range valid {
		if err := ValidateBranch(b); err != nil {
			t.Errorf("ValidateBranch(%q) = %v, want nil", b, err)
		}
	}
	invalid := []string{
		"",
		"../../../../tmp/evil",
		"feature/../../escape",
		"/abs",
		"-x",            // would be parsed as a git option
		"--upload-pack", // option injection
	}
	for _, b := range invalid {
		if err := ValidateBranch(b); err == nil {
			t.Errorf("ValidateBranch(%q) = nil, want error", b)
		}
	}
}

func TestWriteFileAtomicPreservesMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")

	// New file uses the supplied default mode.
	if err := WriteFileAtomic(p, []byte("a"), 0640); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0640 {
		t.Fatalf("new file mode = %o, want 0640", fi.Mode().Perm())
	}

	// User tightens the file; a rewrite must not widen it back to the default.
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("bb"), 0644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Fatalf("rewritten file mode = %o, want preserved 0600", fi.Mode().Perm())
	}
	if got, _ := os.ReadFile(p); string(got) != "bb" {
		t.Fatalf("content = %q, want %q", got, "bb")
	}
	// No temp file left behind.
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file %q.tmp should not exist", p)
	}
}
