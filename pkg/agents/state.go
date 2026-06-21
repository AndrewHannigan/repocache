package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/AndrewHannigan/repocache/pkg/paths"
)

// State is the on-disk record of what repocache has installed into each
// agent's config files. Used to compute precise reversals on uninstall.
type State struct {
	Agents map[string]Installed `json:"agents,omitempty"`
}

const stateFilename = "agents.state.json"

func stateFile() string {
	return filepath.Join(paths.DataDir(), stateFilename)
}

// LoadState reads the sidecar state file. Missing file returns an empty
// State (not an error).
func LoadState() (*State, error) {
	data, err := os.ReadFile(stateFile())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Agents: map[string]Installed{}}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Agents == nil {
		s.Agents = map[string]Installed{}
	}
	return &s, nil
}

// SaveState writes the sidecar state file atomically.
func SaveState(s *State) error {
	if err := os.MkdirAll(paths.DataDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := stateFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, stateFile())
}
