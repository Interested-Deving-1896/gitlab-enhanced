package environment

import (
	"context"
	"fmt"
	"strings"
	"time"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
)

// IncusManager provisions dev environments as Incus containers.
// This is the default local backend — no cloud account or Kubernetes required.
//
// Lifecycle per environment:
//  1. incus launch <image> <env-id> --profile <profile>
//  2. incus exec <env-id> -- git clone <repoURL> /workspace/<repo>
//  3. incus exec <env-id> -- supervisor (reads devcontainer.json / .gitpod.yml)
//  4. Incus proxy device exposes IDE port to a random host port
//  5. Return IDEURL pointing at the proxied port
type IncusManager struct {
	socket  string
	profile string
	network string
	idePort int
	conn    incus.InstanceServer
}

func NewIncusManager(socket, profile, network string, idePort int) *IncusManager {
	if socket == "" {
		socket = "/var/lib/incus/unix.socket"
	}
	if profile == "" {
		profile = "workspace-default"
	}
	if network == "" {
		network = "incusbr0"
	}
	if idePort == 0 {
		idePort = 3000
	}
	return &IncusManager{
		socket:  socket,
		profile: profile,
		network: network,
		idePort: idePort,
	}
}

func (m *IncusManager) Name() string { return "incus-environment" }

func (m *IncusManager) connect() (incus.InstanceServer, error) {
	if m.conn != nil {
		return m.conn, nil
	}
	conn, err := incus.ConnectIncusUnix(m.socket, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to Incus at %s: %w", m.socket, err)
	}
	m.conn = conn
	return conn, nil
}

func (m *IncusManager) Available(_ context.Context) bool {
	_, err := m.connect()
	return err == nil
}

// Create provisions and starts a new dev environment container.
func (m *IncusManager) Create(ctx context.Context, spec Spec) (*Environment, error) {
	conn, err := m.connect()
	if err != nil {
		return nil, err
	}

	name := envName(spec.ID)
	image := spec.Image
	if image == "" {
		image = "gitlab-enhanced/workspace-full:latest"
	}

	// Create the container
	op, err := conn.CreateInstance(api.InstancesPost{
		Name: name,
		Type: api.InstanceTypeContainer,
		Source: api.InstanceSource{
			Type:  "image",
			Alias: image,
		},
		InstancePut: api.InstancePut{
			Profiles: []string{"default", m.profile},
			Config: map[string]string{
				"security.nesting":                          "true",
				"security.syscalls.intercept.mknod":         "true",
				"security.syscalls.intercept.setxattr":      "true",
				"user.gitlab-enhanced.env-id":               spec.ID,
				"user.gitlab-enhanced.repo-url":             spec.RepoURL,
				"user.gitlab-enhanced.branch":               spec.Branch,
				"user.gitlab-enhanced.ide":                  spec.IDE,
				"user.gitlab-enhanced.devcontainer-path":    spec.DevcontainerPath,
			},
			Devices: map[string]map[string]string{
				"workspace": {
					"type": "disk",
					"path": "/workspace",
					"size": diskSize(spec.Resources.Disk),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating environment container: %w", err)
	}
	if err := op.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for container creation: %w", err)
	}

	// Start the container
	startOp, err := conn.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "start",
		Timeout: 60,
	}, "")
	if err != nil {
		return nil, fmt.Errorf("starting environment container: %w", err)
	}
	if err := startOp.Wait(); err != nil {
		return nil, fmt.Errorf("waiting for container start: %w", err)
	}

	// Wait for init system
	if err := m.waitReady(ctx, conn, name); err != nil {
		return nil, err
	}

	// Clone the repository
	if spec.RepoURL != "" {
		if err := m.cloneRepo(ctx, conn, name, spec); err != nil {
			return nil, fmt.Errorf("cloning repository: %w", err)
		}
	}

	// Start the IDE (supervisor reads devcontainer.json / .gitpod.yml)
	if err := m.startIDE(ctx, conn, name, spec); err != nil {
		return nil, fmt.Errorf("starting IDE: %w", err)
	}

	// Add proxy device to expose IDE port on a host port
	hostPort, err := m.addProxyDevice(conn, name)
	if err != nil {
		return nil, fmt.Errorf("adding proxy device: %w", err)
	}

	return &Environment{
		ID:      spec.ID,
		Spec:    spec,
		Status:  StatusRunning,
		IDEURL:  fmt.Sprintf("http://localhost:%d", hostPort),
		SSHHost: fmt.Sprintf("incus exec %s -- bash", name),
		Created: time.Now(),
	}, nil
}

// Get returns the current state of an environment.
func (m *IncusManager) Get(_ context.Context, id string) (*Environment, error) {
	conn, err := m.connect()
	if err != nil {
		return nil, err
	}
	name := envName(id)
	inst, _, err := conn.GetInstance(name)
	if err != nil {
		return nil, fmt.Errorf("getting environment %s: %w", id, err)
	}
	return m.instanceToEnv(inst), nil
}

// List returns all environments, optionally filtered by status.
func (m *IncusManager) List(_ context.Context, status Status) ([]*Environment, error) {
	conn, err := m.connect()
	if err != nil {
		return nil, err
	}
	instances, err := conn.GetInstances(api.InstanceTypeContainer)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	var envs []*Environment
	for _, inst := range instances {
		// Only return instances managed by gitlab-enhanced
		if _, ok := inst.Config["user.gitlab-enhanced.env-id"]; !ok {
			continue
		}
		env := m.instanceToEnv(&inst)
		if status == "" || env.Status == status {
			envs = append(envs, env)
		}
	}
	return envs, nil
}

// Stop halts a running environment container.
func (m *IncusManager) Stop(_ context.Context, id string) error {
	conn, err := m.connect()
	if err != nil {
		return err
	}
	op, err := conn.UpdateInstanceState(envName(id), api.InstanceStatePut{
		Action:  "stop",
		Timeout: 30,
	}, "")
	if err != nil {
		return fmt.Errorf("stopping environment %s: %w", id, err)
	}
	return op.Wait()
}

// Start resumes a stopped environment container.
func (m *IncusManager) Start(ctx context.Context, id string) error {
	conn, err := m.connect()
	if err != nil {
		return err
	}
	op, err := conn.UpdateInstanceState(envName(id), api.InstanceStatePut{
		Action:  "start",
		Timeout: 60,
	}, "")
	if err != nil {
		return fmt.Errorf("starting environment %s: %w", id, err)
	}
	if err := op.Wait(); err != nil {
		return err
	}
	// Re-start the IDE after resume
	inst, _, err := conn.GetInstance(envName(id))
	if err != nil {
		return err
	}
	spec := Spec{
		ID:               id,
		IDE:              inst.Config["user.gitlab-enhanced.ide"],
		DevcontainerPath: inst.Config["user.gitlab-enhanced.devcontainer-path"],
	}
	return m.startIDE(ctx, conn, envName(id), spec)
}

// Delete destroys an environment container and frees all resources.
func (m *IncusManager) Delete(_ context.Context, id string) error {
	conn, err := m.connect()
	if err != nil {
		return err
	}
	name := envName(id)
	// Stop first (force) if running
	stopOp, err := conn.UpdateInstanceState(name, api.InstanceStatePut{
		Action:  "stop",
		Timeout: 10,
		Force:   true,
	}, "")
	if err == nil {
		stopOp.Wait() //nolint:errcheck
	}
	op, err := conn.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("deleting environment %s: %w", id, err)
	}
	return op.Wait()
}

// waitReady polls until the container's init system is running.
func (m *IncusManager) waitReady(ctx context.Context, conn incus.InstanceServer, name string) error {
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, err := conn.ExecInstance(name, api.InstanceExecPost{
			Command:   []string{"systemctl", "is-system-running"},
			WaitForWS: true,
		}, nil)
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("environment container did not become ready within 60s")
}

// cloneRepo clones the repository into /workspace inside the container.
func (m *IncusManager) cloneRepo(_ context.Context, conn incus.InstanceServer, name string, spec Spec) error {
	cloneCmd := fmt.Sprintf("git clone %s /workspace/repo", spec.RepoURL)
	if spec.Branch != "" {
		cloneCmd = fmt.Sprintf("git clone --branch %s %s /workspace/repo", spec.Branch, spec.RepoURL)
	}
	_, err := conn.ExecInstance(name, api.InstanceExecPost{
		Command:   []string{"bash", "-c", cloneCmd},
		WaitForWS: true,
	}, nil)
	return err
}

// startIDE launches the IDE process inside the container.
// supervisor reads devcontainer.json or .gitpod.yml and starts the IDE.
func (m *IncusManager) startIDE(_ context.Context, conn incus.InstanceServer, name string, spec Spec) error {
	ide := spec.IDE
	if ide == "" {
		ide = "openvscode-server"
	}

	var startCmd string
	switch ide {
	case "openvscode-server":
		startCmd = fmt.Sprintf(
			"nohup /usr/local/bin/openvscode-server --host 0.0.0.0 --port %d "+
				"--without-connection-token --default-folder /workspace/repo "+
				"> /var/log/ide.log 2>&1 &",
			m.idePort,
		)
	default:
		startCmd = fmt.Sprintf(
			"nohup supervisor --ide %s --port %d > /var/log/ide.log 2>&1 &",
			ide, m.idePort,
		)
	}

	_, err := conn.ExecInstance(name, api.InstanceExecPost{
		Command:   []string{"bash", "-c", startCmd},
		WaitForWS: true,
	}, nil)
	return err
}

// addProxyDevice adds an Incus proxy device that forwards a random host port
// to the IDE port inside the container. Returns the allocated host port.
func (m *IncusManager) addProxyDevice(conn incus.InstanceServer, name string) (int, error) {
	// Use a deterministic port derived from the container name to avoid conflicts
	hostPort := 10000 + (hashName(name) % 50000)

	inst, etag, err := conn.GetInstance(name)
	if err != nil {
		return 0, err
	}

	inst.Devices["ide-proxy"] = map[string]string{
		"type":    "proxy",
		"listen":  fmt.Sprintf("tcp:0.0.0.0:%d", hostPort),
		"connect": fmt.Sprintf("tcp:127.0.0.1:%d", m.idePort),
	}

	op, err := conn.UpdateInstance(name, inst.Writable(), etag)
	if err != nil {
		return 0, fmt.Errorf("adding proxy device: %w", err)
	}
	if err := op.Wait(); err != nil {
		return 0, fmt.Errorf("waiting for proxy device: %w", err)
	}
	return hostPort, nil
}

// instanceToEnv converts an Incus instance to an Environment.
func (m *IncusManager) instanceToEnv(inst *api.Instance) *Environment {
	id := inst.Config["user.gitlab-enhanced.env-id"]
	if id == "" {
		id = inst.Name
	}
	status := StatusStopped
	if inst.Status == "Running" {
		status = StatusRunning
	}
	return &Environment{
		ID: id,
		Spec: Spec{
			ID:      id,
			RepoURL: inst.Config["user.gitlab-enhanced.repo-url"],
			Branch:  inst.Config["user.gitlab-enhanced.branch"],
			IDE:     inst.Config["user.gitlab-enhanced.ide"],
		},
		Status:  status,
		Created: inst.CreatedAt,
	}
}

// envName converts an environment ID to a valid Incus instance name.
func envName(id string) string {
	name := "env-" + strings.ToLower(id)
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	if len(result) > 63 {
		result = result[:63]
	}
	return strings.Trim(result, "-")
}

// diskSize returns a valid Incus disk size string, defaulting to 30GiB.
func diskSize(s string) string {
	if s == "" {
		return "30GiB"
	}
	return s
}

// hashName produces a stable integer from a string for port allocation.
func hashName(s string) int {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

var _ Manager = (*IncusManager)(nil)
