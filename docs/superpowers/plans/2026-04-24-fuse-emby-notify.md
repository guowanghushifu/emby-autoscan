# FUSE Mount Emby Notify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a static Go daemon that periodically scans FUSE-mounted media directories and notifies Emby once per changed library per scan cycle.

**Architecture:** The binary loads YAML config, initializes a dual stdout/file logger, loads persisted snapshots, runs one scan cycle immediately, then repeats on a ticker until shutdown. Scan state is keyed by monitor name so multiple paths can map to the same Emby library ID while notifications are deduplicated by library ID.

**Tech Stack:** Go 1.22+, standard library, `gopkg.in/yaml.v3` for YAML parsing, JSON state files, `net/http` for Emby API calls, `log/slog`-style structured log line conventions implemented with a small custom logger.

---

## File Structure

- Create `go.mod`: module definition and YAML dependency.
- Create `cmd/fuse-mount-emby-notify/main.go`: CLI flags, startup wiring, signal handling, daemon loop.
- Create `internal/config/config.go`: YAML structures, defaults, validation.
- Create `internal/config/config_test.go`: config loading and validation tests.
- Create `internal/logging/logger.go`: Chinese structured logger, stdout/file dual writer, daily rotation, seven-day retention.
- Create `internal/logging/logger_test.go`: log output, file output, rotation, retention tests.
- Create `internal/snapshot/snapshot.go`: file snapshot types, directory scanner, snapshot comparison.
- Create `internal/snapshot/snapshot_test.go`: add/delete/modify and scan-error tests.
- Create `internal/state/store.go`: JSON state load/save with atomic replace.
- Create `internal/state/store_test.go`: missing state, round-trip, save tests.
- Create `internal/emby/client.go`: Emby library refresh HTTP client.
- Create `internal/emby/client_test.go`: request path/query/header/status tests.
- Create `internal/app/app.go`: one-cycle orchestration and daemon ticker loop.
- Create `internal/app/app_test.go`: first-scan behavior, per-library dedupe, failed-monitor behavior.
- Create `config.example.yaml`: sample config with two movie paths sharing one library.
- Create `README.md`: build, config, Debian 12/systemd usage.
- Create `deploy/fuse-mount-emby-notify.service`: example systemd unit.

## Shared Data Contracts

Use these names consistently across packages:

```go
type Config struct {
    Emby     EmbyConfig      `yaml:"emby"`
    Scan     ScanConfig      `yaml:"scan"`
    Logging  LoggingConfig   `yaml:"logging"`
    Monitors []MonitorConfig `yaml:"monitors"`
}

type MonitorConfig struct {
    Name      string `yaml:"name"`
    Path      string `yaml:"path"`
    LibraryID string `yaml:"library_id"`
}

type FileInfo struct {
    Path    string `json:"path"`
    Size    int64  `json:"size"`
    ModTime int64  `json:"mod_time"`
}

type MonitorSnapshot struct {
    MonitorName string              `json:"monitor_name"`
    Path        string              `json:"path"`
    LibraryID   string              `json:"library_id"`
    Files       map[string]FileInfo `json:"files"`
}

type State struct {
    Version  int                        `json:"version"`
    Monitors map[string]MonitorSnapshot `json:"monitors"`
}

type Change struct {
    MonitorName string
    LibraryID   string
    Path        string
    Type        string
    Size        int64
    ModTime     int64
}
```

### Task 1: Go Module And Config Loader

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write failing config tests**

Create `internal/config/config_test.go` with tests named:

```go
func TestLoadAppliesDefaultsAndAllowsDuplicateLibraryIDs(t *testing.T)
func TestLoadRejectsRelativeMonitorPath(t *testing.T)
func TestLoadRejectsDuplicateMonitorName(t *testing.T)
func TestLoadRejectsInvalidDuration(t *testing.T)
func TestLoadRejectsMissingEmbySettings(t *testing.T)
```

The first test writes YAML containing `/mnt/gd/sync/Movie1` and `/mnt/gd/sync/Movie2` with the same `library_id`, omits `logging`, calls `Load(path)`, and asserts `Scan.Interval == 5*time.Minute`, `Logging.Dir == "logs"`, `Logging.RetentionDays == 7`, and both monitors are preserved.

- [ ] **Step 2: Run config tests to verify failure**

Run: `go test ./internal/config -run TestLoad -v`

Expected: FAIL because `go.mod` or `internal/config` does not exist.

- [ ] **Step 3: Implement config loader**

Create `go.mod`:

```go
module github.com/wangdazhuo/fuse-mount-emby-notify

go 1.22

require gopkg.in/yaml.v3 v3.0.1
```

Create `internal/config/config.go` with exported `Load(path string) (Config, error)`. It must read YAML, parse `scan.interval` with `time.ParseDuration`, default interval to `5m` when empty, default state file to `state.json` when empty, default `notify_on_first_scan` to false, default `logging.dir` to `logs`, default retention to `7`, require absolute monitor paths, reject duplicate monitor names, allow duplicate library IDs, trim whitespace for string fields, and return clear errors.

- [ ] **Step 4: Run config tests to verify pass**

Run: `go test ./internal/config -run TestLoad -v`

Expected: PASS.

- [ ] **Step 5: Commit config loader**

Run:

```bash
git add go.mod go.sum internal/config/config.go internal/config/config_test.go
git commit -m "feat: add config loader"
```

### Task 2: Snapshot Scanner And Change Detection

**Files:**
- Create: `internal/snapshot/snapshot.go`
- Create: `internal/snapshot/snapshot_test.go`

- [ ] **Step 1: Write failing snapshot tests**

Create tests named:

```go
func TestScanRecordsRegularFilesOnly(t *testing.T)
func TestDiffDetectsAddedModifiedDeletedFiles(t *testing.T)
func TestChangedLibrariesAreDeduplicated(t *testing.T)
func TestScanMissingDirectoryReturnsErrorWithoutSnapshot(t *testing.T)
```

`TestChangedLibrariesAreDeduplicated` must create changes for two monitor names with the same `LibraryID` and assert `ChangedLibraryIDs(changes)` returns one ID.

- [ ] **Step 2: Run snapshot tests to verify failure**

Run: `go test ./internal/snapshot -run 'TestScan|TestDiff|TestChanged' -v`

Expected: FAIL because package is not implemented.

- [ ] **Step 3: Implement scanner and diff**

Create exported functions:

```go
func ScanMonitor(name, root, libraryID string) (MonitorSnapshot, error)
func DiffMonitor(previous, current MonitorSnapshot) []Change
func ChangedLibraryIDs(changes []Change) []string
```

`ScanMonitor` must use `filepath.WalkDir`, ignore non-regular entries, call `Info()` for regular files, store absolute paths, size, and UnixNano mod time. `DiffMonitor` returns `added`, `modified`, and `deleted` changes. `ChangedLibraryIDs` must sort and deduplicate library IDs for deterministic tests.

- [ ] **Step 4: Run snapshot tests to verify pass**

Run: `go test ./internal/snapshot -run 'TestScan|TestDiff|TestChanged' -v`

Expected: PASS.

- [ ] **Step 5: Commit snapshot package**

Run:

```bash
git add internal/snapshot/snapshot.go internal/snapshot/snapshot_test.go
git commit -m "feat: add snapshot scanner"
```

### Task 3: JSON State Store

**Files:**
- Create: `internal/state/store.go`
- Create: `internal/state/store_test.go`

- [ ] **Step 1: Write failing state tests**

Create tests named:

```go
func TestLoadMissingStateReturnsEmptyStateAndFalse(t *testing.T)
func TestSaveAndLoadRoundTrip(t *testing.T)
func TestSaveCreatesParentDirectory(t *testing.T)
```

The round-trip test must save two monitor snapshots with the same `LibraryID` and verify both monitor names remain separate after load.

- [ ] **Step 2: Run state tests to verify failure**

Run: `go test ./internal/state -run Test -v`

Expected: FAIL because package is not implemented.

- [ ] **Step 3: Implement state store**

Create:

```go
type Store struct { Path string }
func (s Store) Load() (snapshot.State, bool, error)
func (s Store) Save(state snapshot.State) error
```

`Load` returns `exists=false` for `os.IsNotExist`. `Save` creates the parent directory, writes indented JSON to `Path + ".tmp"`, closes the file, then renames it to the final path.

- [ ] **Step 4: Run state tests to verify pass**

Run: `go test ./internal/state -run Test -v`

Expected: PASS.

- [ ] **Step 5: Commit state store**

Run:

```bash
git add internal/state/store.go internal/state/store_test.go
git commit -m "feat: add state store"
```

### Task 4: Chinese Dual Logger With Retention

**Files:**
- Create: `internal/logging/logger.go`
- Create: `internal/logging/logger_test.go`

- [ ] **Step 1: Write failing logger tests**

Create tests named:

```go
func TestLoggerWritesChineseMessageToStdoutAndDailyFile(t *testing.T)
func TestLoggerRemovesDailyFilesOlderThanRetention(t *testing.T)
func TestLoggerKeepsNonMatchingFiles(t *testing.T)
func TestLoggerFallsBackToStdoutWhenFileSetupFails(t *testing.T)
```

The first test must log message `开始执行目录检测` with event `scan_start`, then assert stdout and today's log file both contain the timestamp prefix, `level=INFO`, `event=scan_start`, and `msg="开始执行目录检测"`.

- [ ] **Step 2: Run logger tests to verify failure**

Run: `go test ./internal/logging -run TestLogger -v`

Expected: FAIL because logger is not implemented.

- [ ] **Step 3: Implement logger**

Create:

```go
type Logger struct { /* stdout writer, file writer, clock, dir, retention */ }
type Field struct { Key string; Value any }
func New(stdout io.Writer, dir string, retentionDays int, now func() time.Time) (*Logger, error)
func (l *Logger) Info(event, msg string, fields ...Field)
func (l *Logger) Error(event, msg string, fields ...Field)
func (l *Logger) Close() error
func F(key string, value any) Field
```

Every line must be UTF-8 text in this shape: `time=2026-04-24T12:00:00+08:00 level=INFO event=scan_start msg="开始执行目录检测" key=value`. `New` must create `YYYY-MM-DD.log`, write to stdout even if file setup fails, and remove only files matching `^\d{4}-\d{2}-\d{2}\.log$` older than `retentionDays` days.

- [ ] **Step 4: Run logger tests to verify pass**

Run: `go test ./internal/logging -run TestLogger -v`

Expected: PASS.

- [ ] **Step 5: Commit logger**

Run:

```bash
git add internal/logging/logger.go internal/logging/logger_test.go
git commit -m "feat: add chinese dual logger"
```

### Task 5: Emby HTTP Client

**Files:**
- Create: `internal/emby/client.go`
- Create: `internal/emby/client_test.go`

- [ ] **Step 1: Write failing Emby client tests**

Create tests named:

```go
func TestRefreshLibrarySendsExpectedRequest(t *testing.T)
func TestRefreshLibraryReturnsErrorForNon2xx(t *testing.T)
```

The first test must use `httptest.Server` and assert method `POST`, path `/emby/Items/movie-library-id/Refresh`, query `Recursive=true`, and header `X-Emby-Token: test-token`.

- [ ] **Step 2: Run Emby tests to verify failure**

Run: `go test ./internal/emby -run TestRefreshLibrary -v`

Expected: FAIL because client is not implemented.

- [ ] **Step 3: Implement Emby client**

Create:

```go
type Client struct { BaseURL string; APIKey string; HTTPClient *http.Client }
func (c Client) RefreshLibrary(ctx context.Context, libraryID string) error
func RefreshPath(libraryID string) string
```

`RefreshLibrary` must trim trailing slash from `BaseURL`, URL-escape the library ID path segment, POST to `/emby/Items/{libraryID}/Refresh?Recursive=true`, set `X-Emby-Token`, use the provided `HTTPClient` or `http.DefaultClient`, and return an error for non-2xx status.

- [ ] **Step 4: Run Emby tests to verify pass**

Run: `go test ./internal/emby -run TestRefreshLibrary -v`

Expected: PASS.

- [ ] **Step 5: Commit Emby client**

Run:

```bash
git add internal/emby/client.go internal/emby/client_test.go
git commit -m "feat: add emby refresh client"
```

### Task 6: One Scan Cycle Orchestrator

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`

- [ ] **Step 1: Write failing app tests**

Create tests named:

```go
func TestRunOnceFirstScanSavesBaselineWithoutNotify(t *testing.T)
func TestRunOnceNotifyOnFirstScanNotifiesChangedLibraries(t *testing.T)
func TestRunOnceDeduplicatesSameLibraryAcrossMonitors(t *testing.T)
func TestRunOnceSkipsStateUpdateForFailedMonitor(t *testing.T)
func TestRunOnceLogsChineseScanChangeAndNotifyMessages(t *testing.T)
```

Use fake scanner, fake store, fake notifier, and `bytes.Buffer` logger output. The dedupe test must configure `Movie1` and `Movie2` with the same library ID and assert the notifier receives that library ID once.

- [ ] **Step 2: Run app tests to verify failure**

Run: `go test ./internal/app -run TestRunOnce -v`

Expected: FAIL because app package is not implemented.

- [ ] **Step 3: Implement app orchestration**

Create interfaces:

```go
type Scanner interface { Scan(config.MonitorConfig) (snapshot.MonitorSnapshot, error) }
type Store interface { Load() (snapshot.State, bool, error); Save(snapshot.State) error }
type Notifier interface { RefreshLibrary(context.Context, string) error }
type App struct { Config config.Config; Scanner Scanner; Store Store; Notifier Notifier; Logger *logging.Logger }
func (a *App) RunOnce(ctx context.Context, cycleID string) error
func (a *App) Run(ctx context.Context) error
```

`RunOnce` must log `开始执行目录检测`, scan all monitors, compare only successful monitors, preserve old state for failed monitors, log each file change in Chinese, deduplicate changed library IDs, notify once per changed library, log notify start/finish in Chinese, save state after scans, and log `目录检测完成` with elapsed seconds.

- [ ] **Step 4: Run app tests to verify pass**

Run: `go test ./internal/app -run TestRunOnce -v`

Expected: PASS.

- [ ] **Step 5: Commit app orchestrator**

Run:

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: add scan cycle orchestration"
```

### Task 7: CLI Wiring And Daemon Runtime

**Files:**
- Create: `cmd/fuse-mount-emby-notify/main.go`
- Modify: `internal/snapshot/snapshot.go`

- [ ] **Step 1: Write failing CLI build check**

Run: `go test ./cmd/fuse-mount-emby-notify -run TestDoesNotExist -v`

Expected: FAIL because the command package does not exist.

- [ ] **Step 2: Implement command package**

Create `main.go` that parses `-config`, requires a non-empty config path, loads config, creates logger with `os.Stdout`, creates state store, wraps `snapshot.ScanMonitor` in an app scanner, creates Emby client, runs `App.Run(ctx)`, and cancels context on `SIGINT`/`SIGTERM`.

Add a concrete scanner adapter:

```go
type FileScanner struct{}
func (FileScanner) Scan(m config.MonitorConfig) (snapshot.MonitorSnapshot, error) {
    return snapshot.ScanMonitor(m.Name, m.Path, m.LibraryID)
}
```

- [ ] **Step 3: Run full package tests and build**

Run: `go test ./...`

Expected: PASS.

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify`

Expected: PASS and creates `dist/fuse-mount-emby-notify`.

- [ ] **Step 4: Commit CLI runtime**

Run:

```bash
git add cmd/fuse-mount-emby-notify/main.go internal/snapshot/snapshot.go
git commit -m "feat: wire daemon runtime"
```

### Task 8: Examples And Debian Deployment Docs

**Files:**
- Create: `config.example.yaml`
- Create: `README.md`
- Create: `deploy/fuse-mount-emby-notify.service`

- [ ] **Step 1: Create example config**

Create `config.example.yaml` with Emby URL/API key, `scan.interval: "5m"`, `scan.state_file: "/var/lib/fuse-mount-emby-notify/state.json"`, `logging.dir: "logs"`, `logging.retention_days: 7`, and monitors for `/mnt/gd/sync/Movie1`, `/mnt/gd/sync/Movie2`, and `/mnt/gd/sync/TV` where the two movie paths share one `library_id`.

- [ ] **Step 2: Create systemd unit**

Create `deploy/fuse-mount-emby-notify.service` with `ExecStart=/usr/local/bin/fuse-mount-emby-notify -config /etc/fuse-mount-emby-notify/config.yaml`, `Restart=on-failure`, `WorkingDirectory=/var/lib/fuse-mount-emby-notify`, and `StateDirectory=fuse-mount-emby-notify`.

- [ ] **Step 3: Create README**

Document what the program does, why it uses polling for FUSE/rclone mounts, config fields, Chinese logs, build command, install commands for Debian 12, systemd setup, and how multiple paths can map to one library ID.

- [ ] **Step 4: Validate docs and config**

Run: `go test ./...`

Expected: PASS.

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify`

Expected: PASS.

- [ ] **Step 5: Commit docs and examples**

Run:

```bash
git add README.md config.example.yaml deploy/fuse-mount-emby-notify.service
git commit -m "docs: add deployment guide"
```

### Task 9: Final Verification

**Files:**
- Modify only if verification reveals a defect in files created by earlier tasks.

- [ ] **Step 1: Run full tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Run static Linux build**

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify`

Expected: PASS.

- [ ] **Step 3: Inspect git status**

Run: `git status --short`

Expected: no uncommitted source changes except optional ignored build artifacts.

- [ ] **Step 4: Record verification result**

If all commands pass, report the exact commands and outcomes. If a command fails, fix the root cause in the smallest related package, rerun the failing command, then rerun `go test ./...`.

## Self-Review

- Spec coverage: configuration, polling snapshots, duplicate library ID dedupe, first-scan behavior, Emby refresh, Chinese dual logs, daily retention, systemd deployment, and static build are covered by tasks.
- Placeholder scan: this plan avoids deferred items and gives exact file paths, commands, expected outcomes, and function/type names.
- Type consistency: config, snapshot, state, logger, Emby, and app contracts use the same names across tasks.
