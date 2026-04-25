//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// runCmdContext runs a command attached to the terminal, forwarding stdin/stdout/stderr.
//
// Graceful shutdown on context cancellation (Linux):
//  1. The child is placed in its own process group (Setpgid=true) so that
//     signals sent to the group reach all child processes (e.g. Ansible forks
//     Python workers, rudolfs spawns threads that hold file locks).
//  2. When ctx is cancelled, SIGTERM is sent to the entire process group via
//     syscall.Kill(-pgid, SIGTERM). The negative PID means "process group".
//  3. After a 5-second grace period, SIGKILL is sent if the group has not exited.
//
// We do not use exec.CommandContext because its built-in cancellation sends
// SIGKILL directly to the child PID, which does not propagate to grandchildren
// and gives the process no opportunity to flush writes or release locks.
func runCmdContext(ctx context.Context, name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Place the child in a new process group so kill(-pgid) reaches all forks.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err

	case <-ctx.Done():
		pgid := cmd.Process.Pid
		// Signal the entire process group.
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Give the group 5 seconds to exit cleanly.
		select {
		case err := <-done:
			if err != nil && ctx.Err() != nil {
				// Cancelled by the user — not an error from our perspective.
				return nil
			}
			return err
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
			return nil
		}
	}
}
