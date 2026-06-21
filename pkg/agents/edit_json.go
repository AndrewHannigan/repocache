package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// loadJSONC reads a JSONC file (JSON with comments). Comments are
// stripped on read; they will not survive a subsequent saveJSON.
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
