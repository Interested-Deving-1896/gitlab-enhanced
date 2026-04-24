// Package abstraction defines the interface and registry for Git LFS server
// backends. The pattern mirrors abstraction/storage and abstraction/environment.
package abstraction

import (
	"context"
	"fmt"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

// Server is a running LFS server that can be started and stopped.
type Server interface {
	// Name returns the backend identifier (rudolfs | giftless | lfs-test-server).
	Name() string
	// Start launches the server process and blocks until it is ready to serve.
	// Implementations should return when the server exits or ctx is cancelled.
	Start(ctx context.Context, cfg Config) error
	// Stop gracefully shuts down the server.
	Stop(ctx context.Context) error
	// URL returns the base URL the server is listening on.
	URL() string
}

// Config holds the common configuration for all LFS server backends.
type Config struct {
	// ListenAddr is the address to bind (default: 127.0.0.1:8080).
	ListenAddr string
	// StoragePath is the local directory for object storage.
	StoragePath string
	// S3Bucket is the S3 bucket name for cloud-backed storage.
	S3Bucket string
	// S3Region is the AWS region for S3-backed storage.
	S3Region string
	// AuthToken is the shared secret for LFS authentication (optional).
	AuthToken string
	// EncryptionKey is the key used for at-rest encryption (rudolfs only).
	EncryptionKey string
}

// FromConfig returns the appropriate LFS Server from configuration.
func FromConfig(cfg *config.Config) (Server, error) {
	lc := Config{
		ListenAddr:    "127.0.0.1:8080",
		StoragePath:   cfg.LFS.Path,
		EncryptionKey: cfg.LFS.EncryptionKey,
	}

	switch cfg.LFS.Server {
	case "rudolfs", "":
		return &ExecServer{
			backend:    "rudolfs",
			listenAddr: lc.ListenAddr,
			args:       rudolfsArgs(lc),
		}, nil
	case "giftless":
		return &ExecServer{
			backend:    "giftless",
			listenAddr: lc.ListenAddr,
			args:       giftlessArgs(lc),
		}, nil
	case "lfs-test-server":
		return &ExecServer{
			backend:    "lfs-test-server",
			listenAddr: lc.ListenAddr,
			args:       lfsTestServerArgs(lc),
		}, nil
	default:
		return nil, fmt.Errorf("unknown LFS server backend: %q", cfg.LFS.Server)
	}
}

func rudolfsArgs(cfg Config) []string {
	args := []string{"--listen", cfg.ListenAddr, "--storage", cfg.StoragePath}
	if cfg.EncryptionKey != "" {
		args = append(args, "--key", cfg.EncryptionKey)
	}
	return args
}

func giftlessArgs(cfg Config) []string {
	return []string{"--host", cfg.ListenAddr, "--storage-path", cfg.StoragePath}
}

func lfsTestServerArgs(cfg Config) []string {
	return []string{"-l", cfg.ListenAddr, "-dir", cfg.StoragePath}
}
