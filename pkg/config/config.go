// Package config defines the on-disk config schema and load/save with
// file-level locking.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// Config is the root TOML document.
type Config struct {
	Settings Settings `toml:"settings,omitempty"`
	Repos    []Repo   `toml:"repo,omitempty"`
}

type Settings struct {
	BgSyncInterval string `toml:"bg_sync_interval,omitempty"`
}

type Repo struct {
	URL  string `toml:"url"`
	Name string `toml:"name,omitempty"`
}

// Name returns the effective name for a repo: the explicit Name field if
// set, else the default derived from URL.
func (r Repo) ResolvedName() (string, error) {
	if r.Name != "" {
		return r.Name, nil
	}
	return paths.DefaultName(r.URL)
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

// Validate enforces invariants: every repo has a URL, every (resolved)
// name is unique.
func (c *Config) Validate() error {
	seen := make(map[string]int)
	for i, r := range c.Repos {
		if r.URL == "" {
			return fmt.Errorf("repo[%d]: url is required", i)
		}
		name, err := r.ResolvedName()
		if err != nil {
			return fmt.Errorf("repo[%d] (%q): %w", i, r.URL, err)
		}
		if prev, ok := seen[name]; ok {
			return fmt.Errorf("repo name %q appears in entries %d and %d", name, prev, i)
		}
		seen[name] = i
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

// EmptyTemplate returns the contents of an empty config file with a
// helpful header comment.
func EmptyTemplate() []byte {
	return []byte(`# repocache config.
# Add repos with:   repocache repo add <url>
# List with:        repocache repo list
# Sync with:        repocache sync
#
# Manual entries look like:
# [[repo]]
# url = "https://github.com/owner/name"
# # name = "owner/name"   # optional; default derived from URL

`)
}

