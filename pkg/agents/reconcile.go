package agents

import (
	"bytes"
	"os"
	"sort"
)

// ReconcileDocs refreshes each integrated agent's REPOCACHE.md so it
// matches the embedded DocContent. It exists because upgrading the
// repocache binary (brew, go install, install.sh) swaps in a possibly
// newer embedded doc, but nothing re-runs Install — so the on-disk copy
// drifts. The drift check is content-based, not version-based, so it
// also heals `go install` dogfood builds (which all stamp version=dev).
//
// Only agents recorded in state are touched, and only their REPOCACHE.md
// file — never the CLAUDE.md/AGENTS.md import line, allowed-dir list, or
// hooks (those are presence-checked and effectively never change shape;
// reconciling them belongs to `repocache init`). A doc the user deleted
// is left deleted: reconcile updates drift, it does not resurrect files.
//
// Returns the keys of agents whose doc was rewritten, sorted.
func ReconcileDocs(state *State) ([]string, error) {
	var updated []string
	for key := range state.Agents {
		a := ByKey(key)
		if a == nil {
			continue
		}
		changed, err := reconcileDoc(a.DocPath())
		if err != nil {
			return updated, err
		}
		if changed {
			updated = append(updated, key)
		}
	}
	sort.Strings(updated)
	return updated, nil
}

// reconcileDoc rewrites path with DocContent when it exists but differs.
// A missing file is left missing (see ReconcileDocs doc comment).
func reconcileDoc(path string) (bool, error) {
	cur, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if bytes.Equal(cur, DocContent) {
		return false, nil
	}
	if err := os.WriteFile(path, DocContent, 0644); err != nil {
		return false, err
	}
	return true, nil
}
