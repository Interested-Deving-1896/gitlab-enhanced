package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDownCmd(cfgRoot *string) *cobra.Command {
	var (
		withK8s bool
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop all local gitlab-enhanced services",
		Long: `Stops all running Incus containers and VMs managed by gitlab-enhanced.
Data volumes are preserved — run 'up' to restart.

Use --with-k8s to also tear down the K8s-in-Incus cluster.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDown(*cfgRoot, withK8s, force)
		},
	}

	cmd.Flags().BoolVar(&withK8s, "with-k8s", false, "also tear down the K8s-in-Incus cluster")
	cmd.Flags().BoolVar(&force, "force", false, "force-stop instances without graceful shutdown")

	return cmd
}

func runDown(root string, withK8s, force bool) error {
	services := []string{
		"gitlab-enhanced-gitlab",
		"gitlab-enhanced-lfs",
		"gitlab-enhanced-soft-serve",
		"gitlab-enhanced-buildkit",
	}

	// Stop host systemd services (adblock-proxy, bandwidth, rewards).
	// Best-effort — services may not be installed on all machines.
	for _, svc := range []string{"adblock-proxy", "gitlab-enhanced-bandwidth", "gitlab-enhanced-rewards"} {
		_ = runCmd("systemctl", "stop", svc)
	}

	printSection("Stopping services")
	for _, svc := range services {
		if !incusInstanceRunning(svc) {
			printInfo(fmt.Sprintf("%s not running", svc))
			continue
		}
		args := []string{"stop", svc}
		if force {
			args = append(args, "--force")
		}
		if err := runCmd("incus", args...); err != nil {
			printWarn(fmt.Sprintf("could not stop %s: %v", svc, err))
		} else {
			printOK(fmt.Sprintf("stopped %s", svc))
		}
	}

	if withK8s {
		printSection("Tearing down K8s cluster")
		k8sDir := filepath.Join(root, "runtime", "k8s-in-incus", "ansible")
		if err := runAnsible(k8sDir, "playbooks/k8s-cleanup.yml"); err != nil {
			printWarn(fmt.Sprintf("K8s cleanup had errors: %v", err))
		} else {
			printOK("K8s cluster removed")
		}
	}

	fmt.Println()
	printOK("Stack stopped — data volumes preserved")
	printInfo("Run 'gitlab-enhanced up' to restart")
	return nil
}
