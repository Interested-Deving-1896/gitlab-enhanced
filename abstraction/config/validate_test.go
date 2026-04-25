package config_test

import (
	"strings"
	"testing"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/abstraction/config"
)

func validBase() *config.Config {
	cfg := &config.Config{}
	cfg.GitLab.Domain = "gitlab.local"
	cfg.GitLab.Edition = "ce"
	return cfg
}

func TestValidate_ValidMinimal(t *testing.T) {
	if err := validBase().Validate(); err != nil {
		t.Fatalf("expected no error for minimal valid config, got: %v", err)
	}
}

func TestValidate_MissingDomain(t *testing.T) {
	cfg := validBase()
	cfg.GitLab.Domain = ""
	assertValidationError(t, cfg, "gitlab.domain")
}

func TestValidate_BadEdition(t *testing.T) {
	cfg := validBase()
	cfg.GitLab.Edition = "enterprise"
	assertValidationError(t, cfg, "gitlab.edition")
}

func TestValidate_CloudBackendMissingProvider(t *testing.T) {
	cfg := validBase()
	cfg.Storage.Backend = "cloud"
	cfg.Storage.Bucket = "my-bucket"
	assertValidationError(t, cfg, "storage.provider")
}

func TestValidate_CloudBackendMissingBucket(t *testing.T) {
	cfg := validBase()
	cfg.Storage.Backend = "cloud"
	cfg.Storage.Provider = "aws"
	assertValidationError(t, cfg, "storage.bucket")
}

func TestValidate_MinioMissingEndpoint(t *testing.T) {
	cfg := validBase()
	cfg.Storage.Backend = "cloud"
	cfg.Storage.Provider = "minio"
	cfg.Storage.Bucket = "my-bucket"
	assertValidationError(t, cfg, "storage.endpoint")
}

func TestValidate_UnknownStorageBackend(t *testing.T) {
	cfg := validBase()
	cfg.Storage.Backend = "nfs"
	assertValidationError(t, cfg, "storage.backend")
}

func TestValidate_DepotMissingProjectID(t *testing.T) {
	cfg := validBase()
	cfg.Build.Backend = "depot"
	cfg.Build.Token = "tok"
	assertValidationError(t, cfg, "build.project_id")
}

func TestValidate_DepotMissingToken(t *testing.T) {
	cfg := validBase()
	cfg.Build.Backend = "depot"
	cfg.Build.ProjectID = "proj-123"
	assertValidationError(t, cfg, "build.token")
}

func TestValidate_BlacksmithMissingToken(t *testing.T) {
	cfg := validBase()
	cfg.Runner.Backend = "blacksmith"
	cfg.Runner.BlacksmithAPIURL = "https://api.blacksmith.sh"
	assertValidationError(t, cfg, "runner.token")
}

func TestValidate_BlacksmithMissingAPIURL(t *testing.T) {
	cfg := validBase()
	cfg.Runner.Backend = "blacksmith"
	cfg.Runner.Token = "tok"
	assertValidationError(t, cfg, "runner.blacksmith_api_url")
}

func TestValidate_LFSEncryptionMissingKey(t *testing.T) {
	cfg := validBase()
	cfg.LFS.Encryption = true
	cfg.LFS.EncryptionKey = ""
	assertValidationError(t, cfg, "lfs.encryption_key")
}

func TestValidate_LFSEncryptionWithKey(t *testing.T) {
	cfg := validBase()
	cfg.LFS.Encryption = true
	cfg.LFS.EncryptionKey = "supersecret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_CloudEnabledMissingProvider(t *testing.T) {
	cfg := validBase()
	cfg.Cloud.Enabled = true
	assertValidationError(t, cfg, "cloud.provider")
}

func TestValidate_CloudEnabledBadProvider(t *testing.T) {
	cfg := validBase()
	cfg.Cloud.Enabled = true
	cfg.Cloud.Provider = "digitalocean"
	assertValidationError(t, cfg, "cloud.provider")
}

func TestValidate_RewardsMissingWebhookSecret(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WalletAddress = "0xABC"
	assertValidationError(t, cfg, "rewards.webhook_secret")
}

func TestValidate_RewardsMissingWallet(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	assertValidationError(t, cfg, "rewards:")
}

func TestValidate_RewardsUpholdMissingSecret(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	cfg.Rewards.UpholdClientID = "client-id"
	// UpholdClientSecret intentionally missing
	assertValidationError(t, cfg, "rewards.uphold_client_secret")
}

func TestValidate_RewardsNonCustodialMissingRPCURL(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	cfg.Rewards.WalletAddress = "0xABC"
	// No UpholdClientID, no EthRPCURL — should fail
	assertValidationError(t, cfg, "eth_rpc_url")
}

func TestValidate_RewardsNonCustodialMissingPrivateKey(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	cfg.Rewards.WalletAddress = "0xABC"
	cfg.Rewards.EthRPCURL = "https://mainnet.infura.io/v3/key"
	// EthPrivateKey missing
	assertValidationError(t, cfg, "eth_private_key")
}

func TestValidate_RewardsNonCustodialValid(t *testing.T) {
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	cfg.Rewards.WalletAddress = "0xABC"
	cfg.Rewards.EthRPCURL = "https://mainnet.infura.io/v3/key"
	cfg.Rewards.EthPrivateKey = "deadbeef"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_RewardsValid(t *testing.T) {
	// Custodial path — wallet_address not required when Uphold is configured.
	cfg := validBase()
	cfg.Rewards.Enabled = true
	cfg.Rewards.WebhookSecret = "secret"
	cfg.Rewards.UpholdClientID = "client-id"
	cfg.Rewards.UpholdClientSecret = "client-secret"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_BandwidthBadCompressionLevel(t *testing.T) {
	cfg := validBase()
	cfg.Bandwidth.Enabled = true
	cfg.Bandwidth.CompressionLevel = 10
	assertValidationError(t, cfg, "bandwidth.compression_level")
}

func TestValidate_BandwidthNegativeRetention(t *testing.T) {
	cfg := validBase()
	cfg.Bandwidth.Enabled = true
	cfg.Bandwidth.ArtifactRetentionDays = -1
	assertValidationError(t, cfg, "bandwidth.artifact_retention_days")
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &config.Config{} // missing domain, bad storage, etc.
	cfg.Storage.Backend = "cloud"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !config.IsValidationError(err) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	// Should report multiple issues
	if !strings.Contains(err.Error(), "issue(s)") {
		t.Errorf("expected issue count in error message, got: %v", err)
	}
}

// assertValidationError fails the test if cfg.Validate() does not return an
// error containing the given substring.
func assertValidationError(t *testing.T, cfg *config.Config, wantSubstr string) {
	t.Helper()
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("expected error containing %q, got:\n%v", wantSubstr, err)
	}
}
