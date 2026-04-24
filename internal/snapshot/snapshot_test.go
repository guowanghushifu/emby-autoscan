package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestScanRecordsRegularFilesOnly(t *testing.T) {
	root := t.TempDir()
	regularPath := filepath.Join(root, "movie.mkv")
	nestedDir := filepath.Join(root, "nested")
	nestedPath := filepath.Join(nestedDir, "episode.mkv")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(regularPath, []byte("movie"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(nestedPath, []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(regularPath, filepath.Join(root, "linked.mkv")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	modTime := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(regularPath, modTime, modTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	snapshot, err := ScanMonitor("movies", root, "library-1")
	if err != nil {
		t.Fatalf("ScanMonitor() error = %v", err)
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs(root) error = %v", err)
	}
	regularAbs, err := filepath.Abs(regularPath)
	if err != nil {
		t.Fatalf("Abs(regularPath) error = %v", err)
	}
	nestedAbs, err := filepath.Abs(nestedPath)
	if err != nil {
		t.Fatalf("Abs(nestedPath) error = %v", err)
	}

	if snapshot.MonitorName != "movies" {
		t.Fatalf("MonitorName = %q, want movies", snapshot.MonitorName)
	}
	if snapshot.Path != rootAbs {
		t.Fatalf("Path = %q, want %q", snapshot.Path, rootAbs)
	}
	if snapshot.LibraryID != "library-1" {
		t.Fatalf("LibraryID = %q, want library-1", snapshot.LibraryID)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2: %#v", len(snapshot.Files), snapshot.Files)
	}
	if _, ok := snapshot.Files[filepath.Join(root, "linked.mkv")]; ok {
		t.Fatalf("Files contains symlink path, want only regular files")
	}
	file, ok := snapshot.Files[regularAbs]
	if !ok {
		t.Fatalf("Files missing regular file %q", regularAbs)
	}
	if file.Path != regularAbs || file.Size != int64(len("movie")) || file.ModTime != modTime.UnixNano() {
		t.Fatalf("regular file info = %#v, want path %q size %d mod time %d", file, regularAbs, len("movie"), modTime.UnixNano())
	}
	if _, ok := snapshot.Files[nestedAbs]; !ok {
		t.Fatalf("Files missing nested regular file %q", nestedAbs)
	}
}

func TestDiffDetectsAddedModifiedDeletedFiles(t *testing.T) {
	previous := MonitorSnapshot{
		MonitorName: "movies",
		LibraryID:   "library-1",
		Files: map[string]FileInfo{
			"/media/deleted.mkv":  {Path: "/media/deleted.mkv", Size: 10, ModTime: 100},
			"/media/modified.mkv": {Path: "/media/modified.mkv", Size: 20, ModTime: 200},
			"/media/same.mkv":     {Path: "/media/same.mkv", Size: 30, ModTime: 300},
		},
	}
	current := MonitorSnapshot{
		MonitorName: "movies",
		LibraryID:   "library-1",
		Files: map[string]FileInfo{
			"/media/added.mkv":    {Path: "/media/added.mkv", Size: 40, ModTime: 400},
			"/media/modified.mkv": {Path: "/media/modified.mkv", Size: 21, ModTime: 201},
			"/media/same.mkv":     {Path: "/media/same.mkv", Size: 30, ModTime: 300},
		},
	}

	changes := DiffMonitor(previous, current)

	want := []Change{
		{MonitorName: "movies", LibraryID: "library-1", Path: "/media/added.mkv", Type: "added", Size: 40, ModTime: 400},
		{MonitorName: "movies", LibraryID: "library-1", Path: "/media/deleted.mkv", Type: "deleted", Size: 10, ModTime: 100},
		{MonitorName: "movies", LibraryID: "library-1", Path: "/media/modified.mkv", Type: "modified", Size: 21, ModTime: 201},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("DiffMonitor() = %#v, want %#v", changes, want)
	}
}

func TestChangedLibrariesAreDeduplicated(t *testing.T) {
	changes := []Change{
		{MonitorName: "movies-a", LibraryID: "library-shared", Path: "/media/a.mkv", Type: "added"},
		{MonitorName: "movies-b", LibraryID: "library-shared", Path: "/media/b.mkv", Type: "modified"},
		{MonitorName: "shows", LibraryID: "library-shows", Path: "/media/c.mkv", Type: "deleted"},
	}

	got := ChangedLibraryIDs(changes)
	want := []string{"library-shared", "library-shows"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedLibraryIDs() = %#v, want %#v", got, want)
	}
}

func TestChangeJSONFieldNames(t *testing.T) {
	data, err := json.Marshal(Change{
		MonitorName: "movies",
		LibraryID:   "library-1",
		Path:        "/media/movie.mkv",
		Type:        "added",
		Size:        42,
		ModTime:     123456789,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	wantFields := []string{"monitor_name", "library_id", "path", "type", "size", "mod_time"}
	for _, field := range wantFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("marshaled Change missing field %q in %s", field, data)
		}
	}
	for field := range fields {
		if !contains(wantFields, field) {
			t.Fatalf("marshaled Change contains unexpected field %q in %s", field, data)
		}
	}
}

func TestScanMissingDirectoryReturnsErrorWithoutSnapshot(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing")

	snapshot, err := ScanMonitor("missing", missingPath, "library-1")
	if err == nil {
		t.Fatalf("ScanMonitor() error = nil, want error")
	}
	if snapshot.Files != nil || snapshot.MonitorName != "" || snapshot.Path != "" || snapshot.LibraryID != "" {
		t.Fatalf("ScanMonitor() snapshot = %#v, want zero value", snapshot)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
