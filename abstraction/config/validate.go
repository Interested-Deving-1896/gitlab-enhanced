package config

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError collects all configuration problems found during Validate.
// It implements the error interface so callers can treat it as a single error
// or inspect individual issues via Unwrap.
type ValidationError struct {
	Issues []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed (%d issue(s)):\n  - %s",
		len(e.Issues), strings.Join(e.Issues, "\n  - "))
}

func (e *ValidationError) add(format string, args ...any) {
	e.Issues = append(e.Issues, fmt.Sprintf(format, args...))
}

// Validate checks the configuration for missing required fields and
// inconsistent combinations. It returns a *ValidationError listing all
// problems found, or nil if the configuration is valid.
//
// Call this at startup after Load() so misconfiguration is caught immediately
// rather than at the first request that exercises the affected subsystem.
func (c *Config) Validate() error {
	var ve ValidationError

	c.validateGitLab(&ve)
	c.validateStorage(&ve)
	c.validateBuild(&ve)
	c.validateRunner(&ve)
	c.validateLFS(&ve)
	c.validateCloud(&ve)
	c.validateRewards(&ve)
	c.validateBandwidth(&ve)

	if len(ve.Issues) > 0 {
		return &ve
	}
	return nil
}

func (c *Config) validateGitLab(ve *ValidationError) {
	if c.GitLab.Domain == "" {
		ve.add("gitlab.domain is required")
	}
	if c.GitLab.Edition != "" && c.GitLab.Edition != "ce" && c.GitLab.Edition != "ee" {
		ve.add("gitlab.edition must be 'ce' or 'ee', got %q", c.GitLab.Edition)
	}
}

func (c *Config) validateStorage(ve *ValidationError) {
	switch c.Storage.Backend {
	case "", "local":
		// path defaults to repo root — always valid
	case "cloud":
		if c.Storage.Provider == "" {
			ve.add("storage.provider is required when storage.backend=cloud (aws|gcs|azure|minio|ceph|r2)")
		}
		if c.Storage.Bucket == "" {
			ve.add("storage.bucket is required when storage.backend=cloud")
		}
		switch strings.ToLower(c.Storage.Provider) {
		case "minio", "ceph", "r2":
			if c.Storage.Endpoint == "" {
				ve.add("storage.endpoint is required for provider %q", c.Storage.Provider)
			}
		case "aws", "gcs", "azure", "":
			// endpoint optional
		default:
			ve.add("storage.provider %q is not recognised (aws|gcs|azure|minio|ceph|r2)", c.Storage.Provider)
		}
	case "ipfs", "chain":
		// no extra required fields
	default:
		ve.add("storage.backend %q is not recognised (local|cloud|ipfs|chain)", c.Storage.Backend)
	}
}

func (c *Config) validateBuild(ve *ValidationError) {
	switch c.Build.Backend {
	case "", "incus":
		// socket defaults to /var/lib/incus/unix.socket
	case "depot":
		if c.Build.ProjectID == "" {
			ve.add("build.project_id is required when build.backend=depot")
		}
		if c.Build.Token == "" {
			ve.add("build.token is required when build.backend=depot (set GITLAB_ENHANCED_BUILD_TOKEN or build.token)")
		}
	default:
		ve.add("build.backend %q is not recognised (incus|depot)", c.Build.Backend)
	}
}

func (c *Config) validateRunner(ve *ValidationError) {
	switch c.Runner.Backend {
	case "", "incus":
		// vm_profile defaults to gitlab-runner
	case "blacksmith":
		if c.Runner.Token == "" {
			ve.add("runner.token is required when runner.backend=blacksmith")
		}
		if c.Runner.BlacksmithAPIURL == "" {
			ve.add("runner.blacksmith_api_url is required when runner.backend=blacksmith")
		}
	default:
		ve.add("runner.backend %q is not recognised (incus|blacksmith)", c.Runner.Backend)
	}
}

func (c *Config) validateLFS(ve *ValidationError) {
	switch c.LFS.Server {
	case "", "rudolfs", "giftless", "lfs-test-server":
		// all valid
	default:
		ve.add("lfs.server %q is not recognised (rudolfs|giftless|lfs-test-server)", c.LFS.Server)
	}
	if c.LFS.Encryption && c.LFS.EncryptionKey == "" {
		ve.add("lfs.encryption_key is required when lfs.encryption=true (set GITLAB_ENHANCED_LFS_ENCRYPTION_KEY)")
	}
}

func (c *Config) validateCloud(ve *ValidationError) {
	if !c.Cloud.Enabled {
		return
	}
	switch strings.ToLower(c.Cloud.Provider) {
	case "aws", "gcp", "azure":
		// valid
	case "":
		ve.add("cloud.provider is required when cloud.enabled=true (aws|gcp|azure)")
	default:
		ve.add("cloud.provider %q is not recognised (aws|gcp|azure)", c.Cloud.Provider)
	}
}

func (c *Config) validateRewards(ve *ValidationError) {
	if !c.Rewards.Enabled {
		return
	}
	if c.Rewards.WebhookSecret == "" {
		ve.add("rewards.webhook_secret is required when rewards.enabled=true — " +
			"set GITLAB_ENHANCED_REWARDS_WEBHOOK_SECRET to the token configured in GitLab Admin > System Hooks")
	}

	custodial := c.Rewards.UpholdClientID != ""
	nonCustodial := c.Rewards.WalletAddress != ""

	if !custodial && !nonCustodial {
		ve.add("rewards: at least one of rewards.uphold_client_id (custodial) or " +
			"rewards.wallet_address (non-custodial ERC-20) must be set when rewards.enabled=true")
	}

	// Custodial path: Uphold secret must accompany the client ID.
	if custodial && c.Rewards.UpholdClientSecret == "" {
		ve.add("rewards.uphold_client_secret is required when rewards.uphold_client_id is set " +
			"(set GITLAB_ENHANCED_REWARDS_UPHOLD_CLIENT_SECRET)")
	}

	// Non-custodial path: wallet_address alone is not enough — the service
	// also needs an RPC endpoint and private key to sign transactions.
	// Without them every payout attempt will fail silently at runtime.
	if nonCustodial && !custodial {
		if c.Rewards.EthRPCURL == "" {
			ve.add("rewards.eth_rpc_url is required when using non-custodial ERC-20 payouts " +
				"(rewards.wallet_address is set but rewards.uphold_client_id is not) — " +
				"set GITLAB_ENHANCED_REWARDS_ETH_RPC_URL to an Ethereum JSON-RPC endpoint")
		}
		if c.Rewards.EthPrivateKey == "" {
			ve.add("rewards.eth_private_key is required when using non-custodial ERC-20 payouts — " +
				"set GITLAB_ENHANCED_REWARDS_ETH_PRIVATE_KEY (hex-encoded secp256k1 private key)")
		}
	}

	if c.Rewards.MinPayoutBAT < 0 {
		ve.add("rewards.min_payout_bat must be >= 0, got %g", c.Rewards.MinPayoutBAT)
	}
}

func (c *Config) validateBandwidth(ve *ValidationError) {
	if !c.Bandwidth.Enabled {
		return
	}
	if c.Bandwidth.CompressionLevel < 0 || c.Bandwidth.CompressionLevel > 9 {
		ve.add("bandwidth.compression_level must be 0-9, got %d", c.Bandwidth.CompressionLevel)
	}
	if c.Bandwidth.ArtifactMaxSizeMB < 0 {
		ve.add("bandwidth.artifact_max_size_mb must be >= 0, got %d", c.Bandwidth.ArtifactMaxSizeMB)
	}
	if c.Bandwidth.ArtifactRetentionDays < 0 {
		ve.add("bandwidth.artifact_retention_days must be >= 0, got %d", c.Bandwidth.ArtifactRetentionDays)
	}
}

// IsValidationError reports whether err is a *ValidationError.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
