package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	stdout        io.Writer
	file          *os.File
	clock         func() time.Time
	dir           string
	retentionDays int
	mu            sync.Mutex
}

type Field struct {
	Key   string
	Value any
}

func New(stdout io.Writer, dir string, retentionDays int, now func() time.Time) (*Logger, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if now == nil {
		now = time.Now
	}

	logger := &Logger{
		stdout:        stdout,
		clock:         now,
		dir:           dir,
		retentionDays: retentionDays,
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return logger, fmt.Errorf("create log dir: %w", err)
	}

	path := filepath.Join(dir, now().Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return logger, fmt.Errorf("open log file: %w", err)
	}
	logger.file = file

	if err := logger.removeExpiredDailyFiles(); err != nil {
		return logger, err
	}

	return logger, nil
}

func (l *Logger) Info(event, msg string, fields ...Field) {
	l.write("INFO", event, msg, fields...)
}

func (l *Logger) Error(event, msg string, fields ...Field) {
	l.write("ERROR", event, msg, fields...)
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func (l *Logger) write(level, event, msg string, fields ...Field) {
	line := l.formatLine(level, event, msg, fields...)

	l.mu.Lock()
	defer l.mu.Unlock()

	_, _ = io.WriteString(l.stdout, line)
	if l.file != nil {
		_, _ = io.WriteString(l.file, line)
	}
}

func (l *Logger) formatLine(level, event, msg string, fields ...Field) string {
	var builder strings.Builder
	builder.WriteString("time=")
	builder.WriteString(l.clock().Format(time.RFC3339))
	builder.WriteString(" level=")
	builder.WriteString(level)
	builder.WriteString(" event=")
	builder.WriteString(event)
	builder.WriteString(" msg=")
	builder.WriteString(fmt.Sprintf("%q", msg))

	for _, field := range fields {
		builder.WriteByte(' ')
		builder.WriteString(field.Key)
		builder.WriteByte('=')
		builder.WriteString(fmt.Sprint(field.Value))
	}
	builder.WriteByte('\n')

	return builder.String()
}

func (l *Logger) removeExpiredDailyFiles() error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}

	cutoff := localDate(l.clock()).AddDate(0, 0, -l.retentionDays)
	for _, entry := range entries {
		if entry.IsDir() || !dailyLogName(entry.Name()) {
			continue
		}

		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(entry.Name(), ".log"), l.clock().Location())
		if err != nil {
			continue
		}
		if !date.Before(cutoff) {
			continue
		}

		if err := os.Remove(filepath.Join(l.dir, entry.Name())); err != nil {
			return fmt.Errorf("remove expired log %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func localDate(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

var dailyLogNamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.log$`)

func dailyLogName(name string) bool {
	return dailyLogNamePattern.MatchString(name)
}
