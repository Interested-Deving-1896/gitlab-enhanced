//go:build !linux

package main

import (
	"context"
	"os"
	"os/exec"
)

// runCmdContext runs a command attached to the terminal, forwarding stdin/stdout/stderr.
//
// Portable fallback (non-Linux): sends os.Interrupt to the child process when
// ctx is cancelled. This does not reach grandchildren and does not guarantee
// graceful shutdown of process trees, but it is the best available without
// platform-specific process group APIs.
//
// On Linux the full SIGTERM-to-process-group implementation in util_linux.go
// is used instead.
func runCmdContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 5e9 // 5 seconds
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	return cmd.Run()
}
