package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/app"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/config"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/emby"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/logging"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/snapshot"
	"github.com/wangdazhuo/fuse-mount-emby-notify/internal/state"
)

type FileScanner struct{}

func (FileScanner) Scan(m config.MonitorConfig) (snapshot.MonitorSnapshot, error) {
	return snapshot.ScanMonitor(m.Name, m.Path, m.LibraryID)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("fuse-mount-emby-notify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "配置文件路径")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "错误：必须指定非空 -config 配置文件路径")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败：%v\n", err)
		return 1
	}

	logger, err := logging.New(os.Stdout, cfg.Logging.Dir, cfg.Logging.RetentionDays, time.Now)
	if err != nil {
		if logger == nil {
			fmt.Fprintf(os.Stderr, "初始化日志失败：%v\n", err)
			return 1
		}
		logger.Error("logger_setup_warning", "警告：日志初始化部分失败，将继续运行", logging.F("error", err))
	}
	if logger != nil {
		defer logger.Close()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	daemon := app.App{
		Config:  cfg,
		Scanner: FileScanner{},
		Store:   state.Store{Path: cfg.Scan.StateFile},
		Notifier: emby.Client{
			BaseURL: cfg.Emby.URL,
			APIKey:  cfg.Emby.APIKey,
		},
		Logger: logger,
	}

	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if logger != nil {
			logger.Error("daemon_run_failed", "守护进程运行失败", logging.F("error", err))
		} else {
			fmt.Fprintf(os.Stderr, "守护进程运行失败：%v\n", err)
		}
		return 1
	}

	return 0
}
