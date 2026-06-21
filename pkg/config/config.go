// Package config defines the on-disk config schema and load/save with
// file-level locking.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"

	"github.com/AndrewHannigan/repocache/pkg/errs"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// Config is the root TOML document.
type Config struct {
	Settings Settings `toml:"settings,omitempty"`
	Repos    []Repo   `toml:"repo,omitempty"`
	Owners   []Owner  `toml:"owner,omitempty"`
}

type Settings struct {
	BgSyncInterval string `toml:"bg_sync_interval,omitempty"`
}

type Repo struct {
	URL  string `toml:"url"`
	Name string `toml:"name,omitempty"`
	// Source, when set, is the resolved name of the [[owner]] entry that
	// auto-added this repo (see Owner). Empty means the repo was added by
	// the user directly. Auto-managed repos are reconciled on each sync;
	// user-added repos are never touched by owner reconciliation.
	Source string `toml:"source,omitempty"`
}

// Owner is a tracked user or org. On each sync, repocache lists the owner's
// repos (via `gh`) and materializes any new ones as Source-tagged Repo
// entries, so the rest of repocache treats them as ordinary cache repos.
type Owner struct {
	URL             string `toml:"url"`
	Name            string `toml:"name,omitempty"`
	IncludeForks    bool   `toml:"include_forks,omitempty"`
	IncludeArchived bool   `toml:"include_archived,omitempty"`
	// Visibility filters discovered repos: "all" (default), "public", or
	// "private". Empty is treated as "all".
	Visibility string `toml:"visibility,omitempty"`
}

// Name returns the effective name for a repo: the explicit Name field if
// set, else the default derived from URL.
func (r Repo) ResolvedName() (string, error) {
	if r.Name != "" {
		return r.Name, nil
	}
	return paths.DefaultName(r.URL)
}

// ResolvedName returns the effective name for an owner: the explicit Name
// field if set, else "host/owner" derived from URL.
func (o Owner) ResolvedName() (string, error) {
	if o.Name != "" {
		return o.Name, nil
	}
	return paths.DefaultOwnerName(o.URL)
}

// ErrLocked is returned when the config lock cannot be acquired in time.
var ErrLocked = errors.New("config locked by another process")

// Load reads the config file. Missing file returns an empty Config (not an
// error). Malformed file returns an error.
func Load() (*Config, error) {
	data, err := os.ReadFile(paths.ConfigFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", paths.ConfigFile(), err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config atomically (write to .tmp, rename).
func Save(c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.ConfigDir(), 0755); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := paths.ConfigFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, paths.ConfigFile())
}

// WithLock acquires the config lock, runs fn, releases the lock.
// Returns ErrLocked if the lock cannot be acquired within timeout.
func WithLock(timeout time.Duration, fn func(*Config) error) error {
	if err := os.MkdirAll(paths.ConfigDir(), 0755); err != nil {
		return err
	}
	lock := flock.New(paths.ConfigLockFile())
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if !locked {
		return ErrLocked
	}
	defer lock.Unlock()
	c, err := Load()
	if err != nil {
		return err
	}
	return fn(c)
}

// Validate enforces invariants: every repo and owner has a URL, and every
// (resolved) name is unique across both repos and owners (they share one
// namespace because commands resolve a single argument against both).
func (c *Config) Validate() error {
	seen := make(map[string]string) // resolved name -> "repo N" / "owner N"
	for i, r := range c.Repos {
		if r.URL == "" {
			return fmt.Errorf("repo[%d]: url is required", i)
		}
		name, err := r.ResolvedName()
		if err != nil {
			return fmt.Errorf("repo[%d] (%q): %w", i, r.URL, err)
		}
		if prev, ok := seen[name]; ok {
			return fmt.Errorf("name %q appears in both %s and repo %d", name, prev, i)
		}
		seen[name] = fmt.Sprintf("repo %d", i)
	}
	for i, o := range c.Owners {
		if o.URL == "" {
			return fmt.Errorf("owner[%d]: url is required", i)
		}
		name, err := o.ResolvedName()
		if err != nil {
			return fmt.Errorf("owner[%d] (%q): %w", i, o.URL, err)
		}
		if prev, ok := seen[name]; ok {
			return fmt.Errorf("name %q appears in both %s and owner %d", name, prev, i)
		}
		seen[name] = fmt.Sprintf("owner %d", i)
	}
	return nil
}

// FindByName returns the repo entry with the given resolved name, or nil
// if not present.
func (c *Config) FindByName(name string) *Repo {
	for i := range c.Repos {
		if n, err := c.Repos[i].ResolvedName(); err == nil && n == name {
			return &c.Repos[i]
		}
	}
	return nil
}

// FindOwnerByName returns the owner entry with the given resolved name, or
// nil if not present.
func (c *Config) FindOwnerByName(name string) *Owner {
	for i := range c.Owners {
		if n, err := c.Owners[i].ResolvedName(); err == nil && n == name {
			return &c.Owners[i]
		}
	}
	return nil
}

// ReposForOwner returns the resolved names of every repo whose Source equals
// the given owner name (i.e. repos auto-added by that owner).
func (c *Config) ReposForOwner(owner string) []string {
	var names []string
	for i := range c.Repos {
		if c.Repos[i].Source != owner {
			continue
		}
		if n, err := c.Repos[i].ResolvedName(); err == nil {
			names = append(names, n)
		}
	}
	return names
}

// Resolve finds the config entry matching name, per SPEC §5.0: an exact
// match on the resolved name wins; otherwise an unambiguous suffix match
// on path-segment ("/") boundaries is used. Returns an errs.Coded with
// NotFound when nothing matches or when a suffix matches more than one
// repo (the message lists the candidates so the user can disambiguate).
func (c *Config) Resolve(name string) (*Repo, error) {
	if r := c.FindByName(name); r != nil {
		return r, nil
	}
	var matches []*Repo
	var candidates []string
	for i := range c.Repos {
		n, err := c.Repos[i].ResolvedName()
		if err != nil {
			continue
		}
		if strings.HasSuffix(n, "/"+name) {
			matches = append(matches, &c.Repos[i])
			candidates = append(candidates, n)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, errs.New(errs.NotFound, "repo %q is not in the config", name)
	default:
		return nil, errs.New(errs.NotFound,
			"repo %q is ambiguous; matches: %s", name, strings.Join(candidates, ", "))
	}
}

// ResolveOwner finds the owner entry matching name using the same rule as
// Resolve (exact resolved-name match, else unambiguous suffix match on "/"
// boundaries). Returns an errs.Coded with NotFound when nothing matches or
// when a suffix matches more than one owner.
func (c *Config) ResolveOwner(name string) (*Owner, error) {
	if o := c.FindOwnerByName(name); o != nil {
		return o, nil
	}
	var matches []*Owner
	var candidates []string
	for i := range c.Owners {
		n, err := c.Owners[i].ResolvedName()
		if err != nil {
			continue
		}
		if strings.HasSuffix(n, "/"+name) {
			matches = append(matches, &c.Owners[i])
			candidates = append(candidates, n)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, errs.New(errs.NotFound, "owner %q is not in the config", name)
	default:
		return nil, errs.New(errs.NotFound,
			"owner %q is ambiguous; matches: %s", name, strings.Join(candidates, ", "))
	}
}

// EmptyTemplate returns the contents of an empty config file with a
// helpful header comment.
func EmptyTemplate() []byte {
	return []byte(`# repocache config.
# Add a repo with:        repocache repo add <repo-url>
# Add a whole user/org:   repocache repo add <owner-url>   # needs gh
# List with:              repocache repo list
# Sync with:              repocache sync
#
# Manual entries look like:
# [[repo]]
# url = "https://github.com/owner/name"
# # name = "owner/name"   # optional; default derived from URL
#
# [[owner]]
# url = "https://github.com/owner"   # sync discovers + adds this owner's repos
# # include_forks = false
# # include_archived = false
# # visibility = "all"   # all|public|private

`)
}
