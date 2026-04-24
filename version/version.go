// Package version provides build-time version information.
// Values are injected via -ldflags during build.
package version

import "fmt"

var (
	// Version is the semver release tag (e.g. "1.2.3").
	// Set via: -ldflags "-X gitlab.com/openos-project/git-management_deving/gitlab-enhanced/version.Version=1.2.3"
	Version = "dev"

	// Commit is the short git SHA of the build.
	Commit = "unknown"

	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
