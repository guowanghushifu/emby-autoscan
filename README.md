# emby-autoscan

`emby-autoscan` is a small Go daemon for Debian servers that watches configured media directories and asks Emby to refresh only the libraries whose files changed. It is intended for media paths mounted with `rclone mount` or other FUSE filesystems where normal filesystem event watchers can miss, delay, or duplicate change notifications.

## Why Polling

FUSE and `rclone mount` layers do not always provide reliable inotify-style events to applications running above the mount. Network latency, VFS caching, remounts, and provider behavior can hide the real timing of changes. This program uses periodic polling instead: every scan builds a lightweight snapshot from file path, size, and modification time, compares it with the saved state, and refreshes the matching Emby library only when a configured path changed.

## Configuration

Copy `config.example.yaml` to `/etc/emby-autoscan/config.yaml` and edit it for your server:

```yaml
emby:
  url: "http://127.0.0.1:8096"
  api_key: "replace-with-your-emby-api-key"

scan:
  interval: "5m"
  state_file: "/var/lib/emby-autoscan/state.json"
  notify_on_first_scan: false
  notify_extensions:
    - ".mp4"
    - ".mkv"
    - ".ts"
    - ".m2ts"
    - ".srt"
    - ".ass"
    - ".sup"
    - ".pgs"

logging:
  dir: "logs"
  retention_days: 7
  debug: false

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
- `scan.notify_extensions`: File suffixes that should log file changes and refresh Emby. Matching is case-insensitive, and values may be written with or without the leading dot. Other suffixes still update the saved state, but do not produce file-change logs or Emby refresh requests.
- `logging.dir`: Directory for daily log files. A relative value such as `logs` is resolved from the process working directory.
- `logging.retention_days`: Number of daily log files to keep.
- `logging.debug`: When `true`, unchanged scan cycles are logged. The default `false` suppresses routine "0 changes" cycle summaries.
- `monitors[].name`: Unique local name for a monitored path.
- `monitors[].path`: Absolute media directory path to scan.
- `monitors[].library_id`: Emby library item ID to refresh when that path changes.

Multiple monitor paths may map to the same `library_id`. In the example, `/mnt/gd/sync/Movie1` and `/mnt/gd/sync/Movie2` both refresh `movie-library-id`; if both change in the same scan cycle, the program deduplicates notifications and refreshes that library once.

## Logs

The daemon writes each log line to both stdout and a daily log file in `logging.dir`. Log files rotate by date and files older than `logging.retention_days` are removed during startup and daily rotation.

Human-readable log messages are written in Chinese for operators, while event names and key fields remain stable ASCII identifiers for filtering with tools such as `grep`, `journalctl`, or log collectors. Under systemd, stdout is also available in the journal.

## Build

Build static Linux amd64 and arm64 binaries from the repository root:

```sh
./build.sh
```

The script writes `dist/emby-autoscan-linux-amd64` and `dist/emby-autoscan-linux-arm64` by default. You can override target and output values for a single build:

```sh
GOOS=linux GOARCH=arm64 OUTPUT=dist/emby-autoscan-arm64 ./build.sh
```

You can also override the default architecture list:

```sh
TARGET_ARCHES="amd64 arm64" ./build.sh
```

Equivalent manual command:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/emby-autoscan ./cmd/emby-autoscan
```

## Debian 12 Install

These commands install the binary, configuration, and systemd unit on Debian 12:

```sh
sudo useradd --system --home-dir /var/lib/emby-autoscan --shell /usr/sbin/nologin emby-autoscan

sudo install -d -m 0755 /usr/local/bin
sudo install -m 0755 dist/emby-autoscan-linux-amd64 /usr/local/bin/emby-autoscan

sudo install -d -m 0755 /etc/emby-autoscan
sudo install -o root -g emby-autoscan -m 0640 config.example.yaml /etc/emby-autoscan/config.yaml
sudo editor /etc/emby-autoscan/config.yaml
sudo chown root:emby-autoscan /etc/emby-autoscan/config.yaml
sudo chmod 0640 /etc/emby-autoscan/config.yaml

sudo install -m 0644 deploy/emby-autoscan.service /etc/systemd/system/emby-autoscan.service
sudo systemctl daemon-reload
sudo systemctl enable --now emby-autoscan.service
```

The service runs as the dedicated `emby-autoscan` user and group. The config file is owned by `root:emby-autoscan` with mode `0640` so the service can read the Emby API key while other local users cannot.

The unit uses `StateDirectory=emby-autoscan`, so systemd creates `/var/lib/emby-autoscan` for writable service state. It runs with `WorkingDirectory=/var/lib/emby-autoscan`; when `logging.dir: "logs"`, the app creates the `logs` directory inside that working directory. The service starts:

```sh
/usr/local/bin/emby-autoscan -config /etc/emby-autoscan/config.yaml
```

The generic unit only waits for `network-online.target`; it does not wait for your actual media mount units. If the service starts before the FUSE/rclone mounts are ready, scans may fail, or an empty first baseline may make later existing files look like new changes. Add `Requires=` and `After=` entries for your mount unit, or start this service only after your rclone mount process is ready.

At runtime, each scan cycle also checks whether a `/usr/bin/rclone mount` process is running. If it is missing, the daemon logs the condition and skips that cycle without reading state, scanning directories, saving state, or notifying Emby; the next cycle checks again.

The service user must also be able to traverse every configured `monitors[].path`. For user-mounted rclone/FUSE paths, either run this service as the same user that owns the mount, or configure FUSE/rclone access appropriately, such as `allow_other` with the required `/etc/fuse.conf` setting.

Useful operational commands:

```sh
sudo systemctl status emby-autoscan.service
sudo journalctl -u emby-autoscan.service -f
sudo systemctl restart emby-autoscan.service
```
