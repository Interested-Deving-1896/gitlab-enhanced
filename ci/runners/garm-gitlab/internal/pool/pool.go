// Package pool manages the lifecycle of Incus runner instances for GitLab CI.
//
// Design (adapted from cloudbase/garm):
//
//   - A Pool maps to a set of Incus containers/VMs sharing the same image,
//     resource profile, and runner tag set.
//   - The pool manager watches the GitLab job webhook event channel and the
//     idle runner list to decide when to scale up or down.
//   - Each instance runs gitlab-runner in custom executor mode, using the
//     scripts in executor/ to manage Incus containers per job.
//
// Scale-up trigger:  a "pending" job event arrives whose tags match the pool.
// Scale-down trigger: idle runner count exceeds MinIdle after a cooldown period.
package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/config"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/gitlab"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/provider"
)

// Instance represents a single Incus runner instance managed by the pool.
type Instance struct {
	ID          string    // Incus container/VM name
	RunnerID    int64     // GitLab runner numeric ID
	RunnerToken string    // gitlab-runner poll token
	PoolID      string    // which pool owns this instance
	CreatedAt   time.Time
	LastJobAt   time.Time
	Status      string // "starting", "idle", "running", "stopping"
}

// Pool manages a set of Incus runner instances for a specific tag set.
type Pool struct {
	cfg      config.PoolConfig
	gitlab   *gitlab.Client
	provider provider.IncusProvider
	log      *logrus.Entry

	mu        sync.Mutex
	instances map[string]*Instance // keyed by Incus instance ID
}

// Manager owns all pools and dispatches job events to the right pool.
type Manager struct {
	pools   map[string]*Pool // keyed by pool ID
	eventCh <-chan gitlab.JobEvent
	log     *logrus.Logger
	wg      sync.WaitGroup
}

// NewManager creates a pool manager. cfg contains all pool definitions.
func NewManager(
	cfg []config.PoolConfig,
	gitlabClient *gitlab.Client,
	prov provider.IncusProvider,
	eventCh <-chan gitlab.JobEvent,
	log *logrus.Logger,
) (*Manager, error) {
	m := &Manager{
		pools:   make(map[string]*Pool),
		eventCh: eventCh,
		log:     log,
	}

	for _, pc := range cfg {
		p := &Pool{
			cfg:       pc,
			gitlab:    gitlabClient,
			provider:  prov,
			log:       log.WithField("pool", pc.ID),
			instances: make(map[string]*Instance),
		}
		m.pools[pc.ID] = p
	}

	return m, nil
}

// Run starts the manager event loop. Blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	// Start per-pool reconcile loops.
	for _, p := range m.pools {
		m.wg.Add(1)
		go func(p *Pool) {
			defer m.wg.Done()
			p.reconcileLoop(ctx)
		}(p)
	}

	// Dispatch job events to matching pools.
	for {
		select {
		case <-ctx.Done():
			m.wg.Wait()
			return ctx.Err()
		case event, ok := <-m.eventCh:
			if !ok {
				m.wg.Wait()
				return nil
			}
			m.dispatch(ctx, event)
		}
	}
}

// dispatch routes a job event to all pools whose tag set matches the job tags.
func (m *Manager) dispatch(ctx context.Context, event gitlab.JobEvent) {
	if event.BuildStatus != gitlab.JobStatusPending {
		return
	}

	for _, p := range m.pools {
		if p.matchesTags(event.RunnerTags) {
			m.log.WithFields(logrus.Fields{
				"pool":     p.cfg.ID,
				"build_id": event.BuildID,
				"tags":     event.RunnerTags,
			}).Debug("job matched pool — checking capacity")

			go func(p *Pool) {
				if err := p.scaleUpIfNeeded(ctx); err != nil {
					m.log.WithError(err).WithField("pool", p.cfg.ID).Error("scale up failed")
				}
			}(p)
		}
	}
}

// matchesTags returns true if all of the pool's required tags are present in jobTags.
func (p *Pool) matchesTags(jobTags []string) bool {
	tagSet := make(map[string]struct{}, len(jobTags))
	for _, t := range jobTags {
		tagSet[t] = struct{}{}
	}
	for _, required := range p.cfg.Tags {
		if _, ok := tagSet[required]; !ok {
			return false
		}
	}
	return true
}

// scaleUpIfNeeded creates a new Incus instance if the pool is below MaxRunners
// and has no idle instances available.
func (p *Pool) scaleUpIfNeeded(ctx context.Context) error {
	p.mu.Lock()
	current := len(p.instances)
	idleCount := p.countIdle()
	p.mu.Unlock()

	if idleCount > 0 {
		p.log.Debug("idle runner available — no scale up needed")
		return nil
	}

	if current >= p.cfg.MaxRunners {
		p.log.WithFields(logrus.Fields{
			"current": current,
			"max":     p.cfg.MaxRunners,
		}).Warn("pool at max capacity — job will queue")
		return nil
	}

	return p.createInstance(ctx)
}

// createInstance provisions a new Incus container/VM, registers a GitLab runner
// inside it, and adds it to the pool.
func (p *Pool) createInstance(ctx context.Context) error {
	instanceID := fmt.Sprintf("garm-gitlab-%s-%d", p.cfg.ID, time.Now().UnixNano())

	p.log.WithField("instance_id", instanceID).Info("creating Incus instance")

	// 1. Create the Incus container/VM.
	if err := p.provider.CreateInstance(ctx, provider.CreateInstanceRequest{
		InstanceID:  instanceID,
		Image:       p.cfg.Image,
		Profile:     p.cfg.IncusProfile,
		Privileged:  p.cfg.Privileged,
		ExtraConfig: p.cfg.ExtraConfig,
	}); err != nil {
		return fmt.Errorf("create Incus instance %s: %w", instanceID, err)
	}

	// 2. Wait for the instance to be running.
	if err := p.provider.WaitForInstance(ctx, instanceID, 2*time.Minute); err != nil {
		_ = p.provider.DeleteInstance(ctx, instanceID)
		return fmt.Errorf("wait for instance %s: %w", instanceID, err)
	}

	// 3. Install gitlab-runner inside the instance.
	if err := p.provider.RunCommand(ctx, instanceID, installGitLabRunnerScript(p.cfg)); err != nil {
		_ = p.provider.DeleteInstance(ctx, instanceID)
		return fmt.Errorf("install gitlab-runner in %s: %w", instanceID, err)
	}

	// 4. Register the runner with GitLab.
	tagList := joinTags(p.cfg.Tags)
	info, err := p.gitlab.RegisterRunner(ctx,
		p.cfg.RegistrationToken,
		instanceID,
		tagList,
		p.cfg.RunUntagged,
	)
	if err != nil {
		_ = p.provider.DeleteInstance(ctx, instanceID)
		return fmt.Errorf("register runner for %s: %w", instanceID, err)
	}

	// 5. Start gitlab-runner inside the instance using the registered token.
	if err := p.provider.RunCommand(ctx, instanceID, startGitLabRunnerScript(info.Token, p.cfg)); err != nil {
		_ = p.gitlab.DeleteRunner(ctx, info.Token)
		_ = p.provider.DeleteInstance(ctx, instanceID)
		return fmt.Errorf("start gitlab-runner in %s: %w", instanceID, err)
	}

	p.mu.Lock()
	p.instances[instanceID] = &Instance{
		ID:          instanceID,
		RunnerID:    info.ID,
		RunnerToken: info.Token,
		PoolID:      p.cfg.ID,
		CreatedAt:   time.Now(),
		Status:      "idle",
	}
	p.mu.Unlock()

	p.log.WithFields(logrus.Fields{
		"instance_id": instanceID,
		"runner_id":   info.ID,
	}).Info("instance ready")

	return nil
}

// reconcileLoop runs on a ticker to enforce MinIdle and scale down excess idle runners.
func (p *Pool) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.reconcile(ctx)
		}
	}
}

func (p *Pool) reconcile(ctx context.Context) {
	p.mu.Lock()
	current := len(p.instances)
	idle := p.countIdle()
	p.mu.Unlock()

	// Scale up to MinIdle if below.
	for idle < p.cfg.MinIdle && current < p.cfg.MaxRunners {
		if err := p.createInstance(ctx); err != nil {
			p.log.WithError(err).Error("reconcile: scale up failed")
			break
		}
		idle++
		current++
	}

	// Scale down excess idle runners beyond MinIdle after IdleTimeout.
	if idle > p.cfg.MinIdle {
		p.scaleDownIdle(ctx, idle-p.cfg.MinIdle)
	}
}

// scaleDownIdle removes up to n idle instances that have been idle longer than IdleTimeout.
func (p *Pool) scaleDownIdle(ctx context.Context, n int) {
	p.mu.Lock()
	candidates := make([]*Instance, 0)
	for _, inst := range p.instances {
		if inst.Status == "idle" && time.Since(inst.LastJobAt) > p.cfg.IdleTimeout.Duration {
			candidates = append(candidates, inst)
		}
	}
	p.mu.Unlock()

	removed := 0
	for _, inst := range candidates {
		if removed >= n {
			break
		}
		p.log.WithField("instance_id", inst.ID).Info("scaling down idle instance")
		if err := p.destroyInstance(ctx, inst); err != nil {
			p.log.WithError(err).WithField("instance_id", inst.ID).Error("scale down failed")
			continue
		}
		removed++
	}
}

// destroyInstance deregisters the runner and deletes the Incus instance.
func (p *Pool) destroyInstance(ctx context.Context, inst *Instance) error {
	if err := p.gitlab.DeleteRunner(ctx, inst.RunnerToken); err != nil {
		p.log.WithError(err).Warn("failed to deregister runner — continuing with instance deletion")
	}

	if err := p.provider.DeleteInstance(ctx, inst.ID); err != nil {
		return fmt.Errorf("delete instance %s: %w", inst.ID, err)
	}

	p.mu.Lock()
	delete(p.instances, inst.ID)
	p.mu.Unlock()

	return nil
}

func (p *Pool) countIdle() int {
	n := 0
	for _, inst := range p.instances {
		if inst.Status == "idle" {
			n++
		}
	}
	return n
}

func joinTags(tags []string) string {
	result := ""
	for i, t := range tags {
		if i > 0 {
			result += ","
		}
		result += t
	}
	return result
}

// installGitLabRunnerScript returns a shell script that installs gitlab-runner
// and the custom Incus executor scripts inside the instance.
func installGitLabRunnerScript(cfg config.PoolConfig) string {
	return `#!/bin/sh
set -e
# Install gitlab-runner
curl -fsSL https://packages.gitlab.com/install/repositories/runner/gitlab-runner/script.deb.sh | bash
apt-get install -y gitlab-runner

# Install Incus executor scripts
mkdir -p /opt/garm-gitlab/executor
cat > /opt/garm-gitlab/executor/base.sh << 'EOF'
` + executorBaseScript + `
EOF
cat > /opt/garm-gitlab/executor/prepare.sh << 'EOF'
` + executorPrepareScript + `
EOF
cat > /opt/garm-gitlab/executor/run.sh << 'EOF'
` + executorRunScript + `
EOF
cat > /opt/garm-gitlab/executor/cleanup.sh << 'EOF'
` + executorCleanupScript + `
EOF
chmod +x /opt/garm-gitlab/executor/*.sh
`
}

// startGitLabRunnerScript returns a script that registers and starts gitlab-runner
// in custom executor mode using the installed executor scripts.
func startGitLabRunnerScript(runnerToken string, cfg config.PoolConfig) string {
	return fmt.Sprintf(`#!/bin/sh
set -e
gitlab-runner register \
  --non-interactive \
  --url "%s" \
  --token "%s" \
  --executor custom \
  --custom-prepare-exec /opt/garm-gitlab/executor/prepare.sh \
  --custom-run-exec /opt/garm-gitlab/executor/run.sh \
  --custom-cleanup-exec /opt/garm-gitlab/executor/cleanup.sh \
  --description "%s" \
  --tag-list "%s"

gitlab-runner start
`, cfg.GitLabURL, runnerToken, cfg.ID, joinTags(cfg.Tags))
}

// Executor script content — embedded so the pool manager can inject them
// into new instances without needing external file access.
// These are the extended versions of gitlab-incus-runner's scripts with
// privileged container support for live-build.
const executorBaseScript = `#!/usr/bin/env bash
CONTAINER_ID="runner-${CUSTOM_ENV_CI_RUNNER_ID}-project-${CUSTOM_ENV_CI_PROJECT_ID}-concurrent-${CUSTOM_ENV_CI_CONCURRENT_PROJECT_ID}-${CUSTOM_ENV_CI_JOB_ID}"
CONTAINER_IMAGE="${CUSTOM_ENV_GARM_INCUS_IMAGE:-ubuntu:noble}"
CONTAINER_PRIVILEGED="${CUSTOM_ENV_GARM_INCUS_PRIVILEGED:-false}"
`

const executorPrepareScript = `#!/usr/bin/env bash
currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"
set -eo pipefail

echo "Preparing container: ${CONTAINER_ID}"
LAUNCH_ARGS="--ephemeral"
if [ "${CONTAINER_PRIVILEGED}" = "true" ]; then
  LAUNCH_ARGS="${LAUNCH_ARGS} --config security.privileged=true --config security.nesting=true"
fi

incus launch "${CONTAINER_IMAGE}" "${CONTAINER_ID}" ${LAUNCH_ARGS}

# Wait for network
for i in $(seq 1 30); do
  incus exec "${CONTAINER_ID}" -- sh -c "ping -c1 8.8.8.8 >/dev/null 2>&1" && break
  sleep 2
done

# Install dependencies for live-build if privileged
if [ "${CONTAINER_PRIVILEGED}" = "true" ]; then
  incus exec "${CONTAINER_ID}" -- sh -c "
    apt-get update -qq &&
    apt-get install -y --no-install-recommends live-build debootstrap squashfs-tools xorriso
  "
fi
`

const executorRunScript = `#!/usr/bin/env bash
currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"
set -eo pipefail

incus exec "${CONTAINER_ID}" -- bash < "${1}"
`

const executorCleanupScript = `#!/usr/bin/env bash
currentDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${currentDir}/base.sh"
set -eo pipefail

echo "Cleaning up container: ${CONTAINER_ID}"
incus delete --force "${CONTAINER_ID}" 2>/dev/null || true
`
