package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
		"time=2026-04-24T12:00:00+08:00",
		"level=INFO",
		"event=scan_start",
		"msg=\"开始执行目录检测\"",
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
		"time=2026-04-24T12:00:00+08:00",
		"level=ERROR",
		"event=file_setup_failed",
		"msg=\"开始执行目录检测\"",
		"attempt=1",
	})
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
	if !regexp.MustCompile(`^time=\S+ level=\S+ event=\S+ msg=".*"`).MatchString(line) {
		t.Fatalf("log line %q does not have expected prefix format", line)
	}
}

func writeLogFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
}
