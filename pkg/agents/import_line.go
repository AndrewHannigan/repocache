package agents

import (
	"os"
	"strings"
)

// removeImportLine removes any line starting with `@<doc>` from
// memoryPath. Used to migrate away from / uninstall the legacy
// @REPOCACHE.md import that older versions (under the old repocache name)
// appended to CLAUDE.md / AGENTS.md.
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
