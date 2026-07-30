package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDirenvShellHelper(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	for _, path := range []string{
		"shell/direnv/ai-dev.sh",
		"shell/direnv/ai-dev_test.sh",
	} {
		if _, err := os.ReadFile(path); err != nil {
			t.Fatalf("read shell test dependency %s: %v", path, err)
		}
	}

	binaryPath := filepath.Join(t.TempDir(), "ai-dev")
	build := exec.Command(
		"go",
		"build",
		"-trimpath",
		"-o",
		binaryPath,
		".",
	)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")

	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build temporary ai-dev binary: %v\n%s", err, output)
	}

	command := exec.Command("bash", "shell/direnv/ai-dev_test.sh")
	command.Env = append(
		os.Environ(),
		"AI_DEV_TEST_BINARY="+binaryPath,
	)
	output, err := command.CombinedOutput()

	if err != nil {
		t.Fatalf("shell/direnv/ai-dev_test.sh failed: %v\n%s", err, output)
	}

	t.Log(string(output))
}
