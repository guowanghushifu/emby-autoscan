package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesChineseMessageToStdoutAndDailyFile(t *testing.T) {
	now := fixedTime()
	dir := t.TempDir()
	var stdout bytes.Buffer

	logger, err := New(&stdout, dir, 7, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	logger.Info("scan_start", "开始执行目录检测")

	wantParts := []string{
		"2026-04-24 12:00:00 [INFO]",
		"event=scan_start",
		"开始执行目录检测",
	}
	assertLineContains(t, stdout.String(), wantParts)

	data, err := os.ReadFile(filepath.Join(dir, "2026-04-24.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	assertLineContains(t, string(data), wantParts)
}

func TestLoggerRemovesDailyFilesOlderThanRetention(t *testing.T) {
	now := fixedTime()
	dir := t.TempDir()
	writeLogFile(t, dir, "2026-04-21.log")
	writeLogFile(t, dir, "2026-04-22.log")
	writeLogFile(t, dir, "2026-04-23.log")

	logger, err := New(&bytes.Buffer{}, dir, 2, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	if _, err := os.Stat(filepath.Join(dir, "2026-04-21.log")); !os.IsNotExist(err) {
		t.Fatalf("old daily log exists or unexpected stat error: %v", err)
	}
	for _, name := range []string{"2026-04-22.log", "2026-04-23.log", "2026-04-24.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("Stat(%s) error = %v", name, err)
		}
	}
}

func TestLoggerKeepsNonMatchingFiles(t *testing.T) {
	now := fixedTime()
	dir := t.TempDir()
	kept := []string{"2026-04-01.txt", "app.log", "2026-4-01.log", "prefix-2026-04-01.log"}
	for _, name := range kept {
		writeLogFile(t, dir, name)
	}

	logger, err := New(&bytes.Buffer{}, dir, 1, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("Stat(%s) error = %v", name, err)
		}
	}
}

func TestLoggerFallsBackToStdoutWhenFileSetupFails(t *testing.T) {
	now := fixedTime()
	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var stdout bytes.Buffer

	logger, err := New(&stdout, filePath, 7, func() time.Time { return now })
	if err == nil {
		t.Fatalf("New() error = nil, want file setup error")
	}
	if logger == nil {
		t.Fatalf("New() logger = nil, want stdout logger")
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Error("file_setup_failed", "开始执行目录检测", F("attempt", 1))

	assertLineContains(t, stdout.String(), []string{
		"2026-04-24 12:00:00 [ERROR]",
		"event=file_setup_failed",
		"开始执行目录检测",
		"attempt=1",
	})
}

func TestLoggerRotatesAcrossMidnight(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 4, 24, 23, 59, 0, 0, location)
	dir := t.TempDir()
	var stdout bytes.Buffer

	logger, err := New(&stdout, dir, 7, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("before_midnight", "before")
	now = time.Date(2026, 4, 25, 0, 1, 0, 0, location)
	logger.Info("after_midnight", "after")

	firstDay := readLogFile(t, dir, "2026-04-24.log")
	assertLineContains(t, firstDay, []string{"event=before_midnight", "before"})
	if strings.Contains(firstDay, "event=after_midnight") {
		t.Fatalf("first day log contains rotated event: %q", firstDay)
	}

	secondDay := readLogFile(t, dir, "2026-04-25.log")
	assertLineContains(t, secondDay, []string{"event=after_midnight", "after"})
	if strings.Contains(secondDay, "event=before_midnight") {
		t.Fatalf("second day log contains previous event: %q", secondDay)
	}
}

func TestLoggerClosesFileWhenRetentionCleanupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission behavior differs on Windows")
	}

	now := fixedTime()
	dir := t.TempDir()
	todayPath := filepath.Join(dir, "2026-04-24.log")
	if err := os.WriteFile(todayPath, []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(today) error = %v", err)
	}
	writeLogFile(t, dir, "2026-04-21.log")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod(read-only) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	var stdout bytes.Buffer

	logger, err := New(&stdout, dir, 2, func() time.Time { return now })
	if err == nil {
		t.Fatalf("New() error = nil, want retention cleanup error")
	}
	if logger == nil {
		t.Fatalf("New() logger = nil, want stdout logger")
	}

	logger.Info("after_cleanup_error", "stdout only")

	assertLineContains(t, stdout.String(), []string{"event=after_cleanup_error", "stdout only"})
	data, err := os.ReadFile(todayPath)
	if err != nil {
		t.Fatalf("ReadFile(today) error = %v", err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("today log changed after cleanup failure: %q", string(data))
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestLoggerEscapesUnsafeFieldValuesIntoOneLine(t *testing.T) {
	now := fixedTime()
	dir := t.TempDir()
	var stdout bytes.Buffer

	logger, err := New(&stdout, dir, 7, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("field_escape", "check", F("path", "/tmp/media file\nnext \"quote\" \\slash"), F("attempt", 2), F("err", fmt.Errorf("bad file\nsecond line")))

	got := stdout.String()
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("log output has %d newlines, want exactly 1: %q", strings.Count(got, "\n"), got)
	}
	assertLineContains(t, got, []string{
		"path=\"/tmp/media file\\nnext \\\"quote\\\" \\\\slash\"",
		"attempt=2",
		"err=\"bad file\\nsecond line\"",
	})
}

func TestLoggerSanitizesUnsafeFieldKeys(t *testing.T) {
	now := fixedTime()
	dir := t.TempDir()
	var stdout bytes.Buffer

	logger, err := New(&stdout, dir, 7, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("key_sanitize", "check", F("bad key\nwith=equals", "value"), F("路径", "media"), F("ok_key-1", "kept"))

	assertLineContains(t, stdout.String(), []string{
		"bad_key_with_equals=value",
		"__=media",
		"ok_key-1=kept",
	})
	if strings.Contains(stdout.String(), "bad key") || strings.Contains(stdout.String(), "with=equals") {
		t.Fatalf("log output contains unsanitized key: %q", stdout.String())
	}
}

func fixedTime() time.Time {
	location := time.FixedZone("CST", 8*60*60)
	return time.Date(2026, 4, 24, 12, 0, 0, 0, location)
}

func assertLineContains(t *testing.T, line string, wantParts []string) {
	t.Helper()
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("log line %q missing trailing newline", line)
	}
	for _, part := range wantParts {
		if !strings.Contains(line, part) {
			t.Fatalf("log line %q missing %q", line, part)
		}
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[(INFO|ERROR)\] .+ event=\S+`).MatchString(line) {
		t.Fatalf("log line %q does not have expected prefix format", line)
	}
}

func writeLogFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
}

func readLogFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", name, err)
	}
	return string(data)
}
