package configmodel

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kaeawc/grit/internal/fsutil"
	"github.com/kaeawc/grit/internal/responsepayload"
)

type RuntimeState struct {
	ActionCacheProbes map[string]responsepayload.CacheProbe `json:"actionCacheProbes,omitempty"`
	ActionRemoteBytes map[string]int64                      `json:"actionRemoteBytes,omitempty"`
}

// RuntimeActionObservation captures the runtime cache data we want to feed back
// into future schedules. RemoteBytesRead is only recorded when it is positive;
// zero-byte runs do not clear prior observations so the planner retains a
// conservative estimate until it sees a new measured transfer.
type RuntimeActionObservation struct {
	ActionID        string
	CacheProbe      *responsepayload.CacheProbe
	RemoteBytesRead int64
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
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func (s *Store) RecordRuntimeProbes(root, key string, probes []responsepayload.CacheProbe) error {
	if len(probes) == 0 {
		return nil
	}
	observations := make([]RuntimeActionObservation, 0, len(probes))
	for _, probe := range probes {
		probe := probe
		observations = append(observations, RuntimeActionObservation{
			ActionID:   probe.ActionID,
			CacheProbe: &probe,
		})
	}
	return s.RecordRuntimeObservations(root, key, observations)
}

func (s *Store) RecordRuntimeObservations(root, key string, observations []RuntimeActionObservation) error {
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
	if state.ActionRemoteBytes == nil {
		state.ActionRemoteBytes = map[string]int64{}
	}
	for _, observation := range observations {
		if observation.ActionID == "" {
			continue
		}
		if observation.CacheProbe != nil {
			state.ActionCacheProbes[observation.ActionID] = *observation.CacheProbe
		}
		if observation.RemoteBytesRead > 0 {
			state.ActionRemoteBytes[observation.ActionID] = observation.RemoteBytesRead
		}
	}
	return writeRuntimeState(root, key, state)
}
