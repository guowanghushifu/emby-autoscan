package snapshot

import (
	"io/fs"
	"path/filepath"
	"sort"
)

const (
	ChangeAdded    = "added"
	ChangeModified = "modified"
	ChangeDeleted  = "deleted"
)

type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

type MonitorSnapshot struct {
	MonitorName string              `json:"monitor_name"`
	Path        string              `json:"path"`
	LibraryID   string              `json:"library_id"`
	Files       map[string]FileInfo `json:"files"`
}

type State struct {
	Version  int                        `json:"version"`
	Monitors map[string]MonitorSnapshot `json:"monitors"`
}

type Change struct {
	MonitorName string `json:"monitor_name"`
	LibraryID   string `json:"library_id"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	ModTime     int64  `json:"mod_time"`
}

func ScanMonitor(name, root, libraryID string) (MonitorSnapshot, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return MonitorSnapshot{}, err
	}

	files := make(map[string]FileInfo)
	err = filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		files[absolutePath] = FileInfo{
			Path:    absolutePath,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		}

		return nil
	})
	if err != nil {
		return MonitorSnapshot{}, err
	}

	return MonitorSnapshot{
		MonitorName: name,
		Path:        absoluteRoot,
		LibraryID:   libraryID,
		Files:       files,
	}, nil
}

func DiffMonitor(previous, current MonitorSnapshot) []Change {
	changes := make([]Change, 0)

	for path, currentFile := range current.Files {
		previousFile, ok := previous.Files[path]
		if !ok {
			changes = append(changes, changeFromFile(current, currentFile, ChangeAdded))
			continue
		}
		if previousFile.Size != currentFile.Size || previousFile.ModTime != currentFile.ModTime {
			changes = append(changes, changeFromFile(current, currentFile, ChangeModified))
		}
	}

	for path, previousFile := range previous.Files {
		if _, ok := current.Files[path]; !ok {
			changes = append(changes, changeFromFile(previous, previousFile, ChangeDeleted))
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path == changes[j].Path {
			return changes[i].Type < changes[j].Type
		}
		return changes[i].Path < changes[j].Path
	})

	return changes
}

func ChangedLibraryIDs(changes []Change) []string {
	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		seen[change.LibraryID] = struct{}{}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	return ids
}

func changeFromFile(snapshot MonitorSnapshot, file FileInfo, changeType string) Change {
	return Change{
		MonitorName: snapshot.MonitorName,
		LibraryID:   snapshot.LibraryID,
		Path:        file.Path,
		Type:        changeType,
		Size:        file.Size,
		ModTime:     file.ModTime,
	}
}
