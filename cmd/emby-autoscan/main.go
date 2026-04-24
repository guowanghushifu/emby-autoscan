package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/guowanghushifu/emby-autoscan/internal/app"
	"github.com/guowanghushifu/emby-autoscan/internal/config"
	"github.com/guowanghushifu/emby-autoscan/internal/emby"
	"github.com/guowanghushifu/emby-autoscan/internal/logging"
	"github.com/guowanghushifu/emby-autoscan/internal/rclone"
	"github.com/guowanghushifu/emby-autoscan/internal/snapshot"
	"github.com/guowanghushifu/emby-autoscan/internal/state"
)

type FileScanner struct{}

func (FileScanner) Scan(m config.MonitorConfig) (snapshot.MonitorSnapshot, error) {
	return snapshot.ScanMonitor(m.Name, m.Path, m.LibraryID)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("emby-autoscan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "配置文件路径")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		defaultPath, err := defaultConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "查找默认配置失败：%v\n", err)
			return 1
		}
		*configPath = defaultPath
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
		Config:       cfg,
		Scanner:      FileScanner{},
		Store:        state.Store{Path: cfg.Scan.StateFile},
		MountChecker: rclone.ProcMountChecker{},
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

func defaultConfigPath() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executablePath), "config.yaml"), nil
}
