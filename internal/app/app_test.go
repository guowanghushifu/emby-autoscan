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

	"github.com/guowanghushifu/emby-autoscan/internal/config"
	"github.com/guowanghushifu/emby-autoscan/internal/logging"
	"github.com/guowanghushifu/emby-autoscan/internal/snapshot"
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

func TestRunOnceCachesStateAndSkipsSaveWhenUnchanged(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{NotifyOnFirstScan: false}, scanner, store, notifier, nil)

	if err := app.RunOnce(context.Background(), "cycle-1"); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	if err := app.RunOnce(context.Background(), "cycle-2"); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}

	if store.loadCount != 1 {
		t.Fatalf("Load() count = %d, want cached state after first load", store.loadCount)
	}
	if store.saveCount != 1 {
		t.Fatalf("Save() count = %d, want only first baseline save", store.saveCount)
	}
	if len(notifier.libraryIDs) != 0 {
		t.Fatalf("notified library IDs = %#v, want none for unchanged cached scan", notifier.libraryIDs)
	}
}

func TestRunOnceSuppressesUnchangedSummaryByDefault(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-unchanged"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if logs.String() != "" {
		t.Fatalf("logs = %q, want no unchanged summary when debug is disabled", logs.String())
	}
}

func TestRunOnceLogsUnchangedSummaryWhenDebugEnabled(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)
	app.Config.Logging.Debug = true

	if err := app.RunOnce(context.Background(), "cycle-unchanged-debug"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	want := "扫描完成：1 个目录，0 个变化，耗时 0.0s"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("logs missing %q in:\n%s", want, logs.String())
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

func TestRunOnceIgnoresUnmatchedExtensionsForLogsAndNotifyButSavesState(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/movie.nfo", 100, 1000),
			fileInfo("/media/movie1/old.jpg", 200, 2000),
		),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/movie.nfo", 101, 1001),
			fileInfo("/media/movie1/poster.jpg", 300, 3000),
		),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-ignored-extensions"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if len(notifier.libraryIDs) != 0 {
		t.Fatalf("notified library IDs = %#v, want none for ignored extensions", notifier.libraryIDs)
	}
	assertSavedMonitors(t, store, []string{"Movie1"})
	if got := store.saved.Monitors["Movie1"]; !reflect.DeepEqual(got, scanner.snapshots["Movie1"]) {
		t.Fatalf("saved Movie1 snapshot = %#v, want full scanned snapshot %#v", got, scanner.snapshots["Movie1"])
	}
	output := logs.String()
	if strings.Contains(output, "新增文件") || strings.Contains(output, "修改文件") || strings.Contains(output, "删除文件") {
		t.Fatalf("logs contain ignored extension file change in:\n%s", output)
	}
	wantSummary := "扫描完成：1 个目录，0 个需通知变化，忽略 3 个其他变化，耗时 0.0s"
	if !strings.Contains(output, wantSummary) {
		t.Fatalf("logs missing %q in:\n%s", wantSummary, output)
	}
}

func TestRunOnceFiltersNotifyByConfiguredExtensionsCaseInsensitively(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/old.nfo", 10, 10),
		),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/episode.MP4", 20, 20),
			fileInfo("/media/movie1/poster.jpg", 30, 30),
		),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{NotifyExtensions: []string{".mp4"}}, scanner, store, notifier, &logs)

	if err := app.RunOnce(context.Background(), "cycle-custom-extensions"); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	wantLibraries := []string{"library-movies"}
	if !reflect.DeepEqual(notifier.libraryIDs, wantLibraries) {
		t.Fatalf("notified library IDs = %#v, want %#v", notifier.libraryIDs, wantLibraries)
	}
	assertSavedMonitors(t, store, []string{"Movie1"})
	output := logs.String()
	wantParts := []string{
		"新增文件：Movie1 / episode.MP4，0.0 GiB，媒体库ID library-movies",
		"扫描完成：1 个目录，新增 1，修改 0，删除 0；已通知 1/1，忽略 2 个其他变化，耗时 0.0s",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
	if strings.Contains(output, "poster.jpg") || strings.Contains(output, "old.nfo") {
		t.Fatalf("logs contain ignored extension paths in:\n%s", output)
	}
}

func TestRunOnceNotificationFailureStillSavesStateAndReturnsNil(t *testing.T) {
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

	if err := app.RunOnce(context.Background(), "cycle-notify-fail"); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil after logging notification failure", err)
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
		"新增文件：Movie1 / movie1.mkv，0.0 GiB，媒体库ID library-movies",
		"新增文件：Movie2 / movie2.mkv，0.0 GiB，媒体库ID library-shows",
		"通知 Emby 扫描媒体库失败",
		"扫描完成：2 个目录，新增 2，修改 0，删除 0；已通知 1/2，耗时 0.0s",
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
		"扫描状态保存失败",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
	if strings.Contains(output, "扫描完成：1 个目录，0 个变化") {
		t.Fatalf("logs contain unchanged summary with debug disabled:\n%s", output)
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

func TestRunOnceSkipsScanWhenRcloneMountIsNotRunning(t *testing.T) {
	var logs bytes.Buffer
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{}, scanner, store, notifier, &logs)
	app.MountChecker = fakeMountChecker{running: false}

	if err := app.RunOnce(context.Background(), "cycle-rclone-down"); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	if store.loadCount != 0 {
		t.Fatalf("Load() count = %d, want 0", store.loadCount)
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

	output := logs.String()
	wantParts := []string{
		"未检测到 rclone mount 进程，跳过本轮扫描",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
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

func TestRunLogsCycleErrorAndRetriesUntilCanceled(t *testing.T) {
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/old.mkv", 10, 10)),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/new.mkv", 20, 20)),
	}}
	store := &fakeStore{state: previous, exists: true}
	notifier := &fakeNotifier{err: errors.New("emby unavailable")}
	app := newTestApp(t, config.ScanConfig{Interval: time.Millisecond}, scanner, store, notifier, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := app.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline after retry window", err)
	}
	if len(scanner.monitors) < 2 {
		t.Fatalf("scanned monitors = %#v, want retry after first cycle error", scanner.monitors)
	}
}

func TestRunRetriesStateLoadFailureUntilCanceled(t *testing.T) {
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies", fileInfo("/media/movie1.mkv", 100, 1000)),
	}}
	store := &fakeStore{loadErr: errors.New("state temporarily unavailable")}
	notifier := &fakeNotifier{}
	app := newTestApp(t, config.ScanConfig{Interval: time.Millisecond}, scanner, store, notifier, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := app.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline after retry window", err)
	}
	if store.loadCount < 2 {
		t.Fatalf("Load() count = %d, want retries after load failure", store.loadCount)
	}
	if len(scanner.monitors) != 0 {
		t.Fatalf("scanned monitors = %#v, want none while state load fails", scanner.monitors)
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

func TestRunOnceLogsAddedFilesAndSingleScanSummary(t *testing.T) {
	var logs bytes.Buffer
	previous := snapshot.State{Version: 1, Monitors: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/deleted.mkv", 10, 10),
			fileInfo("/media/movie1/modified.mkv", 20, 20),
		),
	}}
	scanner := &fakeScanner{snapshots: map[string]snapshot.MonitorSnapshot{
		"Movie1": monitorSnapshot("Movie1", "library-movies",
			fileInfo("/media/movie1/added.mkv", 30, 30),
			fileInfo("/media/movie1/modified.mkv", 21, 21),
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
		"新增文件：Movie1 / added.mkv，0.0 GiB，媒体库ID library-movies",
		"修改文件：Movie1 / modified.mkv，0.0 GiB，媒体库ID library-movies",
		"删除文件：Movie1 / deleted.mkv，0.0 GiB，媒体库ID library-movies",
		"扫描完成：1 个目录，新增 1，修改 1，删除 1；已通知 1/1，耗时 0.0s",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("logs missing %q in:\n%s", part, output)
		}
	}
	for _, unwanted := range []string{
		"cycle_id=",
		"mod_time=",
		"size=30",
		"event=scan_start",
		"event=notify_start",
		"event=notify_success",
		"event=state_save success=true",
		"event=scan_finish",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("logs contain unwanted %q in:\n%s", unwanted, output)
		}
	}
	if got := strings.Count(output, "event=file_change"); got != 0 {
		t.Fatalf("stdout file change event count = %d, want no structured event fields in:\n%s", got, output)
	}
	if got := strings.Count(output, "event=scan_summary"); got != 0 {
		t.Fatalf("stdout scan summary event count = %d, want no structured event fields in:\n%s", got, output)
	}
	if got := strings.Count(output, "文件：Movie1 /"); got != 3 {
		t.Fatalf("file change log count = %d, want added, modified, and deleted logs in:\n%s", got, output)
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
	loadCount int
	saveCount int
}

func (f *fakeStore) Load() (snapshot.State, bool, error) {
	f.loadCount++
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

type fakeMountChecker struct {
	running bool
	err     error
}

func (f fakeMountChecker) RcloneMountRunning() (bool, error) {
	return f.running, f.err
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
