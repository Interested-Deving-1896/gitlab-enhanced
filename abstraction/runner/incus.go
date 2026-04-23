package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// IncusRunner executes CI jobs inside ephemeral Incus VMs.
// Each job gets a fresh VM launched from the configured profile.
// The VM is automatically destroyed after the job completes (ephemeral flag).
type IncusRunner struct {
	socket    string
	vmProfile string
	network   string
	conn      incus.InstanceServer
}

func NewIncusRunner(socket, vmProfile, network string) *IncusRunner {
	if socket == "" {
		socket = "/var/lib/incus/unix.socket"
	}
	if vmProfile == "" {
		vmProfile = "gitlab-runner"
	}
	if network == "" {
		network = "incusbr0"
	}
	return &IncusRunner{socket: socket, vmProfile: vmProfile, network: network}
}

func (r *IncusRunner) Name() string { return "incus-runner:" + r.vmProfile }

func (r *IncusRunner) connect() (incus.InstanceServer, error) {
	if r.conn != nil {
		return r.conn, nil
	}
	conn, err := incus.ConnectIncusUnix(r.socket, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to Incus at %s: %w", r.socket, err)
	}
	r.conn = conn
	return conn, nil
}

func (r *IncusRunner) Available(_ context.Context) bool {
	_, err := r.connect()
	return err == nil
}

// Run provisions an ephemeral VM, executes all job commands in sequence,
// collects artifacts, then destroys the VM.
func (r *IncusRunner) Run(ctx context.Context, job JobSpec, logs io.Writer) (*JobResult, error) {
	start := time.Now()

	conn, err := r.connect()
	if err != nil {
		return nil, err
	}

	vmName := fmt.Sprintf("runner-%s-%d", sanitiseName(job.ID), time.Now().UnixMilli())
	fmt.Fprintf(logs, "[incus-runner] launching VM %q (profile: %s)\n", vmName, r.vmProfile)

	// Launch ephemeral VM
	op, err := conn.CreateInstance(api.InstancesPost{
		Name: vmName,
		Type: api.InstanceTypeVM,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: job.Image,
		},
		InstancePut: api.InstancePut{
			Ephemeral: true,
			Profiles:  []string{"default", r.vmProfile},
			Config:    r.buildConfig(job),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating runner VM: %w", err)
	}
	if err := op.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for VM creation: %w", err)
	}

	// Start VM
	startOp, err := conn.UpdateInstanceState(vmName, api.InstanceStatePut{
		Action:  "start",
		Timeout: 60,
	}, "")
	if err != nil {
		return nil, fmt.Errorf("starting runner VM: %w", err)
	}
	if err := startOp.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for VM start: %w", err)
	}

	// Ensure VM is stopped on exit (ephemeral flag handles deletion)
	defer func() {
		stopOp, err := conn.UpdateInstanceState(vmName, api.InstanceStatePut{
			Action:  "stop",
			Timeout: 30,
			Force:   true,
		}, "")
		if err == nil {
			stopOp.Wait() //nolint:errcheck
		}
	}()

	// Wait for system to be ready
	fmt.Fprintf(logs, "[incus-runner] waiting for VM to be ready\n")
	if err := r.waitReady(ctx, conn, vmName); err != nil {
		return nil, err
	}

	// Inject environment variables
	if len(job.Env) > 0 {
		envScript := r.buildEnvScript(job.Env)
		if err := r.execInVM(ctx, conn, vmName, []string{"bash", "-c", envScript}, logs); err != nil {
			return nil, fmt.Errorf("injecting environment: %w", err)
		}
	}

	// Execute job commands
	exitCode := 0
	for i, cmd := range job.Commands {
		fmt.Fprintf(logs, "[incus-runner] step %d/%d: %s\n", i+1, len(job.Commands), cmd)
		if err := r.execInVM(ctx, conn, vmName, []string{"bash", "-c", cmd}, logs); err != nil {
			exitCode = 1
			fmt.Fprintf(logs, "[incus-runner] step %d failed: %v\n", i+1, err)
			break
		}
	}

	// Collect artifacts
	artifactPaths := make(map[string]string)
	for _, artifact := range job.Artifacts {
		localPath := fmt.Sprintf("/tmp/artifact-%s-%s", vmName, sanitiseName(artifact))
		if err := r.pullFile(conn, vmName, artifact, localPath); err != nil {
			fmt.Fprintf(logs, "[incus-runner] warning: could not collect artifact %q: %v\n", artifact, err)
			continue
		}
		artifactPaths[artifact] = localPath
		fmt.Fprintf(logs, "[incus-runner] collected artifact: %s → %s\n", artifact, localPath)
	}

	return &JobResult{
		JobID:         job.ID,
		ExitCode:      exitCode,
		Duration:      time.Since(start),
		ArtifactPaths: artifactPaths,
	}, nil
}

// Cancel stops a running job VM by name prefix.
func (r *IncusRunner) Cancel(_ context.Context, jobID string) error {
	conn, err := r.connect()
	if err != nil {
		return err
	}
	instances, err := conn.GetInstances(api.InstanceTypeVM)
	if err != nil {
		return fmt.Errorf("listing instances: %w", err)
	}
	prefix := "runner-" + sanitiseName(jobID)
	for _, inst := range instances {
		if strings.HasPrefix(inst.Name, prefix) {
			op, err := conn.UpdateInstanceState(inst.Name, api.InstanceStatePut{
				Action:  "stop",
				Timeout: 10,
				Force:   true,
			}, "")
			if err != nil {
				return fmt.Errorf("stopping %s: %w", inst.Name, err)
			}
			if err := op.Wait(); err != nil {
				return fmt.Errorf("waiting for stop of %s: %w", inst.Name, err)
			}
		}
	}
	return nil
}

// waitReady polls until the VM's init system is running.
func (r *IncusRunner) waitReady(ctx context.Context, conn incus.InstanceServer, vmName string) error {
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var out bytes.Buffer
		conn.ExecInstance(vmName, api.InstanceExecPost{ //nolint:errcheck
			Command:   []string{"systemctl", "is-system-running"},
			WaitForWS: true,
		}, &incus.InstanceExecArgs{Stdout: &out})
		status := strings.TrimSpace(out.String())
		if status == "running" || status == "degraded" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("VM did not become ready within 60s")
}

// execInVM runs a command inside the VM and streams output to logs.
func (r *IncusRunner) execInVM(_ context.Context, conn incus.InstanceServer, vmName string, cmd []string, logs io.Writer) error {
	var stderr bytes.Buffer
	ret, err := conn.ExecInstance(vmName, api.InstanceExecPost{
		Command:     cmd,
		WaitForWS:   true,
		Interactive: false,
	}, &incus.InstanceExecArgs{
		Stdout: logs,
		Stderr: io.MultiWriter(logs, &stderr),
	})
	if err != nil {
		return fmt.Errorf("exec %v: %w", cmd, err)
	}
	if ret != nil {
		if err := ret.Wait(); err != nil {
			return fmt.Errorf("exec %v: %w\nstderr: %s", cmd, err, stderr.String())
		}
	}
	return nil
}

// pullFile copies a file from the VM to the local host.
func (r *IncusRunner) pullFile(conn incus.InstanceServer, vmName, vmPath, localPath string) error {
	content, _, err := conn.GetInstanceFile(vmName, vmPath)
	if err != nil {
		return err
	}
	defer content.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, content)
	return err
}

// buildConfig constructs Incus instance config from a JobSpec.
func (r *IncusRunner) buildConfig(job JobSpec) map[string]string {
	cfg := map[string]string{}
	if job.Resources.CPUs > 0 {
		cfg["limits.cpu"] = fmt.Sprintf("%d", job.Resources.CPUs)
	}
	if job.Resources.Memory != "" {
		cfg["limits.memory"] = job.Resources.Memory
	}
	return cfg
}

// buildEnvScript generates a shell snippet that exports all job env vars.
func (r *IncusRunner) buildEnvScript(env map[string]string) string {
	var sb strings.Builder
	sb.WriteString("cat >> /etc/environment << 'ENVEOF'\n")
	for k, v := range env {
		fmt.Fprintf(&sb, "%s=%s\n", k, v)
	}
	sb.WriteString("ENVEOF\n")
	return sb.String()
}

// sanitiseName makes a string safe for use in an Incus instance name.
func sanitiseName(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	if len(result) > 40 {
		result = result[:40]
	}
	return strings.Trim(result, "-")
}

var _ Runner = (*IncusRunner)(nil)
