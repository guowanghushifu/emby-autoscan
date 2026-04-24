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
- Monitor `path` values must be absolute paths.
- Monitor `library_id` values must be non-empty.
- Multiple monitor entries may use the same `library_id`; notification is deduplicated per library ID after each scan cycle.

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

## Deployment

The repository should include:

- `README.md` with build, configuration, and service setup instructions.
- `config.example.yaml` with movie and TV examples.
- `deploy/fuse-mount-emby-notify.service` as a systemd unit example.
- Go tests for configuration validation, snapshot comparison, state persistence, and Emby client request generation.

The build target should support static Linux binaries:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify
```

## Error Handling

- Invalid configuration exits with a clear error.
- Missing monitor directories are logged and skipped for that cycle.
- Partial scan errors prevent state updates for the affected monitor.
- State file write failures are logged and retried on the next cycle.
- Emby notification failures are logged with status code or network error.

## Testing Strategy

- Config tests verify valid YAML, invalid durations, empty Emby credentials, relative monitor paths, and duplicate monitor names.
- Scanner tests use temporary directories to verify add, delete, and modify detection.
- State tests verify JSON load/save and missing-state behavior.
- Emby client tests use `httptest.Server` to verify method, path, query, token header, and non-2xx handling.
- Integration-style loop tests use a single scan step rather than sleeping daemon loops.

## Out of Scope

- Native `inotify`/`fsnotify` watchers.
- Provider-specific Google Drive or rclone remote change APIs.
- Web UI or desktop notifications.
- Automatic Emby library ID discovery.
- Retry queues for failed Emby notifications.
