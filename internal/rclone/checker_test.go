package rclone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcMountCheckerFindsUsrBinRcloneMount(t *testing.T) {
	procDir := t.TempDir()
	writeProc(t, procDir, "100", "/usr/bin/rclone", []string{"/usr/bin/rclone", "mount", "remote:path", "/mnt/media"})
	writeProc(t, procDir, "101", "/usr/bin/rclone", []string{"/usr/bin/rclone", "serve", "webdav"})
	writeProc(t, procDir, "102", "/usr/bin/other", []string{"/usr/bin/other", "mount"})

	running, err := ProcMountChecker{ProcDir: procDir, ExePath: "/usr/bin/rclone"}.RcloneMountRunning()
	if err != nil {
		t.Fatalf("RcloneMountRunning() error = %v", err)
	}
	if !running {
		t.Fatal("RcloneMountRunning() = false, want true")
	}
}

func TestProcMountCheckerFindsCommandLineUsrBinRcloneMountWhenExeDiffers(t *testing.T) {
	procDir := t.TempDir()
	writeProc(t, procDir, "100", "/opt/rclone/rclone", []string{"/usr/bin/rclone", "mount", "remote:path", "/mnt/media"})

	running, err := ProcMountChecker{ProcDir: procDir, ExePath: "/usr/bin/rclone"}.RcloneMountRunning()
	if err != nil {
		t.Fatalf("RcloneMountRunning() error = %v", err)
	}
	if !running {
		t.Fatal("RcloneMountRunning() = false, want true")
	}
}

func TestProcMountCheckerReturnsFalseWhenNoRcloneMount(t *testing.T) {
	procDir := t.TempDir()
	writeProc(t, procDir, "100", "/usr/bin/rclone", []string{"/usr/bin/rclone", "serve", "webdav"})
	writeProc(t, procDir, "101", "/usr/local/bin/rclone", []string{"/usr/local/bin/rclone", "mount", "remote:path", "/mnt/media"})

	running, err := ProcMountChecker{ProcDir: procDir, ExePath: "/usr/bin/rclone"}.RcloneMountRunning()
	if err != nil {
		t.Fatalf("RcloneMountRunning() error = %v", err)
	}
	if running {
		t.Fatal("RcloneMountRunning() = true, want false")
	}
}

func writeProc(t *testing.T, procDir, pid, exeTarget string, args []string) {
	t.Helper()

	pidDir := filepath.Join(procDir, pid)
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(exeTarget, filepath.Join(pidDir, "exe")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	cmdline := []byte{}
	for _, arg := range args {
		cmdline = append(cmdline, arg...)
		cmdline = append(cmdline, 0)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), cmdline, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
