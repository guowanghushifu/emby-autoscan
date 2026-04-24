package rclone

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultProcDir = "/proc"
	defaultExePath = "/usr/bin/rclone"
)

type ProcMountChecker struct {
	ProcDir string
	ExePath string
}

func (c ProcMountChecker) RcloneMountRunning() (bool, error) {
	procDir := c.ProcDir
	if procDir == "" {
		procDir = defaultProcDir
	}
	exePath := c.ExePath
	if exePath == "" {
		exePath = defaultExePath
	}

	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}

		pidDir := filepath.Join(procDir, entry.Name())
		cmdline, err := os.ReadFile(filepath.Join(pidDir, "cmdline"))
		if err != nil || !isRcloneMountCommand(cmdline, exePath) {
			continue
		}

		return true, nil
	}

	return false, nil
}

func isRcloneMountCommand(cmdline []byte, exePath string) bool {
	args := cmdlineArgs(cmdline)
	if len(args) == 0 {
		return false
	}
	if args[0] != exePath {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "mount" {
			return true
		}
	}
	return false
}

func cmdlineArgs(cmdline []byte) []string {
	parts := strings.Split(string(cmdline), "\x00")
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			args = append(args, part)
		}
	}
	return args
}
