package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

func loadTOML(filePath string) (map[string]any, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func saveTOML(filePath string, root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	data, err := toml.Marshal(root)
	if err != nil {
		return err
	}
	// Preserve the existing file's mode (this rewrites a user-owned settings
	// file that may be deliberately restricted); 0644 only for a new file.
	return paths.WriteFileAtomic(filePath, data, 0644)
}
