package build

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

const (
	builderVMName    = "gitlab-enhanced-buildkit"
	builderVMImage   = "images:ubuntu/22.04/cloud"
	builderCacheVol  = "gitlab-enhanced-buildkit-cache"
	buildkitPort     = 1234
	buildkitSockPath = "/run/buildkit/buildkitd.sock"
)

// IncusBuilder runs BuildKit inside a persistent Incus VM.
// The VM is created on first use and kept running between builds for cache reuse.
// Build cache is stored in a dedicated Incus storage volume (persistent across VM restarts).
type IncusBuilder struct {
	socket    string
	cachePool string
	vmName    string
	conn      incus.InstanceServer
}

func NewIncusBuilder(socket, cachePool string) *IncusBuilder {
	if socket == "" {
		socket = "/var/lib/incus/unix.socket"
	}
	if cachePool == "" {
		cachePool = "default"
	}
	return &IncusBuilder{
		socket:    socket,
		cachePool: cachePool,
		vmName:    builderVMName,
	}
}

func (b *IncusBuilder) Name() string { return "incus-buildkit:" + b.vmName }

func (b *IncusBuilder) Available(ctx context.Context) bool {
	conn, err := b.connect()
	if err != nil {
		return false
	}
	_, _, err = conn.GetInstance(b.vmName)
	// Available if we can connect to Incus, regardless of whether the VM exists yet
	return err == nil || strings.Contains(err.Error(), "not found")
}

func (b *IncusBuilder) connect() (incus.InstanceServer, error) {
	if b.conn != nil {
		return b.conn, nil
	}
	conn, err := incus.ConnectIncusUnix(b.socket, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to Incus at %s: %w", b.socket, err)
	}
	b.conn = conn
	return conn, nil
}

func (b *IncusBuilder) Build(ctx context.Context, req BuildRequest, logs io.Writer) (*BuildResult, error) {
	start := time.Now()

	conn, err := b.connect()
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(logs, "[incus-buildkit] ensuring builder VM %q is running\n", b.vmName)
	if err := b.ensureVM(ctx, conn, logs); err != nil {
		return nil, fmt.Errorf("ensuring builder VM: %w", err)
	}

	fmt.Fprintf(logs, "[incus-buildkit] pushing build context\n")
	contextDir := req.ContextDir
	if contextDir == "" {
		contextDir = "."
	}
	if err := b.pushContext(ctx, conn, contextDir, logs); err != nil {
		return nil, fmt.Errorf("pushing build context: %w", err)
	}

	fmt.Fprintf(logs, "[incus-buildkit] running build\n")
	imageID, err := b.runBuild(ctx, conn, req, logs)
	if err != nil {
		return nil, fmt.Errorf("running build: %w", err)
	}

	return &BuildResult{
		ImageID:       imageID,
		Tags:          req.Tags,
		BuildDuration: time.Since(start).Round(time.Second).String(),
	}, nil
}

// ensureVM creates and starts the BuildKit VM if it doesn't exist.
func (b *IncusBuilder) ensureVM(ctx context.Context, conn incus.InstanceServer, logs io.Writer) error {
	inst, _, err := conn.GetInstance(b.vmName)
	if err != nil {
		// VM doesn't exist — create it
		fmt.Fprintf(logs, "[incus-buildkit] creating builder VM\n")
		if err := b.createVM(ctx, conn); err != nil {
			return err
		}
		inst, _, err = conn.GetInstance(b.vmName)
		if err != nil {
			return fmt.Errorf("getting VM after creation: %w", err)
		}
	}

	if inst.Status != "Running" {
		fmt.Fprintf(logs, "[incus-buildkit] starting builder VM\n")
		op, err := conn.UpdateInstanceState(b.vmName, api.InstanceStatePut{
			Action:  "start",
			Timeout: 60,
		}, "")
		if err != nil {
			return fmt.Errorf("starting VM: %w", err)
		}
		if err := op.Wait(); err != nil {
			return fmt.Errorf("waiting for VM start: %w", err)
		}
		// Wait for buildkitd to be ready
		if err := b.waitForBuildkit(ctx, conn, logs); err != nil {
			return err
		}
	}
	return nil
}

// createVM provisions the BuildKit VM with a persistent cache volume.
func (b *IncusBuilder) createVM(_ context.Context, conn incus.InstanceServer) error {
	// Ensure cache volume exists
	_, _, err := conn.GetStoragePoolVolume(b.cachePool, "custom", builderCacheVol)
	if err != nil {
		if err := conn.CreateStoragePoolVolume(b.cachePool, api.StorageVolumesPost{
			Name: builderCacheVol,
			Type: "custom",
			StorageVolumePut: api.StorageVolumePut{
				Config: map[string]string{"size": "50GiB"},
			},
		}); err != nil {
			return fmt.Errorf("creating cache volume: %w", err)
		}
	}

	op, err := conn.CreateInstance(api.InstancesPost{
		Name: b.vmName,
		Type: api.InstanceTypeVM,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: builderVMImage,
		},
		InstancePut: api.InstancePut{
			Config: map[string]string{
				"limits.cpu":    "4",
				"limits.memory": "8GiB",
				// cloud-init installs buildkit on first boot
				"user.user-data": buildkitCloudInit,
			},
			Devices: map[string]map[string]string{
				"cache": {
					"type":   "disk",
					"pool":   b.cachePool,
					"source": builderCacheVol,
					"path":   "/var/cache/buildkit",
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("creating builder VM: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("waiting for VM creation: %w", err)
	}

	// Start the VM
	startOp, err := conn.UpdateInstanceState(b.vmName, api.InstanceStatePut{
		Action:  "start",
		Timeout: 60,
	}, "")
	if err != nil {
		return fmt.Errorf("starting builder VM: %w", err)
	}
	return startOp.Wait()
}

// waitForBuildkit polls until buildkitd is ready inside the VM.
func (b *IncusBuilder) waitForBuildkit(ctx context.Context, conn incus.InstanceServer, logs io.Writer) error {
	fmt.Fprintf(logs, "[incus-buildkit] waiting for buildkitd to be ready")
	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var stdout, stderr bytes.Buffer
		_, err := conn.ExecInstance(b.vmName, api.InstanceExecPost{
			Command:     []string{"buildctl", "debug", "workers"},
			WaitForWS:   true,
			Interactive: false,
		}, &incus.InstanceExecArgs{
			Stdout: &stdout,
			Stderr: &stderr,
		})
		if err == nil {
			fmt.Fprintf(logs, " ready\n")
			return nil
		}
		fmt.Fprintf(logs, ".")
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("buildkitd did not become ready within 120s")
}

// pushContext copies the build context directory into the VM.
func (b *IncusBuilder) pushContext(_ context.Context, conn incus.InstanceServer, contextDir string, logs io.Writer) error {
	// Create /tmp/build-context in VM
	conn.ExecInstance(b.vmName, api.InstanceExecPost{ //nolint:errcheck
		Command: []string{"rm", "-rf", "/tmp/build-context"},
	}, nil)
	conn.ExecInstance(b.vmName, api.InstanceExecPost{ //nolint:errcheck
		Command: []string{"mkdir", "-p", "/tmp/build-context"},
	}, nil)

	// Walk and push each file
	return filepath.Walk(contextDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(contextDir, path)
		vmPath := "/tmp/build-context/" + filepath.ToSlash(rel)

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		fmt.Fprintf(logs, "[incus-buildkit] pushing %s\n", rel)
		return conn.CreateInstanceFile(b.vmName, vmPath, incus.InstanceFileArgs{
			Content:  f,
			Mode:     int(info.Mode()),
			Type:     "file",
			WriteMode: "overwrite",
		})
	})
}

// runBuild executes buildctl inside the VM and returns the image digest.
func (b *IncusBuilder) runBuild(_ context.Context, conn incus.InstanceServer, req BuildRequest, logs io.Writer) (string, error) {
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	// Build the buildctl command
	cmd := []string{
		"buildctl", "build",
		"--frontend", "dockerfile.v0",
		"--opt", "filename=" + dockerfile,
		"--local", "context=/tmp/build-context",
		"--local", "dockerfile=/tmp/build-context",
		"--export-cache", "type=local,dest=/var/cache/buildkit",
		"--import-cache", "type=local,src=/var/cache/buildkit",
	}

	// Add build args
	for k, v := range req.BuildArgs {
		cmd = append(cmd, "--opt", fmt.Sprintf("build-arg:%s=%s", k, v))
	}

	// Output: push to registry or load locally
	if len(req.Tags) > 0 {
		outputType := "image"
		if req.Push {
			outputType = "image,push=true"
		}
		cmd = append(cmd, "--output",
			fmt.Sprintf("type=%s,name=%s", outputType, strings.Join(req.Tags, ",")))
	}

	// Multi-platform
	if len(req.Platforms) > 0 {
		cmd = append(cmd, "--opt", "platform="+strings.Join(req.Platforms, ","))
	}

	var stdout, stderr bytes.Buffer
	_, err := conn.ExecInstance(b.vmName, api.InstanceExecPost{
		Command:     cmd,
		WaitForWS:   true,
		Interactive: false,
	}, &incus.InstanceExecArgs{
		Stdout: io.MultiWriter(logs, &stdout),
		Stderr: io.MultiWriter(logs, &stderr),
	})
	if err != nil {
		return "", fmt.Errorf("buildctl: %w\nstderr: %s", err, stderr.String())
	}

	// Extract digest from buildctl output
	imageID := extractDigest(stdout.String())
	return imageID, nil
}

// extractDigest parses the image digest from buildctl output.
func extractDigest(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "sha256:") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if strings.HasPrefix(p, "sha256:") {
					return p
				}
			}
		}
	}
	return ""
}

// buildkitCloudInit is the cloud-init user-data that installs buildkit on first boot.
const buildkitCloudInit = `#cloud-config
packages:
  - apt-transport-https
  - ca-certificates
  - curl
runcmd:
  - |
    BUILDKIT_VERSION=v0.15.1
    curl -fsSL https://github.com/moby/buildkit/releases/download/${BUILDKIT_VERSION}/buildkit-${BUILDKIT_VERSION}.linux-amd64.tar.gz \
      | tar -xz -C /usr/local
    cat > /etc/systemd/system/buildkitd.service << 'EOF'
    [Unit]
    Description=BuildKit daemon
    After=network.target
    [Service]
    ExecStart=/usr/local/bin/buildkitd --root /var/cache/buildkit
    Restart=always
    [Install]
    WantedBy=multi-user.target
    EOF
    systemctl daemon-reload
    systemctl enable buildkitd
    systemctl start buildkitd
`

var _ Builder = (*IncusBuilder)(nil)
