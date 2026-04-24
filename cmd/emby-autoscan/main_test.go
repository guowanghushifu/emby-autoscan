package main

import (
	"os"
	"testing"
)

func TestRunWithoutConfigFlagUsesDefaultConfigPath(t *testing.T) {
	workingDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Chdir(temp) error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("Chdir(original) error = %v", err)
		}
	})

	if code := run(nil); code != 1 {
		t.Fatalf("run(nil) exit code = %d, want 1 from loading default config.yaml", code)
	}
}
