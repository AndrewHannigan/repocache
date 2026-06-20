package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

// Claude implements Agent for Claude Code.
type Claude struct {
	dir string // ~/.claude
}

func NewClaude() *Claude {
	home, _ := os.UserHomeDir()
	return &Claude{dir: filepath.Join(home, ".claude")}
}

func (c *Claude) Key() string  { return "claude" }
func (c *Claude) Name() string { return "Claude Code" }

func (c *Claude) Detected() bool {
	s, err := os.Stat(c.dir)
	return err == nil && s.IsDir()
}

func (c *Claude) docFile() string      { return filepath.Join(c.dir, "REPOCACHE.md") }
func (c *Claude) memoryFile() string   { return filepath.Join(c.dir, "CLAUDE.md") }
func (c *Claude) settingsFile() string { return filepath.Join(c.dir, "settings.json") }

func (c *Claude) Install() (Installed, error) {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return Installed{}, err
	}
	// 1. Write/overwrite REPOCACHE.md (refresh on every install).
	if err := os.WriteFile(c.docFile(), DocContent, 0644); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", c.docFile(), err)
	}
	// 2. Append @REPOCACHE.md line to CLAUDE.md if not already present.
	importLine, err := ensureImportLine(c.memoryFile(), "REPOCACHE.md")
	if err != nil {
		return Installed{}, err
	}
	// 3. Add directories to permissions.additionalDirectories.
	added, err := ensureJSONArrayEntries(c.settingsFile(),
		[]string{"permissions", "additionalDirectories"}, PathsToRegister())
	if err != nil {
		return Installed{}, err
	}
	return Installed{
		AddedPaths:   added,
		AddedImports: importLineRecord(importLine),
	}, nil
}

func (c *Claude) Uninstall(prev Installed) error {
	if err := removeImportLine(c.memoryFile(), "REPOCACHE.md"); err != nil {
		return err
	}
	if len(prev.AddedPaths) > 0 {
		if err := removeJSONArrayEntries(c.settingsFile(),
			[]string{"permissions", "additionalDirectories"}, prev.AddedPaths); err != nil {
			return err
		}
	}
	// Remove the REPOCACHE.md file (best-effort).
	_ = os.Remove(c.docFile())
	return nil
}

// importLineRecord returns the canonical record of an added import for
// the state file, or nil if nothing was added this run.
func importLineRecord(added bool) []string {
	if !added {
		return nil
	}
	return []string{"REPOCACHE.md"}
}

// ensureImportLine appends `@<doc>  <!-- repocache:managed -->` to
// memoryPath if no line starting with `@<doc>` already exists. Returns
// true if a line was added this call.
func ensureImportLine(memoryPath, doc string) (added bool, err error) {
	existing, err := os.ReadFile(memoryPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	prefix := "@" + doc
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0755); err != nil {
		return false, err
	}
	var sep string
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		sep = "\n"
	}
	line := fmt.Sprintf("%s%s  <!-- %s -->\n", sep, prefix, Marker)
	f, err := os.OpenFile(memoryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return false, err
	}
	return true, nil
}

// removeImportLine removes any line starting with `@<doc>` from memoryPath.
func removeImportLine(memoryPath, doc string) error {
	existing, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	prefix := "@" + doc
	var out []string
	changed := false
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			changed = true
			continue
		}
		out = append(out, line)
	}
	if !changed {
		return nil
	}
	return os.WriteFile(memoryPath, []byte(strings.Join(out, "\n")), 0644)
}

// ensureJSONArrayEntries reads a (possibly missing) JSONC file, walks
// to the nested key path, ensures the value is an array, and appends
// any entries from `values` that aren't already present. Returns the
// list of entries that were added this call (for state recording).
//
// Comments in the existing JSONC file are stripped on write (encoding/
// json round-trip). This is a documented limitation.
func ensureJSONArrayEntries(filePath string, keyPath []string, values []string) ([]string, error) {
	root, err := loadJSONC(filePath)
	if err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	cur := root
	for i, k := range keyPath {
		if i == len(keyPath)-1 {
			break
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	lastKey := keyPath[len(keyPath)-1]

	existing, _ := cur[lastKey].([]any)
	existingSet := map[string]bool{}
	for _, e := range existing {
		if s, ok := e.(string); ok {
			existingSet[s] = true
		}
	}
	var added []string
	for _, v := range values {
		if !existingSet[v] {
			existing = append(existing, v)
			added = append(added, v)
		}
	}
	cur[lastKey] = existing

	if err := saveJSON(filePath, root); err != nil {
		return nil, err
	}
	return added, nil
}

// removeJSONArrayEntries removes the given entries from the array at
// keyPath. Missing keyPath or missing entries are no-ops.
func removeJSONArrayEntries(filePath string, keyPath []string, values []string) error {
	root, err := loadJSONC(filePath)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	cur := root
	for i, k := range keyPath {
		if i == len(keyPath)-1 {
			break
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	lastKey := keyPath[len(keyPath)-1]
	existing, ok := cur[lastKey].([]any)
	if !ok {
		return nil
	}
	remove := map[string]bool{}
	for _, v := range values {
		remove[v] = true
	}
	kept := existing[:0]
	for _, e := range existing {
		if s, ok := e.(string); ok && remove[s] {
			continue
		}
		kept = append(kept, e)
	}
	cur[lastKey] = kept
	return saveJSON(filePath, root)
}

func loadJSONC(filePath string) (map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	var root map[string]any
	if err := json.Unmarshal(standardized, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	return root, nil
}

func saveJSON(filePath string, root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filePath)
}
