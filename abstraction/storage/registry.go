// Package storage — registry builds the active backend from config.
package storage

import (
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// FromConfig constructs the appropriate Backend from configuration.
//
// Backend selection (config.Storage.Backend):
//
//	local          — local filesystem (default, no external deps)
//	chain          — local → IPFS → cloud fallback (cloud only if enabled)
//	ipfs           — IPFS node via Kubo HTTP API
//	cloud          — cloud provider selected by config.Storage.Provider
//
// Cloud provider selection (config.Storage.Provider, only when backend=cloud):
//
//	aws            — AWS S3 (default credential chain: env, ~/.aws, EC2 metadata)
//	gcs            — Google Cloud Storage (ADC: env, gcloud, GCE metadata)
//	azure          — Azure Blob Storage (DefaultAzureCredential)
//	minio          — MinIO (S3-compatible, requires Endpoint)
//	ceph           — Ceph RGW (S3-compatible, requires Endpoint)
//	r2             — Cloudflare R2 (S3-compatible, requires Endpoint)
func FromConfig(cfg *config.Config) (Backend, error) {
	switch cfg.Storage.Backend {

	case "local", "":
		if cfg.Storage.Path == "" {
			return nil, fmt.Errorf("storage.path must be set when backend is 'local'")
		}
		return NewLocalBackend(cfg.Storage.Path), nil

	case "ipfs":
		if !cfg.IPFS.Enabled {
			return nil, fmt.Errorf("storage.backend is 'ipfs' but ipfs.enabled is false")
		}
		return NewIPFSBackend(cfg.IPFS.Node), nil

	case "cloud":
		if !cfg.Cloud.Enabled {
			return nil, fmt.Errorf("storage.backend is 'cloud' but cloud.enabled is false")
		}
		return cloudBackendFromConfig(cfg)

	case "chain":
		return buildChain(cfg)

	default:
		return nil, fmt.Errorf("unknown storage backend %q — supported: local, ipfs, cloud, chain", cfg.Storage.Backend)
	}
}

// cloudBackendFromConfig builds a CloudBackend from the storage config section.
func cloudBackendFromConfig(cfg *config.Config) (*CloudBackend, error) {
	s := cfg.Storage
	if s.Provider == "" {
		return nil, fmt.Errorf("storage.provider must be set when backend is 'cloud' (aws|gcs|azure|minio|ceph|r2)")
	}
	if s.Bucket == "" {
		return nil, fmt.Errorf("storage.bucket must be set when backend is 'cloud'")
	}
	return NewCloudBackend(CloudOptions{
		Provider:              s.Provider,
		Bucket:                s.Bucket,
		Region:                s.Region,
		Endpoint:              s.Endpoint,
		AccessKeyID:           s.Credentials.AccessKeyID,
		SecretAccessKey:       s.Credentials.SecretAccessKey,
		GCSKeyFile:            s.Credentials.GCSKeyFile,
		AzureConnectionString: s.Credentials.AzureConnectionString,
		AzureAccountName:      s.Credentials.AzureAccountName,
		AzureAccountKey:       s.Credentials.AzureAccountKey,
	})
}

// buildChain constructs a local → IPFS → cloud fallback chain.
// Only backends that are configured and enabled are included.
func buildChain(cfg *config.Config) (Chain, error) {
	var chain Chain

	// 1. Local (always first if path is set)
	if cfg.Storage.Path != "" {
		chain = append(chain, NewLocalBackend(cfg.Storage.Path))
	}

	// 2. IPFS (if enabled)
	if cfg.IPFS.Enabled {
		chain = append(chain, NewIPFSBackend(cfg.IPFS.Node))
	}

	// 3. Cloud (if enabled and provider is configured)
	if cfg.Cloud.Enabled && cfg.Storage.Provider != "" && cfg.Storage.Bucket != "" {
		cb, err := cloudBackendFromConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("chain: building cloud backend: %w", err)
		}
		chain = append(chain, cb)
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("chain: no backends configured — set storage.path, ipfs.enabled, or cloud.enabled")
	}
	return chain, nil
}
