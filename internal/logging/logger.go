package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	stdout        io.Writer
	file          *os.File
	fileDate      string
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

	current := now()
	fileDate := current.Format("2006-01-02")
	file, err := openDailyFile(dir, fileDate)
	if err != nil {
		return logger, fmt.Errorf("open log file: %w", err)
	}
	logger.file = file
	logger.fileDate = fileDate

	if err := logger.removeExpiredDailyFiles(current); err != nil {
		_ = file.Close()
		logger.file = nil
		logger.fileDate = ""
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
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	if l.file != nil {
		l.rotateDailyFile(now)
	}
	line := formatLine(now, level, event, msg, fields...)

	_, _ = io.WriteString(l.stdout, line)
	if l.file != nil {
		_, _ = io.WriteString(l.file, line)
	}
}

func (l *Logger) rotateDailyFile(now time.Time) {
	fileDate := now.Format("2006-01-02")
	if l.fileDate == fileDate {
		return
	}

	_ = l.file.Close()
	l.file = nil
	l.fileDate = ""

	file, err := openDailyFile(l.dir, fileDate)
	if err != nil {
		return
	}
	l.file = file
	l.fileDate = fileDate
	if err := l.removeExpiredDailyFiles(now); err != nil {
		_ = file.Close()
		l.file = nil
		l.fileDate = ""
	}
}

func formatLine(now time.Time, level, event, msg string, fields ...Field) string {
	var builder strings.Builder
	builder.WriteString(now.Format("2006-01-02 15:04:05"))
	builder.WriteString(" [")
	builder.WriteString(level)
	builder.WriteString("] ")
	builder.WriteString(msg)
	builder.WriteString(" event=")
	builder.WriteString(event)

	for _, field := range fields {
		builder.WriteByte(' ')
		builder.WriteString(sanitizeKey(field.Key))
		builder.WriteByte('=')
		builder.WriteString(formatValue(field.Value))
	}
	builder.WriteByte('\n')

	return builder.String()
}

func (l *Logger) removeExpiredDailyFiles(now time.Time) error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return fmt.Errorf("read log dir: %w", err)
	}

	cutoff := localDate(now).AddDate(0, 0, -l.retentionDays)
	for _, entry := range entries {
		if entry.IsDir() || !dailyLogName(entry.Name()) {
			continue
		}

		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(entry.Name(), ".log"), now.Location())
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

func openDailyFile(dir, fileDate string) (*os.File, error) {
	path := filepath.Join(dir, fileDate+".log")
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func sanitizeKey(key string) string {
	if key == "" {
		return "_"
	}

	var builder strings.Builder
	for _, char := range key {
		if isSafeKeyChar(char) {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('_')
	}
	return builder.String()
}

func isSafeKeyChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '_' || char == '-'
}

func formatValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int8, int16, int32, int64:
		return fmt.Sprint(typed)
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return fmt.Sprint(typed)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case string:
		return formatStringValue(typed)
	case error:
		return formatStringValue(typed.Error())
	case fmt.Stringer:
		return formatStringValue(typed.String())
	default:
		return formatStringValue(fmt.Sprint(typed))
	}
}

func formatStringValue(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\r\n\"\\") {
		return strconv.Quote(value)
	}
	return value
}

func localDate(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

var dailyLogNamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.log$`)

func dailyLogName(name string) bool {
	return dailyLogNamePattern.MatchString(name)
}
