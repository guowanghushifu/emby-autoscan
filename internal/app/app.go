package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/config"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/logging"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/snapshot"
)

type Scanner interface {
	Scan(config.MonitorConfig) (snapshot.MonitorSnapshot, error)
}

type Store interface {
	Load() (snapshot.State, bool, error)
	Save(snapshot.State) error
}

type Notifier interface {
	RefreshLibrary(context.Context, string) error
}

type App struct {
	Config   config.Config
	Scanner  Scanner
	Store    Store
	Notifier Notifier
	Logger   *logging.Logger
}

func (a *App) RunOnce(ctx context.Context, cycleID string) error {
	startedAt := time.Now()
	a.logInfo("scan_start", "开始执行目录检测", logging.F("cycle_id", cycleID))

	previous, exists, err := a.Store.Load()
	if err != nil {
		return fmt.Errorf("load snapshot state: %w", err)
	}
	if previous.Monitors == nil {
		previous.Monitors = make(map[string]snapshot.MonitorSnapshot)
	}

	next := snapshot.State{
		Version:  1,
		Monitors: cloneMonitors(previous.Monitors),
	}

	allChanges := make([]snapshot.Change, 0)
	failedMonitorCount := 0
	scannedMonitorCount := 0

	for _, monitor := range a.Config.Monitors {
		current, err := a.Scanner.Scan(monitor)
		if err != nil {
			failedMonitorCount++
			a.logError("scan_monitor_failed", "目录检测失败",
				logging.F("cycle_id", cycleID),
				logging.F("monitor", monitor.Name),
				logging.F("library_id", monitor.LibraryID),
				logging.F("error", err),
			)
			continue
		}

		scannedMonitorCount++
		next.Monitors[monitor.Name] = current

		changes := changesForMonitor(previous.Monitors[monitor.Name], current, exists, a.Config.Scan.NotifyOnFirstScan)
		for _, change := range changes {
			logFileChange(a, cycleID, change)
		}
		allChanges = append(allChanges, changes...)
	}

	changedLibraryIDs := snapshot.ChangedLibraryIDs(allChanges)
	for _, libraryID := range changedLibraryIDs {
		a.logInfo("notify_start", "开始通知 Emby 扫描媒体库",
			logging.F("cycle_id", cycleID),
			logging.F("library_id", libraryID),
		)
		if err := a.Notifier.RefreshLibrary(ctx, libraryID); err != nil {
			a.logError("notify_failed", "通知 Emby 扫描媒体库失败",
				logging.F("cycle_id", cycleID),
				logging.F("library_id", libraryID),
				logging.F("error", err),
			)
			return fmt.Errorf("notify library %s: %w", libraryID, err)
		}
		a.logInfo("notify_success", "通知 Emby 扫描媒体库成功",
			logging.F("cycle_id", cycleID),
			logging.F("library_id", libraryID),
		)
	}

	if err := a.Store.Save(next); err != nil {
		return fmt.Errorf("save snapshot state: %w", err)
	}

	a.logInfo("scan_finish", "目录检测完成",
		logging.F("cycle_id", cycleID),
		logging.F("elapsed_seconds", time.Since(startedAt).Seconds()),
		logging.F("scanned_monitor_count", scannedMonitorCount),
		logging.F("failed_monitor_count", failedMonitorCount),
		logging.F("changed_library_count", len(changedLibraryIDs)),
	)

	return nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.RunOnce(ctx, newCycleID()); err != nil {
		return err
	}

	ticker := time.NewTicker(a.Config.Scan.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := a.RunOnce(ctx, newCycleID()); err != nil {
				return err
			}
		}
	}
}

func changesForMonitor(previous, current snapshot.MonitorSnapshot, stateExists, notifyOnFirstScan bool) []snapshot.Change {
	if !stateExists {
		if !notifyOnFirstScan {
			return nil
		}
		changes := make([]snapshot.Change, 0, len(current.Files))
		for _, file := range current.Files {
			changes = append(changes, snapshot.Change{
				MonitorName: current.MonitorName,
				LibraryID:   current.LibraryID,
				Path:        file.Path,
				Type:        snapshot.ChangeAdded,
				Size:        file.Size,
				ModTime:     file.ModTime,
			})
		}
		sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
		return changes
	}

	return snapshot.DiffMonitor(previous, current)
}

func logFileChange(a *App, cycleID string, change snapshot.Change) {
	message := map[string]string{
		snapshot.ChangeAdded:    "检测到文件新增",
		snapshot.ChangeModified: "检测到文件修改",
		snapshot.ChangeDeleted:  "检测到文件删除",
	}[change.Type]
	if message == "" {
		message = "检测到文件变化"
	}

	a.logInfo("file_change", message,
		logging.F("cycle_id", cycleID),
		logging.F("monitor", change.MonitorName),
		logging.F("path", change.Path),
		logging.F("library_id", change.LibraryID),
		logging.F("change_type", change.Type),
		logging.F("size", change.Size),
		logging.F("mod_time", change.ModTime),
	)
}

func cloneMonitors(monitors map[string]snapshot.MonitorSnapshot) map[string]snapshot.MonitorSnapshot {
	cloned := make(map[string]snapshot.MonitorSnapshot, len(monitors))
	for name, monitor := range monitors {
		cloned[name] = monitor
	}
	return cloned
}

func newCycleID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func (a *App) logInfo(event, message string, fields ...logging.Field) {
	if a.Logger != nil {
		a.Logger.Info(event, message, fields...)
	}
}

func (a *App) logError(event, message string, fields ...logging.Field) {
	if a.Logger != nil {
		a.Logger.Error(event, message, fields...)
	}
}
