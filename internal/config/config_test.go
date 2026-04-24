package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndAllowsDuplicateLibraryIDs(t *testing.T) {
	path := writeConfig(t, `
emby:
  url: " http://localhost:8096 "
  api_key: " secret "
scan: {}
monitors:
  - name: " movie one "
    path: " /mnt/gd/sync/Movie1 "
    library_id: " library-a "
  - name: "movie two"
    path: "/mnt/gd/sync/Movie2"
    library_id: "library-a"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Scan.Interval != 5*time.Minute {
		t.Fatalf("Scan.Interval = %v, want %v", cfg.Scan.Interval, 5*time.Minute)
	}
	if cfg.Logging.Dir != "logs" {
		t.Fatalf("Logging.Dir = %q, want %q", cfg.Logging.Dir, "logs")
	}
	if cfg.Logging.RetentionDays != 7 {
		t.Fatalf("Logging.RetentionDays = %d, want %d", cfg.Logging.RetentionDays, 7)
	}
	if len(cfg.Monitors) != 2 {
		t.Fatalf("len(Monitors) = %d, want 2", len(cfg.Monitors))
	}
	if cfg.Monitors[0].Path != "/mnt/gd/sync/Movie1" || cfg.Monitors[1].Path != "/mnt/gd/sync/Movie2" {
		t.Fatalf("monitor paths = %#v, want trimmed absolute paths", cfg.Monitors)
	}
	if cfg.Monitors[0].LibraryID != "library-a" || cfg.Monitors[1].LibraryID != "library-a" {
		t.Fatalf("library IDs = %#v, want duplicate library IDs preserved", cfg.Monitors)
	}
}

func TestLoadRejectsRelativeMonitorPath(t *testing.T) {
	path := writeConfig(t, validConfigWithMonitor(`
  - name: movies
    path: relative/path
    library_id: library-a
`))

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Load() error = %v, want absolute path error", err)
	}
}

func TestLoadRejectsDuplicateMonitorName(t *testing.T) {
	path := writeConfig(t, validConfigWithMonitor(`
  - name: movies
    path: /mnt/gd/sync/Movie1
    library_id: library-a
  - name: " movies "
    path: /mnt/gd/sync/Movie2
    library_id: library-b
`))

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate monitor name") {
		t.Fatalf("Load() error = %v, want duplicate monitor name error", err)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	path := writeConfig(t, `
emby:
  url: http://localhost:8096
  api_key: secret
scan:
  interval: sometimes
monitors:
  - name: movies
    path: /mnt/gd/sync/Movie1
    library_id: library-a
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "scan.interval") {
		t.Fatalf("Load() error = %v, want scan.interval duration error", err)
	}
}

func TestLoadRejectsMissingEmbySettings(t *testing.T) {
	path := writeConfig(t, `
emby:
  url: ""
  api_key: ""
scan: {}
monitors:
  - name: movies
    path: /mnt/gd/sync/Movie1
    library_id: library-a
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "emby") {
		t.Fatalf("Load() error = %v, want missing emby settings error", err)
	}
}

func validConfigWithMonitor(monitors string) string {
	return `
emby:
  url: http://localhost:8096
  api_key: secret
scan: {}
monitors:
` + monitors
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}
