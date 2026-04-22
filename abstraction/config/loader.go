// Package config provides layered configuration loading.
// Resolution order (later overrides earlier):
//  1. config/defaults.yaml  — local-first defaults, committed
//  2. config/local.yaml     — machine-specific overrides, gitignored
//  3. config/cloud.yaml     — cloud overlays, applied only when cloud.enabled=true
//  4. Environment variables — GITLAB_ENHANCED_* prefix
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure.
type Config struct {
	GitLab      GitLabConfig      `yaml:"gitlab"`
	Storage     StorageConfig     `yaml:"storage"`
	Build       BuildConfig       `yaml:"build"`
	Runner      RunnerConfig      `yaml:"runner"`
	LFS         LFSConfig         `yaml:"lfs"`
	IPFS        IPFSConfig        `yaml:"ipfs"`
	Environment EnvironmentConfig `yaml:"environment"`
	Cloud       CloudConfig       `yaml:"cloud"`
	Registry    RegistryConfig    `yaml:"registry"`
}

type GitLabConfig struct {
	Domain  string `yaml:"domain"`
	Edition string `yaml:"edition"` // ce | ee
}

type StorageConfig struct {
	// Backend selects the storage implementation.
	// Values: local | chain | ipfs | cloud
	// "cloud" delegates to Provider below.
	Backend string `yaml:"backend"`

	// Path is the root directory for the local backend.
	Path string `yaml:"path"`

	// Provider selects the cloud storage provider when backend=cloud.
	// Values: aws | gcs | azure | minio | ceph | r2
	// All S3-compatible providers (minio, ceph, r2) use the S3 protocol
	// with a custom Endpoint.
	Provider string `yaml:"provider"`

	// Bucket / Container name (all providers).
	Bucket string `yaml:"bucket"`

	// Region is required for aws, optional for others.
	Region string `yaml:"region"`

	// Endpoint overrides the default service URL.
	// Required for minio, ceph, r2. Leave empty for aws/gcs/azure.
	Endpoint string `yaml:"endpoint"`

	// Credentials holds provider-specific authentication.
	// Leave empty to use the provider's default credential chain
	// (env vars, instance metadata, workload identity, etc.).
	Credentials StorageCredentials `yaml:"credentials"`
}

// StorageCredentials holds optional explicit credentials.
// Prefer environment variables or instance metadata over these fields.
type StorageCredentials struct {
	// AWS / S3-compatible
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`

	// GCS: path to service account JSON key file
	// Leave empty to use Application Default Credentials (ADC)
	GCSKeyFile string `yaml:"gcs_key_file"`

	// Azure: connection string or account+key
	// Leave empty to use DefaultAzureCredential (env / managed identity)
	AzureConnectionString string `yaml:"azure_connection_string"`
	AzureAccountName      string `yaml:"azure_account_name"`
	AzureAccountKey       string `yaml:"azure_account_key"`
}

type BuildConfig struct {
	Backend   string `yaml:"backend"` // incus | depot
	Socket    string `yaml:"socket"`
	CachePool string `yaml:"cache_pool"`
	ProjectID string `yaml:"project_id"`
	Token     string `yaml:"token"`
}

type RunnerConfig struct {
	Backend    string `yaml:"backend"` // incus | blacksmith
	VMProfile  string `yaml:"vm_profile"`
	Concurrent int    `yaml:"concurrent"`
	Org        string `yaml:"org"`
	Token      string `yaml:"token"`
}

type LFSConfig struct {
	Server     string `yaml:"server"` // rudolfs | giftless | lfs-test-server
	Backend    string `yaml:"backend"`
	Path       string `yaml:"path"`
	Encryption bool   `yaml:"encryption"`
}

type IPFSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Node    string `yaml:"node"`
	Gateway string `yaml:"gateway"`
}

type EnvironmentConfig struct {
	Backend        string `yaml:"backend"` // incus | gitpod-k8s | ona
	WorkspaceImage string `yaml:"workspace_image"`
	IDE            string `yaml:"ide"`
	IDEPort        int    `yaml:"ide_port"`
	Network        string `yaml:"network"`
	Token          string `yaml:"token"`
}

type CloudConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"` // aws | gcp | azure
}

type RegistryConfig struct {
	Backend string `yaml:"backend"`
	URL     string `yaml:"url"`
}

// Load reads and merges configuration from the standard locations.
// root is the repository root directory.
func Load(root string) (*Config, error) {
	cfg := &Config{}

	layers := []string{
		filepath.Join(root, "config", "defaults.yaml"),
		filepath.Join(root, "config", "local.yaml"),
	}

	for _, path := range layers {
		if err := mergeFile(cfg, path); err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}
	}

	// Apply cloud overlay only when enabled
	if cfg.Cloud.Enabled {
		cloudPath := filepath.Join(root, "config", "cloud.yaml")
		if err := mergeFile(cfg, cloudPath); err != nil {
			return nil, fmt.Errorf("loading cloud config: %w", err)
		}
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// mergeFile reads a YAML file into cfg. Missing files are silently skipped.
func mergeFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Expand environment variables in values
	data = []byte(os.ExpandEnv(string(data)))
	return yaml.Unmarshal(data, cfg)
}

// applyEnvOverrides maps GITLAB_ENHANCED_* environment variables onto cfg.
// Format: GITLAB_ENHANCED_STORAGE_BACKEND → cfg.Storage.Backend
func applyEnvOverrides(cfg *Config) {
	overrides := map[string]*string{
		"GITLAB_ENHANCED_GITLAB_DOMAIN":        &cfg.GitLab.Domain,
		"GITLAB_ENHANCED_STORAGE_BACKEND":      &cfg.Storage.Backend,
		"GITLAB_ENHANCED_STORAGE_PATH":         &cfg.Storage.Path,
		"GITLAB_ENHANCED_BUILD_BACKEND":        &cfg.Build.Backend,
		"GITLAB_ENHANCED_RUNNER_BACKEND":       &cfg.Runner.Backend,
		"GITLAB_ENHANCED_LFS_SERVER":           &cfg.LFS.Server,
		"GITLAB_ENHANCED_LFS_BACKEND":          &cfg.LFS.Backend,
		"GITLAB_ENHANCED_ENVIRONMENT_BACKEND":  &cfg.Environment.Backend,
		"GITLAB_ENHANCED_CLOUD_PROVIDER":       &cfg.Cloud.Provider,
	}
	for env, field := range overrides {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
	// Boolean overrides
	if v := strings.ToLower(os.Getenv("GITLAB_ENHANCED_CLOUD_ENABLED")); v == "true" || v == "1" {
		cfg.Cloud.Enabled = true
	}
	if v := strings.ToLower(os.Getenv("GITLAB_ENHANCED_IPFS_ENABLED")); v == "true" || v == "1" {
		cfg.IPFS.Enabled = true
	}
}
