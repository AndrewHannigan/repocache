package workspace

// Session-to-workspace linking. When an agent creates a workspace mid-session,
// shed records which session did it so `shed resume` can reopen that exact
// session in the right directory. The link is a small JSON sidecar living
// inside the workspace's .git dir (so it is removed automatically with the
// workspace); a pending intent recorded by the pre-exec hook is finalized into
// it by `workspace new`. See docs/design/session-resume.md.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/AndrewHannigan/shed/pkg/paths"
)

// SessionLink records the agent session that owns a workspace. CWD is the
// directory the session was launched in — `shed resume` cd's there before
// invoking the agent, since resume is directory-scoped for some agents.
type SessionLink struct {
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	CWD       string    `json:"cwd"`
	LinkedAt  time.Time `json:"linked_at"`
}

// LocateByName finds the unique workspace with the given workspace name across
// the given repos, returning its repo name and absolute path. The workspace
// name is globally unique (enforced at creation), so at most one matches. It
// only checks for a workspace dir's existence — no git state is computed — so
// it is cheap enough for the per-create uniqueness guard.
func LocateByName(repoNames []string, name string) (repo, path string, found bool) {
	for _, r := range repoNames {
		if Exists(r, name) {
			return r, PathFor(r, name), true
		}
	}
	return "", "", false
}

// WriteLink writes the authoritative session link into the workspace's .git
// sidecar.
func WriteLink(repo, name string, l SessionLink) error {
	return writeJSONAtomic(paths.WorkspaceSessionFile(repo, name), l)
}

// LoadLink reads a workspace's session link, returning (nil, nil) if none.
func LoadLink(repo, name string) (*SessionLink, error) {
	return readLink(paths.WorkspaceSessionFile(repo, name))
}

// WritePending records a pending session→workspace intent, keyed by workspace
// name, for `workspace new` to finalize.
func WritePending(name string, l SessionLink) error {
	p := paths.SessionPendingFile(name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return writeJSONAtomic(p, l)
}

// TakePending reads and removes the pending intent for a workspace name,
// returning (nil, nil) if none exists.
func TakePending(name string) (*SessionLink, error) {
	p := paths.SessionPendingFile(name)
	l, err := readLink(p)
	if err != nil || l == nil {
		return l, err
	}
	os.Remove(p)
	return l, nil
}

func writeJSONAtomic(path string, l SessionLink) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return paths.WriteFileAtomic(path, data, 0644)
}

func readLink(path string) (*SessionLink, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var l SessionLink
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}
