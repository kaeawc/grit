package configmodel

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/responsepayload"
)

type RuntimeState struct {
	ActionCacheProbes map[string]responsepayload.CacheProbe `json:"actionCacheProbes,omitempty"`
}

func runtimeFilePath(root, key string) string {
	return filepath.Join(root, ".grit", "configmodel", key+".runtime.json")
}

func loadRuntimeState(root, key string) (*RuntimeState, error) {
	data, err := os.ReadFile(runtimeFilePath(root, key))
	if err != nil {
		if os.IsNotExist(err) {
			return &RuntimeState{}, nil
		}
		return nil, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeRuntimeState(root, key string, state *RuntimeState) error {
	path := runtimeFilePath(root, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (s *Store) RecordRuntimeProbes(root, key string, probes []responsepayload.CacheProbe) error {
	if key == "" || root == "" {
		return nil
	}
	state, err := loadRuntimeState(root, key)
	if err != nil {
		return err
	}
	if state.ActionCacheProbes == nil {
		state.ActionCacheProbes = map[string]responsepayload.CacheProbe{}
	}
	for _, probe := range probes {
		if probe.ActionID == "" {
			continue
		}
		state.ActionCacheProbes[probe.ActionID] = probe
	}
	return writeRuntimeState(root, key, state)
}

