package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"

	"github.com/AndrewHannigan/repocache/pkg/cache"
	"github.com/AndrewHannigan/repocache/pkg/config"
	"github.com/AndrewHannigan/repocache/pkg/paths"
)

const bgSyncWorkerEnv = "REPOCACHE_BG_SYNC_WORKER"

func newBgSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__bg-sync",
		Short:  "(internal) Background sync invoked by SessionStart hooks",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(bgSyncWorkerEnv) == "1" {
				return bgSyncWorker()
			}
			return bgSyncEntry()
		},
	}
}

// bgSyncEntry runs in the SessionStart hook's process: a quick check,
// then either print a hint (first-run) or spawn a detached worker.
// Always exits 0 — the SessionStart hook must not break the agent.
func bgSyncEntry() error {
	c, err := config.Load()
	if err != nil || len(c.Repos) == 0 {
		return nil
	}
	if !everSynced(c) {
		fmt.Println("repocache: cache is empty. Run `repocache sync` to fetch your tracked repos.")
		return nil
	}
	// Spawn detached worker.
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	cmd := exec.Command(self, "__bg-sync")
	cmd.Env = append(os.Environ(), bgSyncWorkerEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile, lerr := openBgLog(); lerr == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	cmd.Stdin = nil
	_ = cmd.Start()
	return nil
}

// bgSyncWorker runs as the detached child. Acquires the global lock
// non-blocking; if held, exits silently. Otherwise runs the standard
// sync with --if-older-than from config.
func bgSyncWorker() error {
	if err := os.MkdirAll(paths.DataDir(), 0755); err != nil {
		return nil
	}
	lock := flock.New(paths.BgSyncLockFile())
	locked, err := lock.TryLock()
	if err != nil || !locked {
		return nil
	}
	defer lock.Unlock()
	return runSync(nil, 4, configBgInterval(), false)
}

func everSynced(c *config.Config) bool {
	for _, r := range c.Repos {
		name, err := r.ResolvedName()
		if err != nil {
			continue
		}
		if meta, _ := cache.LoadMeta(name); meta != nil {
			return true
		}
	}
	return false
}

// configBgInterval returns the staleness gate passed to `sync
// --if-older-than`. Default is 0 (no gate): the cache refreshes on every
// session start. Set bg_sync_interval (e.g. "1h") to skip repos synced
// within that window instead.
func configBgInterval() time.Duration {
	c, err := config.Load()
	if err != nil {
		return 0
	}
	if c.Settings.BgSyncInterval == "" {
		return 0
	}
	d, err := time.ParseDuration(c.Settings.BgSyncInterval)
	if err != nil {
		return 0
	}
	return d
}

func openBgLog() (*os.File, error) {
	return os.OpenFile(paths.BgSyncLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}
