// Package provider implements the Incus compute backend for garm-gitlab.
//
// It wraps the Incus Go client to create, monitor, exec into, and delete
// container/VM instances used as GitLab CI runner hosts.
package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	incus "github.com/lxc/incus/v6/client"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/sirupsen/logrus"
)

// IncusProvider is the interface pool.Pool uses to manage compute instances.
type IncusProvider interface {
	// CreateInstance provisions a new container or VM and starts it.
	CreateInstance(ctx context.Context, req CreateInstanceRequest) error

	// WaitForInstance blocks until the instance is in the Running state or
	// the timeout elapses.
	WaitForInstance(ctx context.Context, instanceID string, timeout time.Duration) error

	// RunCommand executes a shell script inside a running instance via
	// incus exec. The script is passed on stdin.
	RunCommand(ctx context.Context, instanceID string, script string) error

	// DeleteInstance stops and removes the instance. Errors are logged but
	// not fatal — callers should treat this as best-effort cleanup.
	DeleteInstance(ctx context.Context, instanceID string) error
}

// CreateInstanceRequest carries the parameters for a new Incus instance.
type CreateInstanceRequest struct {
	// InstanceID is the Incus instance name (must be unique on the host).
	InstanceID string

	// Image is the Incus image alias or fingerprint, e.g. "ubuntu:noble".
	Image string

	// Profile is the Incus profile to apply, e.g. "default" or "live-build".
	Profile string

	// Privileged enables security.privileged and security.nesting on the
	// instance, required for nested container workloads such as live-build.
	Privileged bool

	// ExtraConfig is merged into the instance config map verbatim.
	ExtraConfig map[string]string
}

// IncusClient wraps the Incus server connection and implements IncusProvider.
type IncusClient struct {
	server incus.InstanceServer
	log    *logrus.Entry
}

// NewIncusClient connects to the local Incus daemon via its Unix socket and
// returns an IncusClient ready for use.
func NewIncusClient(log *logrus.Logger) (*IncusClient, error) {
	srv, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		return nil, fmt.Errorf("connect to Incus socket: %w", err)
	}

	return &IncusClient{
		server: srv,
		log:    log.WithField("component", "incus-provider"),
	}, nil
}

// CreateInstance implements IncusProvider.
func (c *IncusClient) CreateInstance(ctx context.Context, req CreateInstanceRequest) error {
	log := c.log.WithFields(logrus.Fields{
		"instance": req.InstanceID,
		"image":    req.Image,
		"profile":  req.Profile,
	})

	// Build the instance config map.
	cfg := map[string]string{}
	for k, v := range req.ExtraConfig {
		cfg[k] = v
	}
	if req.Privileged {
		cfg["security.privileged"] = "true"
		cfg["security.nesting"] = "true"
		log.Debug("privileged mode enabled")
	}

	profiles := []string{"default"}
	if req.Profile != "" && req.Profile != "default" {
		profiles = append(profiles, req.Profile)
	}

	instanceReq := api.InstancesPost{
		Name: req.InstanceID,
		Type: api.InstanceTypeContainer,
		InstancePut: api.InstancePut{
			Config:   cfg,
			Profiles: profiles,
		},
		Source: api.InstanceSource{
			Type:  "image",
			Alias: req.Image,
		},
	}

	log.Info("creating Incus instance")

	op, err := c.server.CreateInstance(instanceReq)
	if err != nil {
		return fmt.Errorf("CreateInstance API call: %w", err)
	}

	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("wait for instance creation: %w", err)
	}

	// Start the instance.
	startReq := api.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}

	op, err = c.server.UpdateInstanceState(req.InstanceID, startReq, "")
	if err != nil {
		return fmt.Errorf("start instance: %w", err)
	}

	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("wait for instance start: %w", err)
	}

	log.Info("Incus instance started")
	return nil
}

// WaitForInstance implements IncusProvider. It polls the instance state until
// it reaches "Running" or the timeout elapses.
func (c *IncusClient) WaitForInstance(ctx context.Context, instanceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, _, err := c.server.GetInstanceState(instanceID)
			if err != nil {
				c.log.WithError(err).WithField("instance", instanceID).Warn("GetInstanceState error — retrying")
				continue
			}

			if state.Status == "Running" {
				c.log.WithField("instance", instanceID).Debug("instance is Running")
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("instance %s did not reach Running state within %s (current: %s)",
					instanceID, timeout, state.Status)
			}

			c.log.WithFields(logrus.Fields{
				"instance": instanceID,
				"status":   state.Status,
			}).Debug("waiting for instance to reach Running state")
		}
	}
}

// RunCommand implements IncusProvider. It executes the given shell script
// inside the instance by piping it to `bash` via incus exec.
func (c *IncusClient) RunCommand(ctx context.Context, instanceID string, script string) error {
	log := c.log.WithField("instance", instanceID)
	log.Debug("running command in instance")

	var stdout, stderr bytes.Buffer

	execReq := api.InstanceExecPost{
		Command:     []string{"bash"},
		WaitForWS:   true,
		Interactive: false,
	}

	execArgs := incus.InstanceExecArgs{
		Stdin:  io.NopCloser(bytes.NewBufferString(script)),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	op, err := c.server.ExecInstance(instanceID, execReq, &execArgs)
	if err != nil {
		return fmt.Errorf("ExecInstance: %w", err)
	}

	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("exec wait: %w", err)
	}

	// The operation metadata contains the exit code.
	opMeta := op.Get()
	if exitCode, ok := opMeta.Metadata["return"].(float64); ok && exitCode != 0 {
		log.WithFields(logrus.Fields{
			"exit_code": int(exitCode),
			"stderr":    stderr.String(),
		}).Error("command failed in instance")
		return fmt.Errorf("command exited with code %d: %s", int(exitCode), stderr.String())
	}

	if stderr.Len() > 0 {
		log.WithField("stderr", stderr.String()).Debug("command produced stderr output")
	}

	return nil
}

// DeleteInstance implements IncusProvider. It force-stops and deletes the
// instance. Errors during stop are logged and ignored so deletion is always
// attempted.
func (c *IncusClient) DeleteInstance(ctx context.Context, instanceID string) error {
	log := c.log.WithField("instance", instanceID)

	// Force-stop first; ignore errors (instance may already be stopped).
	stopReq := api.InstanceStatePut{
		Action:  "stop",
		Timeout: 30,
		Force:   true,
	}

	if op, err := c.server.UpdateInstanceState(instanceID, stopReq, ""); err != nil {
		log.WithError(err).Debug("stop instance before delete failed — continuing")
	} else {
		if err := op.WaitContext(ctx); err != nil {
			log.WithError(err).Debug("wait for stop failed — continuing")
		}
	}

	log.Info("deleting Incus instance")

	op, err := c.server.DeleteInstance(instanceID)
	if err != nil {
		return fmt.Errorf("DeleteInstance API call: %w", err)
	}

	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("wait for instance deletion: %w", err)
	}

	log.Info("Incus instance deleted")
	return nil
}
