// Package storage — registry builds the active backend chain from config.
package storage

import (
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// FromConfig constructs the appropriate Backend from configuration.
// The chain is always local-first; cloud backends are appended only when enabled.
func FromConfig(cfg *config.Config) (Backend, error) {
	switch cfg.Storage.Backend {
	case "local", "":
		if cfg.Storage.Path == "" {
			return nil, fmt.Errorf("storage.path must be set when backend is 'local'")
		}
		return NewLocalBackend(cfg.Storage.Path), nil

	case "chain":
		// Local → IPFS → S3 fallback chain
		chain := Chain{NewLocalBackend(cfg.Storage.Path)}
		if cfg.IPFS.Enabled {
			chain = append(chain, NewIPFSBackend(cfg.IPFS.Node))
		}
		if cfg.Cloud.Enabled && cfg.Storage.Bucket != "" {
			chain = append(chain, NewS3Backend(cfg.Storage.Bucket, cfg.Storage.Region))
		}
		return chain, nil

	case "ipfs":
		if !cfg.IPFS.Enabled {
			return nil, fmt.Errorf("storage.backend is 'ipfs' but ipfs.enabled is false")
		}
		return NewIPFSBackend(cfg.IPFS.Node), nil

	case "s3":
		if !cfg.Cloud.Enabled {
			return nil, fmt.Errorf("storage.backend is 's3' but cloud.enabled is false")
		}
		return NewS3Backend(cfg.Storage.Bucket, cfg.Storage.Region), nil

	default:
		return nil, fmt.Errorf("unknown storage backend: %q", cfg.Storage.Backend)
	}
}
