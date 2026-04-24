package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/snapshot"
)

func TestLoadMissingStateReturnsEmptyStateAndFalse(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "missing", "state.json")}

	state, exists, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if exists {
		t.Fatalf("Load() exists = true, want false")
	}
	if state.Version != 1 {
		t.Fatalf("Load() Version = %d, want 1", state.Version)
	}
	if state.Monitors == nil {
		t.Fatalf("Load() Monitors = nil, want initialized map")
	}
	if len(state.Monitors) != 0 {
		t.Fatalf("Load() Monitors length = %d, want 0", len(state.Monitors))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "state.json")}
	want := snapshot.State{
		Monitors: map[string]snapshot.MonitorSnapshot{
			"movies-a": {
				MonitorName: "movies-a",
				Path:        "/media/movies-a",
				LibraryID:   "library-shared",
				Files: map[string]snapshot.FileInfo{
					"/media/movies-a/one.mkv": {Path: "/media/movies-a/one.mkv", Size: 100, ModTime: 1000},
				},
			},
			"movies-b": {
				MonitorName: "movies-b",
				Path:        "/media/movies-b",
				LibraryID:   "library-shared",
				Files: map[string]snapshot.FileInfo{
					"/media/movies-b/two.mkv": {Path: "/media/movies-b/two.mkv", Size: 200, ModTime: 2000},
				},
			},
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, exists, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !exists {
		t.Fatalf("Load() exists = false, want true")
	}
	want.Version = 1
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() state = %#v, want %#v", got, want)
	}
	if got.Monitors["movies-a"].MonitorName != "movies-a" || got.Monitors["movies-b"].MonitorName != "movies-b" {
		t.Fatalf("Load() merged monitors with shared library ID: %#v", got.Monitors)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state", "state.json")
	store := Store{Path: path}

	if err := store.Save(snapshot.State{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(saved path) error = %v", err)
	}
}
