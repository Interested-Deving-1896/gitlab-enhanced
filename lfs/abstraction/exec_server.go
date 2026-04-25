package abstraction

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	// done is closed by Start when cmd.Wait() returns.
	// Stop reads from it instead of calling cmd.Wait() again.
	done chan struct{}
}

func (s *ExecServer) Name() string { return s.backend }

func (s *ExecServer) URL() string { return "http://" + s.listenAddr }

// Start launches the server binary and waits up to 30 seconds for it to
// respond on its listen address. Blocks until ctx is cancelled or the process
// exits. cmd.Wait() is called exactly once, inside this method.
func (s *ExecServer) Start(ctx context.Context, _ Config) error {
	s.done = make(chan struct{})
	s.cmd = exec.CommandContext(ctx, s.backend, s.args...)
	if err := s.cmd.Start(); err != nil {
		close(s.done)
		return fmt.Errorf("lfs %s: start: %w", s.backend, err)
	}

	// Wait for the server to become ready.
	if err := s.waitReady(ctx, 30*time.Second); err != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait() // best-effort cleanup after kill
		close(s.done)
		return fmt.Errorf("lfs %s: not ready: %w", s.backend, err)
	}

	// Block until the process exits or ctx is cancelled.
	// cmd.Wait() is called exactly here — Stop() must not call it again.
	err := s.cmd.Wait()
	close(s.done)
	return err
}

// Stop gracefully shuts down the server process.
// It sends SIGTERM first and gives the process 5 seconds to exit cleanly
// before escalating to SIGKILL. It never calls cmd.Wait() — that is owned
// by Start() — so there is no double-Wait race.
func (s *ExecServer) Stop(_ context.Context) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	// Send SIGTERM for graceful shutdown.
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		// Process may have already exited.
		if s.done != nil {
			<-s.done
		}
		return nil
	}
	// Wait for Start's cmd.Wait() to return via the done channel.
	if s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
		return nil
	}
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
