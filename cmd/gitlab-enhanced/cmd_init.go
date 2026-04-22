package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCmd(cfgRoot *string) *cobra.Command {
	var skipIncus bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise the local environment (prerequisites, Incus, config)",
		Long: `Checks prerequisites, initialises Incus networking and profiles,
and creates config/local.yaml from the example if it doesn't exist.

Safe to run multiple times — all steps are idempotent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(*cfgRoot, skipIncus)
		},
	}

	cmd.Flags().BoolVar(&skipIncus, "skip-incus", false,
		"skip Incus initialisation (useful in CI or when Incus is pre-configured)")

	return cmd
}

func runInit(root string, skipIncus bool) error {
	printSection("Checking prerequisites")
	if err := checkPrerequisites(); err != nil {
		return err
	}

	printSection("Config")
	if err := ensureLocalConfig(root); err != nil {
		return err
	}

	if !skipIncus {
		printSection("Incus network")
		if err := ensureIncusNetwork(); err != nil {
			return err
		}

		printSection("Incus profiles")
		if err := ensureIncusProfiles(root); err != nil {
			return err
		}
	}

	printSection("Subtrees")
	printInfo("Run ./scripts/subtree-add-all.sh to pull upstream sources")
	printInfo("(skipped during init — can take several minutes)")

	fmt.Println()
	printOK("Initialisation complete — run 'gitlab-enhanced up' to start the stack")
	return nil
}

// checkPrerequisites verifies all required binaries are present.
func checkPrerequisites() error {
	type req struct {
		bin     string
		install string
		warn    bool // warn only, don't fail
	}
	reqs := []req{
		{bin: "incus", install: "https://linuxcontainers.org/incus/docs/main/installing/"},
		{bin: "ansible-playbook", install: "pip install ansible"},
		{bin: "git", install: "https://git-scm.com"},
		{bin: "go", install: "https://go.dev/dl/"},
		{bin: "yq", install: "https://github.com/mikefarah/yq", warn: true},
		{bin: "kubectl", install: "https://kubernetes.io/docs/tasks/tools/", warn: true},
		{bin: "helm", install: "https://helm.sh/docs/intro/install/", warn: true},
	}

	allOK := true
	for _, r := range reqs {
		if err := checkBinary(r.bin); err != nil {
			if r.warn {
				printWarn(fmt.Sprintf("%s not found (optional) — install: %s", r.bin, r.install))
			} else {
				printFail(fmt.Sprintf("%s not found — install: %s", r.bin, r.install))
				allOK = false
			}
		} else {
			printOK(r.bin)
		}
	}

	if !isLinux() {
		printWarn("not running on Linux — Incus requires Linux; some features will be unavailable")
	}

	if !allOK {
		return fmt.Errorf("missing required prerequisites — install them and re-run 'gitlab-enhanced init'")
	}
	return nil
}

// ensureLocalConfig creates config/local.yaml from the example if absent.
func ensureLocalConfig(root string) error {
	localPath := filepath.Join(root, "config", "local.yaml")
	examplePath := filepath.Join(root, "config", "local.yaml.example")

	if fileExists(localPath) {
		printOK("config/local.yaml already exists")
		return nil
	}

	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("reading config/local.yaml.example: %w", err)
	}
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		return fmt.Errorf("writing config/local.yaml: %w", err)
	}
	printOK("created config/local.yaml from example")
	printInfo("review and customise config/local.yaml before running 'up'")
	return nil
}

// ensureIncusNetwork creates the gitlab-enhanced bridge network if absent.
func ensureIncusNetwork() error {
	const network = "gitlab-enhanced-br0"
	if incusNetworkExists(network) {
		printOK(fmt.Sprintf("network %q already exists", network))
		return nil
	}
	if err := runCmd("incus", "network", "create", network,
		"ipv4.address=10.200.0.1/24",
		"ipv4.nat=true",
		"ipv6.address=none",
	); err != nil {
		return fmt.Errorf("creating Incus network %q: %w", network, err)
	}
	printOK(fmt.Sprintf("created network %q (10.200.0.0/24, NAT enabled)", network))
	return nil
}

// ensureIncusProfiles applies all profile YAML files from runtime/incus/profiles/.
func ensureIncusProfiles(root string) error {
	profilesDir := filepath.Join(root, "runtime", "incus", "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return fmt.Errorf("reading profiles directory %s: %w", profilesDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		profileName := strings.TrimSuffix(entry.Name(), ".yaml")
		profilePath := filepath.Join(profilesDir, entry.Name())

		if incusProfileExists(profileName) {
			// Update existing profile
			if err := runCmd("incus", "profile", "edit", profileName,
				"<", profilePath); err != nil {
				// incus profile edit doesn't support < redirection via exec;
				// use shell explicitly
				if err2 := runCmd("sh", "-c",
					fmt.Sprintf("incus profile edit %s < %s", profileName, profilePath)); err2 != nil {
					printWarn(fmt.Sprintf("could not update profile %q: %v", profileName, err2))
					continue
				}
			}
			printOK(fmt.Sprintf("updated profile %q", profileName))
		} else {
			// Create new profile then apply YAML
			if err := runCmd("incus", "profile", "create", profileName); err != nil {
				return fmt.Errorf("creating profile %q: %w", profileName, err)
			}
			if err := runCmd("sh", "-c",
				fmt.Sprintf("incus profile edit %s < %s", profileName, profilePath)); err != nil {
				return fmt.Errorf("applying profile %q: %w", profileName, err)
			}
			printOK(fmt.Sprintf("created profile %q", profileName))
		}
	}
	return nil
}
