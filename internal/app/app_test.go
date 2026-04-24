package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/config"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/logging"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/snapshot"
)

func TestRunOnceFirstScanSavesBaselineWithoutNotify(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{NotifyOnFirstScan: false}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-1"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if len(notifier.libraryIDs) != 0 {
		t.Fatalf("notified library IDs = %#v, want none", notifier.libraryIDs)
	}
	assertSavedMonitors(t, store, []string{"Movie1"})
	if got := store.saved.Monitors["Movie1"]; !reflect.DeepEqual(got, scanner.snapshots["Movie1"]) {
		t.Fatalf("saved Movie1 snapshot = %#v, want %#v", got, scanner.snapshots["Movie1"])
	}
}

func TestRunOnceNotifyOnFirstScanNotifiesChangedLibraries(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
		"Empty":  monitorSnapshot("Empty", "library-empty"),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{NotifyOnFirstScan: true}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-1"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	wantLibraries := []string{"library-movies"}
	if !reflect.DeepEqual(notifier.libraryIDs, wantLibraries) {
		t.Fatalf("notified library IDs = %#v, want %#v", notifier.libraryIDs, wantLibraries)
	}
	assertSavedMonitors(t, store, []string{"Empty", "Movie1"})
}

func TestRunOnceDeduplicatesSameLibraryAcrossMonitors(t *testing.T) {
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-shared"),
		"Movie2": monitorSnapshot("Movie2", "library-shared"),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-shared", fileInfo("/media/movie1.mkv", 100, 1000)),
		"Movie2": monitorSnapshot("Movie2", "library-shared", fileInfo("/media/movie2.mkv", 200, 2000)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-2"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	wantLibraries := []string{"library-shared"}
	if !reflect.DeepEqual(notifier.libraryIDs, wantLibraries) {
		t.Fatalf("notified library IDs = %#v, want %#v", notifier.libraryIDs, wantLibraries)
	}
}

func TestRunOnceNotificationFailureStillSavesStateAndContinues(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies"),
		"Movie2": monitorSnapshot("Movie2", "library-shows"),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
		"Movie2": monitorSnapshot("Movie2", "library-shows", fileInfo("/media/movie2.mkv", 200, 2000)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{errors: map[string]error{"library-movies": errors.New("emby unavailable")}}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-notify-fail"); err == nil {
		t.Fatalf("RunOnce() error = nil, want notification error")
	}

	if store.saveCount != 1 {
		t.Fatalf("Save() count = %d, want 1", store.saveCount)
	}
	wantLibraries := []string{"library-movies", "library-shows"}
	if !reflect.DeepEqual(notifier.libraryIDs, wantLibraries) {
		t.Fatalf("notified library IDs = %#v, want %#v", notifier.libraryIDs, wantLibraries)
	}
	assertSavedMonitors(t, store, []string{"Movie1", "Movie2"})
	if got := store.saved.Monitors["Movie1"]; !reflect.DeepEqual(got, scanner.snapshots["Movie1"]) {
		t.Fatalf("saved Movie1 snapshot = %#v, want %#v", got, scanner.snapshots["Movie1"])
	}

	output := logs.String()
	wantParts := []string{
		"event=notify_start",
		"library_id=library-movies",
		"request_path=/emby/Items/library-movies/Refresh",
		"event=notify_failed",
		"elapsed_seconds=",
		"error=\"emby unavailable\"",
		"library_id=library-shows",
		"event=notify_success",
		"status=success",
		"event=state_save",
		"success=true",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
}

func TestRunOnceStateSaveFailureLogsAndReturnsNil(t *testing.T) {
	var logs bytes.Buffer
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{saveErr: errors.New("disk full")}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{NotifyOnFirstScan: false, StateFile: "/tmp/state.json"}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-save-fail"); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if store.saveCount != 1 {
		t.Fatalf("Save() count = %d, want 1", store.saveCount)
	}

	output := logs.String()
	wantParts := []string{
		"event=state_save",
		"msg=\"扫描状态保存失败\"",
		"cycle_id=cycle-save-fail",
		"state_file=/tmp/state.json",
		"success=false",
		"error=\"disk full\"",
		"event=scan_finish",
		"changed_library_count=0",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
}

func TestRunOnceSkipsStateUpdateForFailedMonitor(t *testing.T) {
	oldMovie1 := monitorSnapshot("Movie1", "library-movies", fileInfo("/media/old.mkv", 10, 10))
	oldMovie2 := monitorSnapshot("Movie2", "library-shows", fileInfo("/media/old-episode.mkv", 20, 20))
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": oldMovie1,
		"Movie2": oldMovie2,
	}}
	scanner := &fakeScanner{
		snapshots: map[string]snapshot.MonitorSnapshot{
			"Movie2": monitorSnapshot("Movie2", "library-shows", fileInfo("/media/new-episode.mkv", 30, 30)),
		},
		errors: map[string]error{"Movie1": errors.New("permission denied")},
	}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-3"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if got := store.saved.Monitors["Movie1"]; !reflect.DeepEqual(got, oldMovie1) {
		t.Fatalf("saved failed Movie1 = %#v, want preserved %#v", got, oldMovie1)
	}
	if got := store.saved.Monitors["Movie2"]; reflect.DeepEqual(got, oldMovie2) {
		t.Fatalf("saved successful Movie2 was not updated: %#v", got)
	}
	wantLibraries := []string{"library-shows"}
	if !reflect.DeepEqual(notifier.libraryIDs, wantLibraries) {
		t.Fatalf("notified library IDs = %#v, want %#v", notifier.libraryIDs, wantLibraries)
	}
}

func TestRunOnceDropsRemovedConfiguredMonitorFromSavedState(t *testing.T) {
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1":  monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1-old.mkv", 10, 10)),
		"Removed": monitorSnapshot("Removed", "library-removed", fileInfo("/media/removed.mkv", 20, 20)),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1-new.mkv", 30, 30)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-removed"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	assertSavedMonitors(t, store, []string{"Movie1"})
	if _, ok := store.saved.Monitors["Removed"]; ok {
		t.Fatalf("saved state preserved removed monitor: %#v", store.saved.Monitors)
	}
}

func TestRunWithAlreadyCanceledContextDoesNotScanAndReturnsCanceled(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{Interval: time.Minute}, scanner, store, notifier, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := app.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(scanner.monitors) != 0 {
		t.Fatalf("scanned monitors = %#v, want none", scanner.monitors)
	}
	if store.saveCount != 0 {
		t.Fatalf("Save() count = %d, want 0", store.saveCount)
	}
	if len(notifier.libraryIDs) != 0 {
		t.Fatalf("notified library IDs = %#v, want none", notifier.libraryIDs)
	}
}

func TestRunRejectsNonPositiveIntervalWithoutPanic(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{Interval: 0}, scanner, store, notifier, nil)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Run() panicked with non-positive interval: %v", recovered)
		}
	}()
	err := app.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "scan interval must be positive") {
		t.Fatalf("Run() error = %v, want positive interval error", err)
	}
	if len(scanner.monitors) != 0 {
		t.Fatalf("scanned monitors = %#v, want none", scanner.monitors)
	}
	if store.saveCount != 0 {
		t.Fatalf("Save() count = %d, want 0", store.saveCount)
	}
}

func TestRunOnceLogsChineseScanChangeAndNotifyMessages(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/deleted.mkv", 10, 10),
			fileInfo("/media/modified.mkv", 20, 20),
		),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/added.mkv", 30, 30),
			fileInfo("/media/modified.mkv", 21, 21),
		),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-4"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	output := logs.String()
	wantParts := []string{
		"event=scan_start",
		"msg=\"开始执行目录检测\"",
		"cycle_id=cycle-4",
		"start_time=",
		"monitor_count=1",
		"event=file_change",
		"msg=\"检测到文件新增\"",
		"msg=\"检测到文件修改\"",
		"msg=\"检测到文件删除\"",
		"monitor=Movie1",
		"path=/media/added.mkv",
		"library_id=library-movies",
		"change_type=added",
		"size=30",
		"mod_time=30",
		"msg=\"开始通知 Emby 扫描媒体库\"",
		"request_path=/emby/Items/library-movies/Refresh",
		"msg=\"通知 Emby 扫描媒体库成功\"",
		"status=success",
		"event=state_save",
		"msg=\"扫描状态保存成功\"",
		"success=true",
		"event=scan_finish",
		"msg=\"目录检测完成\"",
		"end_time=",
		"elapsed_seconds=",
		"scanned_monitor_count=1",
		"failed_monitor_count=0",
		"changed_library_count=1",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
}

func newTestApp(t *testing.T, scan config.ScanConfig, scanner *fakeScanner, store *fakeStore, notifier *fakeNotifier, output *bytes.Buffer) *App {
	t.Helper()
	if output == nil {
		output = &bytes.Buffer{}
	}
	logger, err := logging.New(output, t.TempDir(), 7, func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	})
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	return &App{
		Config: config.Config{
			Scan:     scan,
			Monitors: testMonitors(scanner),
		},
		Scanner:  scanner,
		Store:    store,
		Notifier: notifier,
		Logger:   logger,
	}
}

func testMonitors(scanner *fakeScanner) []config.MonitorConfig {
	monitorNames := make(map[string]struct{})
	for name := range scanner.snapshots {
		monitorNames[name] = struct{}{}
	}
	for name := range scanner.errors {
		monitorNames[name] = struct{}{}
	}

	names := make([]string, 0, len(monitorNames))
	for name := range monitorNames {
		names = append(names, name)
	}
	sort.Strings(names)

	monitors := make([]config.MonitorConfig, 0, len(names))
	for _, name := range names {
		libraryID := defaultLibraryID(name)
		if scanned, ok := scanner.snapshots[name]; ok {
			libraryID = scanned.LibraryID
		}
		monitors = append(monitors, config.MonitorConfig{
			Name:      name,
			Path:      "/media/" + strings.ToLower(name),
			LibraryID: libraryID,
		})
	}
	return monitors
}

func defaultLibraryID(name string) string {
	switch name {
	case "Movie1":
		return "library-movies"
	case "Movie2":
		return "library-shows"
	case "Empty":
		return "library-empty"
	default:
		return "library-" + strings.ToLower(name)
	}
}

func monitorSnapshot(name, libraryID string, files ...snapshot.FileInfo) snapshot.MonitorSnapshot {
	fileMap := make(map[string]snapshot.FileInfo, len(files))
	for _, file := range files {
		fileMap[file.Path] = file
	}
	return snapshot.MonitorSnapshot{
		MonitorName: name,
		Path:        "/media/" + strings.ToLower(name),
		LibraryID:   libraryID,
		Files:       fileMap,
	}
}

func fileInfo(path string, size, modTime int64) snapshot.FileInfo {
	return snapshot.FileInfo{Path: path, Size: size, ModTime: modTime}
}

func assertSavedMonitors(t *testing.T, store *fakeStore, wantNames []string) {
	t.Helper()
	if store.saveCount != 1 {
		t.Fatalf("Save() count = %d, want 1", store.saveCount)
	}
	gotNames := make([]string, 0, len(store.saved.Monitors))
	for name := range store.saved.Monitors {
		gotNames = append(gotNames, name)
	}
	if !reflect.DeepEqual(sorted(gotNames), sorted(wantNames)) {
		t.Fatalf("saved monitor names = %#v, want %#v", sorted(gotNames), sorted(wantNames))
	}
}

func sorted(values []string) []string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	return copyValues
}

type fakeScanner struct {
	snapshots map[string]snapshot.MonitorSnapshot
	errors    map[string]error
	monitors  []string
}

func (f *fakeScanner) Scan(monitor config.MonitorConfig) (snapshot.MonitorSnapshot, error) {
	f.monitors = append(f.monitors, monitor.Name)
	if err := f.errors[monitor.Name]; err != nil {
		return snapshot.MonitorSnapshot{}, err
	}
	monitorSnapshot, ok := f.snapshots[monitor.Name]
	if !ok {
		return snapshot.MonitorSnapshot{}, errors.New("unexpected monitor " + monitor.Name)
	}
	return monitorSnapshot, nil
}

type fakeStore struct {
	state     snapshot.State
	exists    bool
	loadErr   error
	saveErr   error
	saved     snapshot.State
	saveCount int
}

func (f *fakeStore) Load() (snapshot.State, bool, error) {
	return f.state, f.exists, f.loadErr
}

func (f *fakeStore) Save(state snapshot.State) error {
	f.saved = state
	f.saveCount++
	return f.saveErr
}

type fakeNotifier struct {
	libraryIDs []string
	err        error
	errors     map[string]error
}

func (f *fakeNotifier) RefreshLibrary(ctx context.Context, libraryID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.libraryIDs = append(f.libraryIDs, libraryID)
	if err := f.errors[libraryID]; err != nil {
		return err
	}
	return f.err
}
