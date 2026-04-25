package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// repoRoot returns the repository root by walking up from the executable.
// Falls back to the current working directory.
func repoRoot() string {
	// Walk up from cwd looking for go.mod as the repo root marker
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

// loadConfig loads and validates configuration from the given root directory.
func loadConfig(root string) (*config.Config, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", root, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// checkBinary returns an error if the named binary is not in PATH.
func checkBinary(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%q not found in PATH", name)
	}
	return nil
}

// runCmd runs a shell command, streaming stdout/stderr to the terminal.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// runCmdContext runs a command attached to the terminal, forwarding stdin/stdout/stderr.
//
// Graceful shutdown on context cancellation:
//  1. The child is placed in its own process group (Setpgid=true) so that
//     signals sent to the group reach all child processes (e.g. Ansible forks).
//  2. When ctx is cancelled, SIGTERM is sent to the entire process group.
//  3. After a 5-second grace period, SIGKILL is sent if the process has not exited.
//
// This is necessary because exec.CommandContext sends SIGKILL directly to the
// child PID on cancellation, which does not propagate to grandchildren and
// gives the process no opportunity to clean up.
func runCmdContext(ctx context.Context, name string, args ...string) error {
	// Do not use exec.CommandContext — we manage cancellation ourselves so we
	// can send SIGTERM to the process group rather than SIGKILL to the PID.
	cmd := exec.Command(name, args...) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Place the child in a new process group so kill(-pgid) reaches all forks.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for either the process to finish or the context to be cancelled.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err

	case <-ctx.Done():
		// Send SIGTERM to the entire process group (negative PID = group).
		pgid := cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Give the process group 5 seconds to exit cleanly.
		select {
		case err := <-done:
			if err != nil && ctx.Err() != nil {
				// Cancelled by the user — treat as a clean exit.
				return nil
			}
			return err
		case <-time.After(5 * time.Second):
			// Escalate to SIGKILL.
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
			return nil
		}
	}
}

// runCmdSilent runs a command and returns its combined output.
func runCmdSilent(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// buildCmd constructs an exec.Cmd with stdout/stderr wired to the terminal.
// Use this when you need to set Cmd.Dir or other fields before running.
func buildCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd
}

// runCmdWithFileStdin runs name with args, piping the contents of filePath to
// the command's stdin. This replaces shell constructs like "cmd arg < file"
// without invoking a shell, eliminating any risk of shell injection from
// filePath or arg values that contain special characters.
func runCmdWithFileStdin(filePath string, name string, args ...string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// printSection prints a section header.
func printSection(title string) {
	fmt.Printf("\n\033[1;34m▶ %s\033[0m\n", title)
}

// printOK prints a success line.
func printOK(msg string) {
	fmt.Printf("  \033[32m✓\033[0m  %s\n", msg)
}

// printWarn prints a warning line.
func printWarn(msg string) {
	fmt.Printf("  \033[33m⚠\033[0m  %s\n", msg)
}

// printFail prints a failure line.
func printFail(msg string) {
	fmt.Printf("  \033[31m✗\033[0m  %s\n", msg)
}

// printInfo prints an info line.
func printInfo(msg string) {
	fmt.Printf("  \033[90m→\033[0m  %s\n", msg)
}

// isLinux returns true when running on Linux.
func isLinux() bool {
	return runtime.GOOS == "linux"
}

// fileExists returns true if path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// incusNetworkExists returns true if the named Incus network exists.
func incusNetworkExists(name string) bool {
	_, err := runCmdSilent("incus", "network", "show", name)
	return err == nil
}

// incusProfileExists returns true if the named Incus profile exists.
func incusProfileExists(name string) bool {
	_, err := runCmdSilent("incus", "profile", "show", name)
	return err == nil
}

// incusInstanceRunning returns true if the named Incus instance is running.
func incusInstanceRunning(name string) bool {
	out, err := runCmdSilent("incus", "info", name)
	if err != nil {
		return false
	}
	return strings.Contains(out, "Status: RUNNING")
}

// httpGet performs a GET request and returns the response body as a string.
func httpGet(url string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// stringReader wraps a string as an io.Reader for HTTP request bodies.
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}
