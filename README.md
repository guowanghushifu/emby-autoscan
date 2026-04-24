# fuse-mount-emby-notify

`fuse-mount-emby-notify` is a small Go daemon for Debian servers that watches configured media directories and asks Emby to refresh only the libraries whose files changed. It is intended for media paths mounted with `rclone mount` or other FUSE filesystems where normal filesystem event watchers can miss, delay, or duplicate change notifications.

## Why Polling

FUSE and `rclone mount` layers do not always provide reliable inotify-style events to applications running above the mount. Network latency, VFS caching, remounts, and provider behavior can hide the real timing of changes. This program uses periodic polling instead: every scan builds a lightweight snapshot from file path, size, and modification time, compares it with the saved state, and refreshes the matching Emby library only when a configured path changed.

## Configuration

Copy `config.example.yaml` to `/etc/fuse-mount-emby-notify/config.yaml` and edit it for your server:

```yaml
emby:
  url: "http://127.0.0.1:8096"
  api_key: "replace-with-your-emby-api-key"

scan:
  interval: "5m"
  state_file: "/var/lib/fuse-mount-emby-notify/state.json"
  notify_on_first_scan: false

logging:
  dir: "logs"
  retention_days: 7

monitors:
  - name: "movie1"
    path: "/mnt/gd/sync/Movie1"
    library_id: "movie-library-id"
  - name: "movie2"
    path: "/mnt/gd/sync/Movie2"
    library_id: "movie-library-id"
  - name: "tv"
    path: "/mnt/gd/sync/TV"
    library_id: "tv-library-id"
```

Fields:

- `emby.url`: Base URL for your Emby server, for example `http://127.0.0.1:8096` or `https://emby.example.com`.
- `emby.api_key`: Emby API key used to call the library refresh endpoint.
- `scan.interval`: Polling interval using Go duration syntax, such as `30s`, `5m`, or `1h`.
- `scan.state_file`: JSON state path used to remember the previous snapshot across restarts.
- `scan.notify_on_first_scan`: When `false`, the first run records state without refreshing Emby; when `true`, existing files are treated as changed once.
- `logging.dir`: Directory for daily log files. A relative value such as `logs` is resolved from the process working directory.
- `logging.retention_days`: Number of daily log files to keep.
- `monitors[].name`: Unique local name for a monitored path.
- `monitors[].path`: Absolute media directory path to scan.
- `monitors[].library_id`: Emby library item ID to refresh when that path changes.

Multiple monitor paths may map to the same `library_id`. In the example, `/mnt/gd/sync/Movie1` and `/mnt/gd/sync/Movie2` both refresh `movie-library-id`; if both change in the same scan cycle, the program deduplicates notifications and refreshes that library once.

## Logs

The daemon writes each log line to both stdout and a daily log file in `logging.dir`. Log files rotate by date and files older than `logging.retention_days` are removed during startup and daily rotation.

Human-readable log messages are written in Chinese for operators, while event names and key fields remain stable ASCII identifiers for filtering with tools such as `grep`, `journalctl`, or log collectors. Under systemd, stdout is also available in the journal.

## Build

Build a static Linux amd64 binary from the repository root:

```sh
./build.sh
```

The script writes `dist/fuse-mount-emby-notify-linux-amd64` by default. You can override target and output values:

```sh
GOOS=linux GOARCH=arm64 OUTPUT=dist/fuse-mount-emby-notify-arm64 ./build.sh
```

Equivalent manual command:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/fuse-mount-emby-notify ./cmd/fuse-mount-emby-notify
```

## Debian 12 Install

These commands install the binary, configuration, and systemd unit on Debian 12:

```sh
sudo useradd --system --home-dir /var/lib/fuse-mount-emby-notify --shell /usr/sbin/nologin fuse-mount-emby-notify

sudo install -d -m 0755 /usr/local/bin
sudo install -m 0755 dist/fuse-mount-emby-notify-linux-amd64 /usr/local/bin/fuse-mount-emby-notify

sudo install -d -m 0755 /etc/fuse-mount-emby-notify
sudo install -o root -g fuse-mount-emby-notify -m 0640 config.example.yaml /etc/fuse-mount-emby-notify/config.yaml
sudo editor /etc/fuse-mount-emby-notify/config.yaml
sudo chown root:fuse-mount-emby-notify /etc/fuse-mount-emby-notify/config.yaml
sudo chmod 0640 /etc/fuse-mount-emby-notify/config.yaml

sudo install -m 0644 deploy/fuse-mount-emby-notify.service /etc/systemd/system/fuse-mount-emby-notify.service
sudo systemctl daemon-reload
sudo systemctl enable --now fuse-mount-emby-notify.service
```

The service runs as the dedicated `fuse-mount-emby-notify` user and group. The config file is owned by `root:fuse-mount-emby-notify` with mode `0640` so the service can read the Emby API key while other local users cannot.

The unit uses `StateDirectory=fuse-mount-emby-notify`, so systemd creates `/var/lib/fuse-mount-emby-notify` for writable service state. It runs with `WorkingDirectory=/var/lib/fuse-mount-emby-notify`; when `logging.dir: "logs"`, the app creates the `logs` directory inside that working directory. The service starts:

```sh
/usr/local/bin/fuse-mount-emby-notify -config /etc/fuse-mount-emby-notify/config.yaml
```

The generic unit only waits for `network-online.target`; it does not wait for your actual media mount units. If the service starts before the FUSE/rclone mounts are ready, scans may fail, or an empty first baseline may make later existing files look like new changes. Add `Requires=` and `After=` entries for your mount unit, or start this service only after your rclone mount process is ready.

The service user must also be able to traverse every configured `monitors[].path`. For user-mounted rclone/FUSE paths, either run this service as the same user that owns the mount, or configure FUSE/rclone access appropriately, such as `allow_other` with the required `/etc/fuse.conf` setting.

Useful operational commands:

```sh
sudo systemctl status fuse-mount-emby-notify.service
sudo journalctl -u fuse-mount-emby-notify.service -f
sudo systemctl restart fuse-mount-emby-notify.service
```
