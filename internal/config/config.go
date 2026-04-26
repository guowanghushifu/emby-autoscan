package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultScanInterval     = 5 * time.Minute
	defaultScanStateFile    = "state.json"
	defaultLoggingDir       = "logs"
	defaultLoggingRetention = 7
)

var defaultScanNotifyExtensions = []string{".mp4", ".mkv", ".ts", ".m2ts", ".srt", ".ass", ".sup", ".pgs"}

type Config struct {
	Emby     EmbyConfig      `yaml:"emby"`
	Scan     ScanConfig      `yaml:"scan"`
	Logging  LoggingConfig   `yaml:"logging"`
	Monitors []MonitorConfig `yaml:"monitors"`
}

type EmbyConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

type ScanConfig struct {
	Interval          time.Duration `yaml:"interval"`
	StateFile         string        `yaml:"state_file"`
	NotifyOnFirstScan bool          `yaml:"notify_on_first_scan"`
	NotifyExtensions  []string      `yaml:"notify_extensions"`
}

type LoggingConfig struct {
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
	Debug         bool   `yaml:"debug"`
}

type MonitorConfig struct {
	Name      string `yaml:"name"`
	Path      string `yaml:"path"`
	LibraryID string `yaml:"library_id"`
}

type rawConfig struct {
	Emby     EmbyConfig       `yaml:"emby"`
	Scan     rawScanConfig    `yaml:"scan"`
	Logging  rawLoggingConfig `yaml:"logging"`
	Monitors []MonitorConfig  `yaml:"monitors"`
}

type rawScanConfig struct {
	Interval          string   `yaml:"interval"`
	StateFile         string   `yaml:"state_file"`
	NotifyOnFirstScan bool     `yaml:"notify_on_first_scan"`
	NotifyExtensions  []string `yaml:"notify_extensions"`
}

type rawLoggingConfig struct {
	Dir           string `yaml:"dir"`
	RetentionDays *int   `yaml:"retention_days"`
	Debug         bool   `yaml:"debug"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var raw rawConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	cfg, err := normalize(raw)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func normalize(raw rawConfig) (Config, error) {
	cfg := Config{
		Emby: EmbyConfig{
			URL:    strings.TrimSpace(raw.Emby.URL),
			APIKey: strings.TrimSpace(raw.Emby.APIKey),
		},
		Scan: ScanConfig{
			Interval:          defaultScanInterval,
			StateFile:         defaultScanStateFile,
			NotifyOnFirstScan: raw.Scan.NotifyOnFirstScan,
			NotifyExtensions:  normalizeNotifyExtensions(raw.Scan.NotifyExtensions),
		},
		Logging: LoggingConfig{
			Dir:           defaultLoggingDir,
			RetentionDays: defaultLoggingRetention,
			Debug:         raw.Logging.Debug,
		},
		Monitors: make([]MonitorConfig, 0, len(raw.Monitors)),
	}

	if cfg.Emby.URL == "" {
		return Config{}, fmt.Errorf("emby.url is required")
	}
	if cfg.Emby.APIKey == "" {
		return Config{}, fmt.Errorf("emby.api_key is required")
	}

	if interval := strings.TrimSpace(raw.Scan.Interval); interval != "" {
		parsed, err := time.ParseDuration(interval)
		if err != nil {
			return Config{}, fmt.Errorf("scan.interval must be a valid duration: %w", err)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("scan.interval must be positive")
		}
		cfg.Scan.Interval = parsed
	}
	if stateFile := strings.TrimSpace(raw.Scan.StateFile); stateFile != "" {
		cfg.Scan.StateFile = stateFile
	}

	if loggingDir := strings.TrimSpace(raw.Logging.Dir); loggingDir != "" {
		cfg.Logging.Dir = loggingDir
	}
	if raw.Logging.RetentionDays != nil {
		if *raw.Logging.RetentionDays <= 0 {
			return Config{}, fmt.Errorf("logging.retention_days must be positive")
		}
		cfg.Logging.RetentionDays = *raw.Logging.RetentionDays
	}

	seenMonitorNames := make(map[string]struct{}, len(raw.Monitors))
	for index, monitor := range raw.Monitors {
		monitor.Name = strings.TrimSpace(monitor.Name)
		monitor.Path = strings.TrimSpace(monitor.Path)
		monitor.LibraryID = strings.TrimSpace(monitor.LibraryID)

		if monitor.Name == "" {
			return Config{}, fmt.Errorf("monitors[%d].name is required", index)
		}
		if _, ok := seenMonitorNames[monitor.Name]; ok {
			return Config{}, fmt.Errorf("duplicate monitor name %q", monitor.Name)
		}
		seenMonitorNames[monitor.Name] = struct{}{}

		if monitor.Path == "" {
			return Config{}, fmt.Errorf("monitors[%d].path is required", index)
		}
		if !filepath.IsAbs(monitor.Path) {
			return Config{}, fmt.Errorf("monitors[%d].path must be absolute", index)
		}
		if monitor.LibraryID == "" {
			return Config{}, fmt.Errorf("monitors[%d].library_id is required", index)
		}

		cfg.Monitors = append(cfg.Monitors, monitor)
	}

	return cfg, nil
}

func DefaultNotifyExtensions() []string {
	return append([]string(nil), defaultScanNotifyExtensions...)
}

func normalizeNotifyExtensions(values []string) []string {
	if values == nil {
		return DefaultNotifyExtensions()
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		extension := strings.ToLower(strings.TrimSpace(value))
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		if _, ok := seen[extension]; ok {
			continue
		}
		seen[extension] = struct{}{}
		normalized = append(normalized, extension)
	}
	return normalized
}
