package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

func newUpCmd(cfgRoot *string) *cobra.Command {
	var (
		withK8s    bool
		withGitpod bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the full local gitlab-enhanced stack",
		Long: `Brings up all local services using Incus and Ansible:

  1. GitLab (omnibus) — packaged GitLab CE/EE in an Incus container
  2. LFS server (rudolfs) — Git LFS backend in an Incus container
  3. soft-serve — SSH-first git server in an Incus container
  4. GitLab Runner — CI runner using Incus VM executor

Optional (--with-k8s):
  5. Kubernetes cluster — K8s-in-Incus (control plane + workers)

Optional (--with-gitpod, requires --with-k8s):
  6. Gitpod Classic — full dev environment platform on the K8s cluster`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runUp(ctx, *cfgRoot, withK8s, withGitpod, dryRun)
		},
	}

	cmd.Flags().BoolVar(&withK8s, "with-k8s", false,
		"provision K8s-in-Incus cluster (required for Gitpod Classic)")
	cmd.Flags().BoolVar(&withGitpod, "with-gitpod", false,
		"install Gitpod Classic on the K8s cluster (implies --with-k8s)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be done without executing")

	return cmd
}

func runUp(ctx context.Context, root string, withK8s, withGitpod, dryRun bool) error {
	if withGitpod {
		withK8s = true
	}

	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	printSection("Pre-flight checks")
	if err := checkBinary("incus"); err != nil {
		return fmt.Errorf("incus is required: %w", err)
	}
	if err := checkBinary("ansible-playbook"); err != nil {
		return fmt.Errorf("ansible-playbook is required: %w", err)
	}
	if !incusNetworkExists("gitlab-enhanced-br0") {
		return fmt.Errorf("Incus network 'gitlab-enhanced-br0' not found — run 'gitlab-enhanced init' first")
	}
	printOK("prerequisites satisfied")

	ansibleDir := filepath.Join(root, "deploy", "ansible")

	// --- GitLab (omnibus) ---
	printSection("GitLab")
	if dryRun {
		printInfo(fmt.Sprintf("would run: ansible-playbook %s/gitlab.yml", ansibleDir))
	} else {
		if err := runAnsible(ctx, ansibleDir, "gitlab.yml",
			"-e", fmt.Sprintf("gitlab_domain=%s", cfg.GitLab.Domain),
			"-e", fmt.Sprintf("gitlab_edition=%s", cfg.GitLab.Edition),
		); err != nil {
			return fmt.Errorf("starting GitLab: %w", err)
		}
	}
	printOK(fmt.Sprintf("GitLab available at https://%s", cfg.GitLab.Domain))

	// --- LFS server ---
	printSection("LFS server")
	if dryRun {
		printInfo(fmt.Sprintf("would run: ansible-playbook %s/lfs.yml", ansibleDir))
	} else {
		if err := runAnsible(ctx, ansibleDir, "lfs.yml",
			"-e", fmt.Sprintf("lfs_server=%s", cfg.LFS.Server),
			"-e", fmt.Sprintf("lfs_backend=%s", cfg.LFS.Backend),
			"-e", fmt.Sprintf("lfs_path=%s", cfg.LFS.Path),
		); err != nil {
			return fmt.Errorf("starting LFS server: %w", err)
		}
	}
	printOK(fmt.Sprintf("LFS server (%s) running", cfg.LFS.Server))

	// --- soft-serve ---
	printSection("soft-serve (SSH git server)")
	if dryRun {
		printInfo(fmt.Sprintf("would run: ansible-playbook %s/soft-serve.yml", ansibleDir))
	} else {
		if err := runAnsible(ctx, ansibleDir, "soft-serve.yml"); err != nil {
			// Non-fatal — soft-serve is optional
			printWarn(fmt.Sprintf("soft-serve failed to start: %v", err))
		}
	}

	// --- GitLab Runner ---
	printSection("GitLab Runner (Incus executor)")
	if dryRun {
		printInfo(fmt.Sprintf("would run: ansible-playbook %s/runner.yml", ansibleDir))
	} else {
		if err := runAnsible(ctx, ansibleDir, "runner.yml",
			"-e", fmt.Sprintf("gitlab_url=https://%s", cfg.GitLab.Domain),
		); err != nil {
			printWarn(fmt.Sprintf("runner setup failed (register manually): %v", err))
		}
	}

	// --- K8s cluster (optional) ---
	if withK8s {
		printSection("Kubernetes cluster (K8s-in-Incus)")
		k8sPlaybookDir := filepath.Join(root, "runtime", "k8s-in-incus", "ansible")
		if dryRun {
			printInfo(fmt.Sprintf("would run: ansible-playbook %s/playbooks/k8s-cluster.yml", k8sPlaybookDir))
		} else {
			if err := runAnsible(ctx, k8sPlaybookDir, "playbooks/k8s-cluster.yml"); err != nil {
				return fmt.Errorf("provisioning K8s cluster: %w", err)
			}
		}
		printOK("K8s cluster running — kubeconfig: runtime/k8s-in-incus/ansible/kubeconfig.yaml")
	}

	// --- Gitpod Classic (optional) ---
	if withGitpod {
		printSection("Gitpod Classic")
		k8sPlaybookDir := filepath.Join(root, "runtime", "k8s-in-incus", "ansible")
		if dryRun {
			printInfo(fmt.Sprintf("would run: ansible-playbook %s/playbooks/k8s-gitpod.yml", k8sPlaybookDir))
		} else {
			if err := runAnsible(ctx, k8sPlaybookDir, "playbooks/k8s-gitpod.yml",
				"-e", fmt.Sprintf("gitpod_domain=gitpod.%s", cfg.GitLab.Domain),
			); err != nil {
				return fmt.Errorf("installing Gitpod Classic: %w", err)
			}
		}
		printOK(fmt.Sprintf("Gitpod Classic available at https://gitpod.%s", cfg.GitLab.Domain))
	}

	// --- adblock-proxy ---
	if cfg.Adblock.Enabled {
		printSection("adblock-proxy (network filter sidecar)")
		if dryRun {
			printInfo(fmt.Sprintf("would run: ansible-playbook %s/adblock.yml", ansibleDir))
		} else {
			if err := runAnsible(ctx, ansibleDir, "adblock.yml",
				"-e", fmt.Sprintf("adblock_listen=%s", cfg.Adblock.ListenAddr),
				"-e", fmt.Sprintf("adblock_lists_dir=%s", cfg.Adblock.ListsDir),
			); err != nil {
				printWarn(fmt.Sprintf("adblock-proxy setup failed: %v", err))
			} else {
				printOK(fmt.Sprintf("adblock-proxy running at http://%s", cfg.Adblock.ListenAddr))
			}
		}
	}

	// --- bandwidth proxy ---
	if cfg.Bandwidth.Enabled {
		printSection("bandwidth proxy (compression + LFS dedup + artifact policies)")
		printInfo("Start manually: gitlab-enhanced bandwidth serve")
		printInfo(fmt.Sprintf("Listen: http://%s", cfg.Bandwidth.ListenAddr))
	}

	// --- rewards service ---
	if cfg.Rewards.Enabled {
		printSection("BAT rewards service (opt-in)")
		printInfo("Start manually: gitlab-enhanced rewards serve")
		printInfo(fmt.Sprintf("Listen: http://%s", cfg.Rewards.ListenAddr))
		printInfo("Configure GitLab system hook → http://" + cfg.Rewards.ListenAddr + "/webhook/gitlab")
	}

	fmt.Println()
	printOK("Stack is up")
	printInfo(fmt.Sprintf("GitLab:  https://%s", cfg.GitLab.Domain))
	printInfo("Run 'gitlab-enhanced status' to check component health")
	return nil
}

// runAnsible runs an ansible-playbook from the given directory.
// The playbook process receives SIGTERM when ctx is cancelled (SIGINT/SIGTERM
// from the user), giving Ansible a chance to finish the current task cleanly.
func runAnsible(ctx context.Context, dir, playbook string, extraArgs ...string) error {
	args := append([]string{playbook}, extraArgs...)
	return runCmdContext(ctx, "ansible-playbook", args...)
}
