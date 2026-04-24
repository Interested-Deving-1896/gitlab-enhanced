package abstraction

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"time"
)

// ExecServer implements Server by launching an external binary as a subprocess.
// It polls the HTTP health endpoint until the server is ready.
type ExecServer struct {
	backend    string
	listenAddr string
	args       []string
	cmd        *exec.Cmd
}

func (s *ExecServer) Name() string { return s.backend }

func (s *ExecServer) URL() string { return "http://" + s.listenAddr }

// Start launches the server binary and waits up to 30 seconds for it to
// respond on its listen address. Blocks until ctx is cancelled or the process
// exits.
func (s *ExecServer) Start(ctx context.Context, _ Config) error {
	s.cmd = exec.CommandContext(ctx, s.backend, s.args...)
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("lfs %s: start: %w", s.backend, err)
	}

	// Wait for the server to become ready.
	if err := s.waitReady(ctx, 30*time.Second); err != nil {
		_ = s.cmd.Process.Kill()
		return fmt.Errorf("lfs %s: not ready: %w", s.backend, err)
	}

	// Block until the process exits or ctx is cancelled.
	return s.cmd.Wait()
}

// Stop sends an interrupt signal to the server process.
func (s *ExecServer) Stop(_ context.Context) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Kill()
}

// waitReady polls GET / on the server's listen address until it returns HTTP
// 200 or the deadline is exceeded.
func (s *ExecServer) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := client.Get(s.URL() + "/")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s", timeout)
}
