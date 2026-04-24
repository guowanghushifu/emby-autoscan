package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/snapshot"
)

type Store struct {
	Path string
}

func (s Store) Load() (snapshot.State, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyState(), false, nil
		}
		return snapshot.State{}, false, err
	}

	var state snapshot.State
	if err := json.Unmarshal(data, &state); err != nil {
		return snapshot.State{}, true, err
	}

	return normalizeState(state), true, nil
}

func (s Store) Save(state snapshot.State) error {
	state = normalizeState(state)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}

	tmpPath := s.Path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, s.Path)
}

func emptyState() snapshot.State {
	return snapshot.State{
		Version:  1,
		Monitors: make(map[string]snapshot.MonitorSnapshot),
	}
}

func normalizeState(state snapshot.State) snapshot.State {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Monitors == nil {
		state.Monitors = make(map[string]snapshot.MonitorSnapshot)
	}
	return state
}
