package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// importLineRecord returns the canonical record of an added import for
// the state file, or nil if nothing was added this run.
func importLineRecord(added bool, doc string) []string {
	if !added {
		return nil
	}
	return []string{doc}
}
