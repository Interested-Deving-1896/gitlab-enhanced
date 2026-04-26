// Package config defines the TOML configuration schema for garm-gitlab.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration structure.
type Config struct {
	// API is the HTTP server that receives GitLab webhooks.
	API APIConfig `toml:"api"`

	// GitLab holds credentials for the GitLab instance.
	GitLab GitLabConfig `toml:"gitlab"`

	// Pools defines one or more runner pools.
	Pools []PoolConfig `toml:"pool"`
}

// APIConfig configures the webhook listener.
type APIConfig struct {
	// ListenAddress is the host:port to bind, e.g. "0.0.0.0:8080".
	ListenAddress string `toml:"listen_address"`

	// WebhookSecret is the secret GitLab uses to sign webhook payloads.
	// If empty, signature verification is skipped (not recommended in production).
	WebhookSecret string `toml:"webhook_secret"`
}

// GitLabConfig holds credentials for the GitLab API.
type GitLabConfig struct {
	// URL is the base URL of the GitLab instance, e.g. "https://gitlab.com".
	URL string `toml:"url"`

	// Token is a GitLab personal access token with api scope, used to
	// register and deregister runners.
	Token string `toml:"token"`
}

// PoolConfig defines a single runner pool.
type PoolConfig struct {
	// ID is a short unique identifier for this pool, used in instance names
	// and log fields. Must be alphanumeric + hyphens only.
	ID string `toml:"id"`

	// GitLabURL overrides GitLabConfig.URL for this pool's runner registration.
	// Useful when pools target different GitLab instances or groups.
	GitLabURL string `toml:"gitlab_url"`

	// RegistrationToken is the GitLab runner registration token for this pool.
	// Obtain it from Settings → CI/CD → Runners in the target project/group.
	RegistrationToken string `toml:"registration_token"`

	// Tags is the list of runner tags this pool handles.
	// A job must have all of these tags to be dispatched to this pool.
	Tags []string `toml:"tags"`

	// RunUntagged allows this pool to pick up jobs with no tags when true.
	RunUntagged bool `toml:"run_untagged"`

	// Image is the Incus image alias for new instances, e.g. "ubuntu:noble".
	Image string `toml:"image"`

	// IncusProfile is the Incus profile applied to new instances.
	// Defaults to "default" if empty.
	IncusProfile string `toml:"incus_profile"`

	// Privileged enables security.privileged and security.nesting on job
	// containers. Required for live-build and other nested-container workloads.
	Privileged bool `toml:"privileged"`

	// ExtraConfig is merged verbatim into the Incus instance config map.
	// Example: {"limits.cpu" = "4", "limits.memory" = "8GB"}
	ExtraConfig map[string]string `toml:"extra_config"`

	// MinIdle is the minimum number of idle runner instances to keep warm.
	// The reconcile loop will scale up to this count proactively.
	MinIdle int `toml:"min_idle"`

	// MaxRunners is the hard cap on total instances (idle + running) in this pool.
	MaxRunners int `toml:"max_runners"`

	// IdleTimeout is how long an idle instance must sit unused before it is
	// eligible for scale-down (beyond MinIdle).
	IdleTimeout duration `toml:"idle_timeout"`
}

// duration is a wrapper so TOML strings like "10m" unmarshal to time.Duration.
type duration struct{ time.Duration }

func (d *duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// Load reads and parses the TOML config file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	if _, err := toml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.API.ListenAddress == "" {
		return fmt.Errorf("api.listen_address is required")
	}
	if c.GitLab.URL == "" {
		return fmt.Errorf("gitlab.url is required")
	}
	if c.GitLab.Token == "" {
		return fmt.Errorf("gitlab.token is required")
	}
	if len(c.Pools) == 0 {
		return fmt.Errorf("at least one [[pool]] is required")
	}

	seen := make(map[string]struct{})
	for i, p := range c.Pools {
		if p.ID == "" {
			return fmt.Errorf("pool[%d]: id is required", i)
		}
		if _, dup := seen[p.ID]; dup {
			return fmt.Errorf("pool[%d]: duplicate id %q", i, p.ID)
		}
		seen[p.ID] = struct{}{}

		if p.RegistrationToken == "" {
			return fmt.Errorf("pool %q: registration_token is required", p.ID)
		}
		if p.Image == "" {
			return fmt.Errorf("pool %q: image is required", p.ID)
		}
		if p.MaxRunners <= 0 {
			return fmt.Errorf("pool %q: max_runners must be > 0", p.ID)
		}
		if p.MinIdle < 0 {
			return fmt.Errorf("pool %q: min_idle must be >= 0", p.ID)
		}
		if p.MinIdle > p.MaxRunners {
			return fmt.Errorf("pool %q: min_idle (%d) must be <= max_runners (%d)", p.ID, p.MinIdle, p.MaxRunners)
		}

		// Backfill GitLabURL from the global config if not set per-pool.
		if c.Pools[i].GitLabURL == "" {
			c.Pools[i].GitLabURL = c.GitLab.URL
		}
	}

	return nil
}
