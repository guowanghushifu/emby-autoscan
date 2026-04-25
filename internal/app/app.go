package app

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/guowanghushifu/emby-autoscan/internal/config"
	"github.com/guowanghushifu/emby-autoscan/internal/emby"
	"github.com/guowanghushifu/emby-autoscan/internal/logging"
	"github.com/guowanghushifu/emby-autoscan/internal/snapshot"
)

type Scanner interface {
	Scan(config.MonitorConfig) (snapshot.MonitorSnapshot, error)
}

type Store interface {
	Load() (snapshot.State, bool, error)
	Save(snapshot.State) error
}

type stateFilePathStore interface {
	StateFilePath() string
}

type Notifier interface {
	RefreshLibrary(context.Context, string) error
}

type MountChecker interface {
	RcloneMountRunning() (bool, error)
}

type App struct {
	Config       config.Config
	Scanner      Scanner
	Store        Store
	Notifier     Notifier
	MountChecker MountChecker
	Logger       *logging.Logger

	stateLoaded bool
	stateExists bool
	stateCache  snapshot.State
}

func (a *App) RunOnce(ctx context.Context, _ string) error {
	startedAt := time.Now()

	if a.MountChecker != nil {
		running, err := a.MountChecker.RcloneMountRunning()
		if err != nil {
			a.logError("rclone_mount_check_failed", "检测 rclone mount 进程失败，跳过本轮扫描",
				logging.F("error", err),
			)
			return nil
		}
		if !running {
			a.logError("rclone_mount_missing", "未检测到 rclone mount 进程，跳过本轮扫描",
				logging.F("rclone_exe", "/usr/bin/rclone"),
				logging.F("rclone_command", "mount"),
			)
			return nil
		}
	}

	previous, exists, err := a.loadState()
	if err != nil {
		return fmt.Errorf("load snapshot state: %w", err)
	}
	if previous.Monitors == nil {
		previous.Monitors = make(map[string]snapshot.MonitorSnapshot)
	}

	next := snapshot.State{
		Version:  1,
		Monitors: make(map[string]snapshot.MonitorSnapshot, len(a.Config.Monitors)),
	}

	allChanges := make([]snapshot.Change, 0)
	notifyChanges := make([]snapshot.Change, 0)
	failedMonitorCount := 0
	scannedMonitorCount := 0
	notifyExtensionSet := notifyExtensions(a.Config.Scan.NotifyExtensions)

	for _, monitor := range a.Config.Monitors {
		current, err := a.Scanner.Scan(monitor)
		if err != nil {
			failedMonitorCount++
			if previousMonitor, ok := previous.Monitors[monitor.Name]; ok {
				next.Monitors[monitor.Name] = previousMonitor
			}
			a.logError("scan_monitor_failed", "目录检测失败",
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
			if !notifiesForExtension(change, notifyExtensionSet) {
				continue
			}
			logFileChange(a, change)
			notifyChanges = append(notifyChanges, change)
		}
		allChanges = append(allChanges, changes...)
	}

	changedLibraryIDs := snapshot.ChangedLibraryIDs(notifyChanges)
	notifySuccessCount := 0
	notifyFailedCount := 0
	logScanSummary := func() {
		endedAt := time.Now()
		addedCount, modifiedCount, deletedCount := changeCounts(notifyChanges)
		elapsedSeconds := seconds1String(endedAt.Sub(startedAt))
		a.logInfo("scan_summary", scanSummaryMessage(
			len(a.Config.Monitors),
			elapsedSeconds,
			len(changedLibraryIDs),
			addedCount,
			modifiedCount,
			deletedCount,
			len(allChanges)-len(notifyChanges),
			notifySuccessCount,
			notifyFailedCount,
		),
			logging.F("monitor_count", len(a.Config.Monitors)),
			logging.F("elapsed_seconds", elapsedSeconds),
			logging.F("scanned_monitor_count", scannedMonitorCount),
			logging.F("failed_monitor_count", failedMonitorCount),
			logging.F("changed_library_count", len(changedLibraryIDs)),
			logging.F("added_count", addedCount),
			logging.F("modified_count", modifiedCount),
			logging.F("deleted_count", deletedCount),
			logging.F("ignored_change_count", len(allChanges)-len(notifyChanges)),
			logging.F("notify_success_count", notifySuccessCount),
			logging.F("notify_failed_count", notifyFailedCount),
		)
	}

	for _, libraryID := range changedLibraryIDs {
		notifyStartedAt := time.Now()
		requestPath := emby.RefreshPath(libraryID)
		if err := a.Notifier.RefreshLibrary(ctx, libraryID); err != nil {
			notifyFailedCount++
			a.logError("notify_failed", "通知 Emby 扫描媒体库失败",
				logging.F("library_id", libraryID),
				logging.F("request_path", requestPath),
				logging.F("elapsed_seconds", seconds1String(time.Since(notifyStartedAt))),
				logging.F("error", err),
			)
			continue
		}
		notifySuccessCount++
	}

	if !a.shouldSaveState(previous, next, exists) {
		a.stateCache = next
		a.stateExists = true
		logScanSummary()
		return nil
	}

	if err := a.Store.Save(next); err != nil {
		a.logError("state_save", "扫描状态保存失败",
			logging.F("state_file", a.stateFilePath()),
			logging.F("success", false),
			logging.F("error", err),
		)
		logScanSummary()
		return nil
	}
	a.stateCache = next
	a.stateExists = true

	logScanSummary()

	return nil
}

func (a *App) loadState() (snapshot.State, bool, error) {
	if a.stateLoaded {
		return a.stateCache, a.stateExists, nil
	}

	state, exists, err := a.Store.Load()
	if err != nil {
		return snapshot.State{}, false, err
	}
	a.stateCache = state
	a.stateExists = exists
	a.stateLoaded = true
	return state, exists, nil
}

func (a *App) shouldSaveState(previous, next snapshot.State, exists bool) bool {
	if !exists {
		return true
	}
	return !reflect.DeepEqual(previous, next)
}

func changeCounts(changes []snapshot.Change) (added, modified, deleted int) {
	for _, change := range changes {
		switch change.Type {
		case snapshot.ChangeAdded:
			added++
		case snapshot.ChangeModified:
			modified++
		case snapshot.ChangeDeleted:
			deleted++
		}
	}
	return added, modified, deleted
}

func scanSummaryMessage(monitorCount int, elapsedSeconds string, changedLibraryCount, addedCount, modifiedCount, deletedCount, ignoredChangeCount, notifySuccessCount, notifyFailedCount int) string {
	if changedLibraryCount == 0 {
		if ignoredChangeCount > 0 {
			return fmt.Sprintf("扫描完成：%d 个目录，0 个需通知变化，忽略 %d 个其他变化，耗时 %ss", monitorCount, ignoredChangeCount, elapsedSeconds)
		}
		return fmt.Sprintf("扫描完成：%d 个目录，0 个变化，耗时 %ss", monitorCount, elapsedSeconds)
	}
	if ignoredChangeCount > 0 {
		return fmt.Sprintf(
			"扫描完成：%d 个目录，新增 %d，修改 %d，删除 %d；已通知 %d/%d，忽略 %d 个其他变化，耗时 %ss",
			monitorCount,
			addedCount,
			modifiedCount,
			deletedCount,
			notifySuccessCount,
			notifySuccessCount+notifyFailedCount,
			ignoredChangeCount,
			elapsedSeconds,
		)
	}
	return fmt.Sprintf(
		"扫描完成：%d 个目录，新增 %d，修改 %d，删除 %d；已通知 %d/%d，耗时 %ss",
		monitorCount,
		addedCount,
		modifiedCount,
		deletedCount,
		notifySuccessCount,
		notifySuccessCount+notifyFailedCount,
		elapsedSeconds,
	)
}

func seconds1(duration time.Duration) float64 {
	return float64(duration.Round(100*time.Millisecond)) / float64(time.Second)
}

func seconds1String(duration time.Duration) string {
	return fmt.Sprintf("%.1f", seconds1(duration))
}

func (a *App) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Config.Scan.Interval <= 0 {
		return fmt.Errorf("scan interval must be positive")
	}

	if err := a.RunOnce(ctx, newCycleID()); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		a.logError("scan_cycle_failed", "本轮目录检测失败，将等待下一个周期重试", logging.F("error", err))
	}

	ticker := time.NewTicker(a.Config.Scan.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := a.RunOnce(ctx, newCycleID()); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				a.logError("scan_cycle_failed", "本轮目录检测失败，将等待下一个周期重试", logging.F("error", err))
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

func notifyExtensions(extensions []string) map[string]struct{} {
	if extensions == nil {
		extensions = config.DefaultNotifyExtensions()
	}

	extensionSet := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		normalized := strings.ToLower(strings.TrimSpace(extension))
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, ".") {
			normalized = "." + normalized
		}
		extensionSet[normalized] = struct{}{}
	}
	return extensionSet
}

func notifiesForExtension(change snapshot.Change, extensionSet map[string]struct{}) bool {
	_, ok := extensionSet[strings.ToLower(filepath.Ext(change.Path))]
	return ok
}

func logFileChange(a *App, change snapshot.Change) {
	message := fileChangeMessage(a, change)

	a.logInfo("file_change", message,
		logging.F("monitor", change.MonitorName),
		logging.F("path", change.Path),
		logging.F("library_id", change.LibraryID),
		logging.F("change_type", change.Type),
		logging.F("size_gib", gib1String(change.Size)),
	)
}

func fileChangeMessage(a *App, change snapshot.Change) string {
	action := map[string]string{
		snapshot.ChangeAdded:    "新增文件",
		snapshot.ChangeModified: "修改文件",
		snapshot.ChangeDeleted:  "删除文件",
	}[change.Type]
	if action == "" {
		action = "文件变化"
	}

	return fmt.Sprintf(
		"%s：%s / %s，%s GiB，媒体库ID %s",
		action,
		change.MonitorName,
		displayChangePath(a, change),
		gib1String(change.Size),
		change.LibraryID,
	)
}

func displayChangePath(a *App, change snapshot.Change) string {
	for _, monitor := range a.Config.Monitors {
		if monitor.Name != change.MonitorName {
			continue
		}
		relative, err := filepath.Rel(monitor.Path, change.Path)
		if err == nil && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." {
			return filepath.ToSlash(relative)
		}
		break
	}

	base := filepath.Base(change.Path)
	if base == "." || base == string(filepath.Separator) {
		return change.Path
	}
	return base
}

func gib1(bytes int64) float64 {
	const gib = 1024 * 1024 * 1024
	return float64((bytes*10+gib/2)/gib) / 10
}

func gib1String(bytes int64) string {
	return fmt.Sprintf("%.1f", gib1(bytes))
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

func (a *App) stateFilePath() string {
	if a.Config.Scan.StateFile != "" {
		return a.Config.Scan.StateFile
	}
	store, ok := a.Store.(stateFilePathStore)
	if !ok {
		return ""
	}
	return store.StateFilePath()
}
