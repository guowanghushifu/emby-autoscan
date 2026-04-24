# FUSE Mount Emby Notify Design

## Goal

Build a small Go daemon for Debian 12 that periodically scans configured media directories mounted through `rclone mount`/FUSE3 and asks Emby to refresh only the affected libraries.

## Context

Regular filesystem event watchers such as `inotify` are unreliable for this use case because the watched paths are user-space FUSE mounts. The program therefore detects changes by taking periodic directory snapshots and comparing them with the previous persisted snapshot.

Example media layout:

- `/mnt/gd/sync/Movie` contains movies.
- `/mnt/gd/sync/TV` contains TV shows.

Each monitored directory maps to one Emby library ID, and multiple directories may map to the same library ID. During a scan cycle, if any file under any directory mapped to a library changes, the program sends at most one refresh request for that library.

## Architecture

The program is a single static Go binary with a YAML configuration file. It runs continuously, scans directories at a configured interval, persists scan state locally, and calls the Emby HTTP API when a library has changed.

Core units:

- **Config loader** reads and validates Emby connection settings, scan interval, state file path, and monitor entries.
- **Logger** writes timestamped logs to stdout and daily log files under a `logs` directory, retaining at most seven days of log files.
- **Scanner** walks configured directories and builds file snapshots from path, size, and modification time.
- **State store** loads and saves the latest snapshot so restarts do not treat all existing files as new.
- **Change detector** compares previous and current snapshots and returns the set of changed Emby library IDs.
- **Emby client** sends one refresh request per changed library ID after each scan cycle.
- **Daemon loop** coordinates scanning, notification, persistence, signal handling, and logging.

## Configuration

Configuration is stored in YAML. A representative config:

```yaml
emby:
  url: "http://127.0.0.1:8096"
  api_key: "0123456789abcdef0123456789abcdef"

scan:
  interval: "5m"
  state_file: "/var/lib/fuse-mount-emby-notify/state.json"
  notify_on_first_scan: false

logging:
  dir: "logs"
  retention_days: 7

monitors:
  - name: "movies"
    path: "/mnt/gd/sync/Movie"
    library_id: "movie-library-id"
  - name: "movies-extra"
    path: "/mnt/gd/sync/Movie2"
    library_id: "movie-library-id"
  - name: "tv"
    path: "/mnt/gd/sync/TV"
    library_id: "tv-library-id"
```

Rules:

- `scan.interval` uses Go duration syntax, such as `30s`, `5m`, or `1h`.
- `scan.notify_on_first_scan` defaults to `false` to avoid a full Emby scan on first startup.
- `logging.dir` defaults to `logs` relative to the current working directory when omitted.
- `logging.retention_days` defaults to `7`; log files older than the retention window are removed at startup and after daily rotation.
- Monitor `path` values must be absolute paths.
- Monitor `library_id` values must be non-empty.
- Multiple monitor entries may use the same `library_id`; notification is deduplicated per library ID after each scan cycle.

## Logging

The program logs every message to both stdout and a daily log file in the configured logs directory. Log files are named by date, for example `logs/2026-04-24.log`. Every log line includes a timestamp, level, event name, Chinese human-readable message, and key-value fields. Event names and field keys remain stable ASCII identifiers so logs are easy to filter with command-line tools, while the message text is written in Chinese for operators.

Required log events:

- Scan transaction start: Chinese message such as `开始执行目录检测`, plus scan cycle ID, start time, configured monitor count.
- Scan transaction finish: Chinese message such as `目录检测完成`, plus scan cycle ID, end time, elapsed seconds, scanned monitor count, failed monitor count, changed library count.
- File changes: Chinese message such as `检测到文件新增`, `检测到文件修改`, or `检测到文件删除`, plus scan cycle ID, monitor name, path, library ID, change type `added`, `modified`, or `deleted`, file size, and modification time when available.
- Emby notification start: Chinese message such as `开始通知 Emby 扫描媒体库`, plus scan cycle ID, library ID, request URL path.
- Emby notification finish: Chinese message such as `Emby 媒体库扫描通知完成` or `Emby 媒体库扫描通知失败`, plus scan cycle ID, library ID, elapsed seconds, HTTP status or network error.
- State persistence: Chinese message such as `扫描状态保存成功` or `扫描状态保存失败`, plus scan cycle ID, state file path, success or failure.

Log retention:

- Logs are written by calendar day using the local timezone.
- At most seven days of daily log files are kept by default.
- Retention cleanup only deletes files matching the program's daily log filename pattern inside `logging.dir`.
- stdout logging is always enabled even if file logging fails.

## Snapshot Semantics

The scanner records regular files only. Each file entry includes:

- Absolute path.
- File size.
- Modification time as Unix nanoseconds.

Directories, symlinks, sockets, devices, and other non-regular entries are ignored. If a directory cannot be read during a scan, the program logs the error and skips updating state for that monitor during that cycle. This prevents temporary FUSE/rclone errors from being persisted as mass deletions.

## Change Detection

A monitor is considered changed when any regular file under its path is added, deleted, or has a different size or modification time compared with the previous persisted snapshot. A library is considered changed when at least one monitor mapped to that library ID changed.

Notification behavior:

- One scan cycle can notify multiple libraries.
- Each library is notified at most once per scan cycle.
- If multiple changed monitor paths map to the same library ID, only one request is sent for that library.
- If no files changed for any monitor mapped to a library, no request is sent for that library.
- After a successful scan cycle, the current snapshot is persisted.

First-start behavior:

- If no prior state exists and `notify_on_first_scan` is `false`, the program saves the first snapshot and sends no Emby notifications.
- If no prior state exists and `notify_on_first_scan` is `true`, every monitor with files is treated as changed once.

## Emby Integration

The client calls Emby's library refresh endpoint for each changed library ID. The API key is sent using Emby's `X-Emby-Token` header.

Expected request shape:

```text
POST /emby/Items/{library_id}/Refresh?Recursive=true
X-Emby-Token: <api_key>
```

HTTP 2xx responses are treated as success. Non-2xx responses and network errors are logged. A failed notification does not prevent state persistence, because the next scan should only notify new changes; this avoids repeated notifications for the same stable filesystem state.

## Runtime Behavior

The binary accepts a config path:

```bash
fuse-mount-emby-notify -config /etc/fuse-mount-emby-notify/config.yaml
```

It logs to stdout/stderr for systemd compatibility. It handles `SIGINT` and `SIGTERM` by stopping after the current scan cycle, saving any completed scan state before exit.

The same timestamped application logs are also written to daily files under `logging.dir`.

## Deployment

The repository should include:

- `README.md` with build, configuration, and service setup instructions.
- `config.example.yaml` with movie and TV examples.
- `deploy/fuse-mount-emby-notify.service` as a systemd unit example.
- Go tests for configuration validation, snapshot comparison, state persistence, and Emby client request generation.
- Go tests for log file creation, stdout/file dual writing, daily filename selection, and seven-day retention cleanup.

The build target should support static Linux binaries:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify
```

## Error Handling

- Invalid configuration exits with a clear error.
- Log file creation failures are reported to stdout and do not prevent the daemon from running.
- Missing monitor directories are logged and skipped for that cycle.
- Partial scan errors prevent state updates for the affected monitor.
- State file write failures are logged and retried on the next cycle.
- Emby notification failures are logged with status code or network error.

## Testing Strategy

- Config tests verify valid YAML, invalid durations, empty Emby credentials, relative monitor paths, and duplicate monitor names.
- Scanner tests use temporary directories to verify add, delete, and modify detection.
- State tests verify JSON load/save and missing-state behavior.
- Emby client tests use `httptest.Server` to verify method, path, query, token header, and non-2xx handling.
- Logger tests verify timestamped stdout output, daily file output, retention cleanup, and behavior when the log directory cannot be created.
- Integration-style loop tests use a single scan step rather than sleeping daemon loops.

## Out of Scope

- Native `inotify`/`fsnotify` watchers.
- Provider-specific Google Drive or rclone remote change APIs.
- Web UI or desktop notifications.
- Automatic Emby library ID discovery.
- Retry queues for failed Emby notifications.
