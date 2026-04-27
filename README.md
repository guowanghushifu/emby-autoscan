# emby-autoscan

`emby-autoscan` 是一个面向 Emby 的目录轮询工具。它会定期扫描配置好的媒体目录，对比上一次保存的快照，只在关心的文件类型发生变化时通知 Emby 刷新对应媒体库。

它适合用于 `rclone mount`、FUSE、网盘挂载等场景。这些场景下文件系统事件不一定可靠，所以本项目使用轮询而不是 inotify。

## 需要准备

- 一台能访问 Emby 的 Linux 主机。
- 已安装并正常运行的 `rclone mount`，程序会检查 `/usr/bin/rclone ... mount ...` 进程。
- Emby API Key。
- Go 1.22 或更新版本，仅源码构建时需要。
- 确认运行用户可以访问所有 `monitors[].path` 目录。

如果使用 rclone/FUSE 挂载，建议先确认挂载目录稳定可读，再启动本程序。程序运行中如果发现某个监控目录无法访问，或目录为空，会记录错误并跳过该目录，不会通知 Emby，也不会把该目录误保存为空状态。

## 构建

源码构建：

```sh
./build.sh
```

默认生成：

```text
dist/emby-autoscan-linux-amd64
dist/emby-autoscan-linux-arm64
```

只构建指定架构：

```sh
GOOS=linux GOARCH=arm64 OUTPUT=dist/emby-autoscan-linux-arm64 ./build.sh
```

GitHub Actions 发布构建使用：

```sh
VERSION=v1.0.0 ./ci_build.sh
```

它会生成：

```text
dist/emby-autoscan-linux-amd64.tar.gz
dist/emby-autoscan-linux-arm64.tar.gz
dist/SHA256SUMS
```

发布包中包含：

```text
emby-autoscan-linux-amd64 或 emby-autoscan-linux-arm64
config.example.yaml
run-forever.sh
```

## 配置文件

复制示例配置：

```sh
cp config.example.yaml config.yaml
```

示例：

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
  - name: "tv"
    path: "/mnt/gd/sync/TV"
    library_id: "tv-library-id"
```

主要配置：

- `emby.url`：Emby 地址。
- `emby.api_key`：Emby API Key。
- `scan.interval`：扫描间隔，例如 `30s`、`5m`、`1h`。
- `scan.state_file`：快照状态文件路径。
- `scan.notify_on_first_scan`：首次扫描是否把已有文件当作新增并通知 Emby，默认建议 `false`。
- `scan.notify_extensions`：只有这些后缀的新增、修改、删除会记录文件变化日志并通知 Emby。大小写不敏感，可以带点或不带点。
- `logging.dir`：日志目录。
- `logging.retention_days`：日志保留天数。
- `logging.debug`：是否输出无变化周期日志。默认 `false`，不会输出“0 个变化”的周期记录。
- `monitors[].name`：监控项名称，必须唯一。
- `monitors[].path`：要扫描的绝对路径。
- `monitors[].library_id`：Emby 媒体库 ID。

## 监控目录注意事项

- `monitors[].path` 必须是绝对路径。
- 多个监控目录可以配置相同的 `library_id`。同一轮扫描里会自动去重，只通知一次 Emby。
- 程序会扫描目录内所有普通文件并保存到 state，但只有 `scan.notify_extensions` 匹配的文件变化会触发日志和 Emby 刷新。
- 如果某个监控目录无法访问，会跳过该目录并保留旧 state。
- 如果某个监控目录为空，也按异常处理：跳过该目录并保留旧 state，避免挂载异常时误判为全量删除。
- 运行用户必须有权限遍历监控目录。rclone/FUSE 挂载场景下，必要时需要配置 `allow_other` 等权限。

## 运行

直接运行：

```sh
./emby-autoscan-linux-amd64 -config ./config.yaml
```

如果没有传 `-config`，程序会尝试读取二进制同目录下的 `config.yaml`。

## run-forever.sh 用法

发布包里带有 `run-forever.sh`，用于前台运行并在异常退出后自动重启。

基本用法：

```sh
cp config.example.yaml config.yaml
editor config.yaml
./run-forever.sh
```

指定配置文件：

```sh
./run-forever.sh -config /path/to/config.yaml
```

或者使用环境变量：

```sh
EMBY_AUTOSCAN_CONFIG=/path/to/config.yaml ./run-forever.sh
```

设置异常退出后的重启等待时间，默认 30 秒：

```sh
EMBY_AUTOSCAN_RESTART_DELAY=10 ./run-forever.sh
```

`run-forever.sh` 会根据机器架构自动寻找同目录下的二进制：

- `x86_64` / `amd64`：`emby-autoscan-linux-amd64`
- `aarch64` / `arm64`：`emby-autoscan-linux-arm64`

如果程序正常退出，或收到 `Ctrl+C` / `SIGTERM`，脚本不会重启。

## 日志

stdout 只输出适合人工查看的短日志。日志文件会保留结构化字段，便于排查和搜索。

示例：

```text
2026-04-27 10:00:00 [INFO] 新增文件：TV / show.mkv，2.7 GiB，媒体库ID 13855
2026-04-27 10:00:01 [ERROR] 目录为空，可能是挂载异常，跳过此目录：movie1
```

当 `logging.debug: false` 时，无变化周期不会输出 summary，避免日志过多。
