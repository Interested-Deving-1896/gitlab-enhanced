package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newLFSCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lfs",
		Short: "Manage the Git LFS server",
	}
	cmd.AddCommand(
		newLFSServeCmd(cfgRoot),
		newLFSStatusCmd(cfgRoot),
	)
	return cmd
}

func newLFSServeCmd(cfgRoot *string) *cobra.Command {
	var (
		addr   string
		server string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the LFS server in the foreground",
		Long: `Starts the configured LFS server (rudolfs or giftless) in the foreground.
For production use, run 'gitlab-enhanced up' which manages the server as an Incus container.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLFSServe(*cfgRoot, addr, server)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "0.0.0.0:8080", "listen address")
	cmd.Flags().StringVar(&server, "server", "", "LFS server to use (overrides config: rudolfs|giftless)")

	return cmd
}

func runLFSServe(root, addr, serverOverride string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	server := cfg.LFS.Server
	if serverOverride != "" {
		server = serverOverride
	}

	printSection("LFS server")
	printInfo(fmt.Sprintf("server:  %s", server))
	printInfo(fmt.Sprintf("backend: %s", cfg.LFS.Backend))
	printInfo(fmt.Sprintf("path:    %s", cfg.LFS.Path))
	printInfo(fmt.Sprintf("addr:    %s", addr))
	fmt.Println()

	// Use a signal-aware context so SIGINT/SIGTERM propagate to the child
	// process and it can flush in-flight LFS writes before exiting.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch server {
	case "rudolfs":
		return runRudolfs(ctx, addr, cfg.LFS.Path, cfg.LFS.Encryption)
	case "giftless":
		return runGiftless(ctx, addr, cfg.LFS.Path)
	case "lfs-test-server":
		return runLFSTestServer(ctx, addr, cfg.LFS.Path)
	default:
		return fmt.Errorf("unknown LFS server %q — supported: rudolfs, giftless, lfs-test-server", server)
	}
}

// runRudolfs starts rudolfs (Rust LFS server) as a subprocess.
// The process is terminated when ctx is cancelled (SIGINT/SIGTERM).
func runRudolfs(ctx context.Context, addr, storagePath string, encryption bool) error {
	if err := checkBinary("rudolfs"); err != nil {
		return fmt.Errorf("rudolfs not found — build it from lfs/server/rudolfs/ or install from https://github.com/jasonwhite/rudolfs")
	}
	args := []string{
		"--host", addr,
		"--cache-dir", storagePath,
	}
	if encryption {
		args = append(args, "--encrypt")
	}
	printOK(fmt.Sprintf("starting rudolfs on %s", addr))
	return runCmdContext(ctx, "rudolfs", args...)
}

// runGiftless starts giftless (Python LFS server) as a subprocess.
// The process is terminated when ctx is cancelled (SIGINT/SIGTERM).
func runGiftless(ctx context.Context, addr, storagePath string) error {
	if err := checkBinary("giftless-server"); err != nil {
		return fmt.Errorf("giftless not found — install from lfs/server/giftless/ with: pip install giftless")
	}
	printOK(fmt.Sprintf("starting giftless on %s", addr))
	return runCmdContext(ctx, "giftless-server",
		"--host", addr,
		"--storage-path", storagePath,
	)
}

// runLFSTestServer starts the reference LFS test server.
// The process is terminated when ctx is cancelled (SIGINT/SIGTERM).
func runLFSTestServer(ctx context.Context, addr, storagePath string) error {
	if err := checkBinary("lfs-test-server"); err != nil {
		return fmt.Errorf("lfs-test-server not found — build from lfs/server/lfs-test-server/")
	}
	printOK(fmt.Sprintf("starting lfs-test-server on %s", addr))
	return runCmdContext(ctx, "lfs-test-server",
		"-host", addr,
		"-dir", storagePath,
	)
}

func newLFSStatusCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check LFS server health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLFSStatus(*cfgRoot)
		},
	}
}

func runLFSStatus(root string) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	printSection("LFS server status")
	printInfo(fmt.Sprintf("configured server: %s", cfg.LFS.Server))
	printInfo(fmt.Sprintf("backend:           %s", cfg.LFS.Backend))
	printInfo(fmt.Sprintf("storage path:      %s", cfg.LFS.Path))

	// Try to reach the LFS server health endpoint
	endpoints := []string{
		"http://localhost:8080/",
		"http://localhost:8080/healthz",
		"http://localhost:8080/_health",
	}

	client := &http.Client{Timeout: 3 * time.Second}
	reachable := false
	for _, ep := range endpoints {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ep, nil)
		resp, err := client.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				printOK(fmt.Sprintf("LFS server reachable at %s (HTTP %d)", ep, resp.StatusCode))
				reachable = true
				break
			}
		}
	}
	if !reachable {
		printWarn("LFS server not reachable on localhost:8080 — run 'gitlab-enhanced lfs serve' or 'gitlab-enhanced up'")
	}
	return nil
}
