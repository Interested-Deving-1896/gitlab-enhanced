package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd(cfgRoot *string) *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show health of all platform components",
		Long: `Checks each component of the gitlab-enhanced stack and reports its status.

Components checked:
  gitlab      — GitLab web UI (HTTP health endpoint)
  lfs         — Git LFS server
  soft-serve  — Soft Serve git server
  runner      — GitLab runner (Incus or Blacksmith)
  build       — BuildKit daemon (Incus or Depot)
  environment — Dev environment backend (Incus / gitpod-k8s / Ona)
  storage     — Storage backend (local path or cloud endpoint)
  ipfs        — IPFS node (when enabled)
  adblock     — adblock-proxy filter sidecar (when enabled)
  rewards     — BAT rewards service (when enabled)
  bandwidth   — Bandwidth proxy service (when enabled)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(*cfgRoot, jsonOut)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "output status as JSON")
	return cmd
}

// componentStatus holds the result of a single health check.
type componentStatus struct {
	Name    string
	Backend string
	OK      bool
	Detail  string
}

func runStatus(root string, jsonOut bool) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 4 * time.Second}

	checks := []componentStatus{
		checkHTTP(client, "gitlab", cfg.GitLab.Edition, gitlabHealthURLs(cfg.GitLab.Domain)),
		checkHTTP(client, "lfs", cfg.LFS.Server, []string{
			"http://localhost:8080/",
			"http://localhost:8080/healthz",
		}),
		checkHTTP(client, "soft-serve", "soft-serve", []string{
			"http://localhost:23231/",
		}),
		checkIncusInstance("runner", cfg.Runner.Backend, "gitlab-runner"),
		checkIncusInstance("build", cfg.Build.Backend, "buildkit"),
		checkEnvironmentBackend("environment", cfg.Environment.Backend),
		checkStorage(root, cfg.Storage.Backend, cfg.Storage.Path),
		checkIPFS(client, cfg.IPFS.Enabled, cfg.IPFS.Node),
		checkOptionalHTTP(client, "adblock", "adblock-proxy", cfg.Adblock.Enabled,
			adblockAddr(cfg.Adblock.ListenAddr)+"/health"),
		checkOptionalHTTP(client, "rewards", "bat-rewards", cfg.Rewards.Enabled,
			rewardsStatusAddr(cfg.Rewards.ListenAddr)+"/health"),
		checkOptionalHTTP(client, "bandwidth", "bw-proxy", cfg.Bandwidth.Enabled,
			bandwidthStatusAddr(cfg.Bandwidth.ListenAddr)+"/health"),
	}

	if jsonOut {
		printStatusJSON(checks)
		return nil
	}

	printStatusTable(checks)
	return nil
}

// gitlabHealthURLs returns candidate health endpoints for GitLab.
func gitlabHealthURLs(domain string) []string {
	if domain == "" {
		domain = "localhost"
	}
	return []string{
		fmt.Sprintf("https://%s/-/health", domain),
		fmt.Sprintf("http://%s/-/health", domain),
		fmt.Sprintf("http://%s:80/-/health", domain),
	}
}

// checkHTTP probes a list of URLs and returns the first reachable one.
func checkHTTP(client *http.Client, name, backend string, urls []string) componentStatus {
	for _, u := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := client.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return componentStatus{
					Name:    name,
					Backend: backend,
					OK:      true,
					Detail:  fmt.Sprintf("HTTP %d at %s", resp.StatusCode, u),
				}
			}
			return componentStatus{
				Name:    name,
				Backend: backend,
				OK:      false,
				Detail:  fmt.Sprintf("HTTP %d at %s", resp.StatusCode, u),
			}
		}
	}
	return componentStatus{
		Name:    name,
		Backend: backend,
		OK:      false,
		Detail:  "not reachable",
	}
}

// checkIncusInstance checks whether an Incus instance is running.
// Falls back to a "not applicable" status for non-Incus backends.
func checkIncusInstance(name, backend, instance string) componentStatus {
	if backend != "incus" && backend != "" {
		return componentStatus{
			Name:    name,
			Backend: backend,
			OK:      true,
			Detail:  fmt.Sprintf("managed externally (%s)", backend),
		}
	}
	if incusInstanceRunning(instance) {
		return componentStatus{
			Name:    name,
			Backend: "incus",
			OK:      true,
			Detail:  fmt.Sprintf("Incus instance %q running", instance),
		}
	}
	return componentStatus{
		Name:    name,
		Backend: "incus",
		OK:      false,
		Detail:  fmt.Sprintf("Incus instance %q not running", instance),
	}
}

// checkEnvironmentBackend checks the environment backend.
func checkEnvironmentBackend(name, backend string) componentStatus {
	switch backend {
	case "incus":
		// Check that the incus socket is accessible
		_, err := runCmdSilent("incus", "list", "--format", "csv")
		if err != nil {
			return componentStatus{Name: name, Backend: backend, OK: false, Detail: "incus not accessible"}
		}
		return componentStatus{Name: name, Backend: backend, OK: true, Detail: "incus accessible"}
	case "gitpod-k8s":
		// Check kubectl connectivity
		_, err := runCmdSilent("kubectl", "get", "nodes", "--no-headers")
		if err != nil {
			return componentStatus{Name: name, Backend: backend, OK: false, Detail: "kubectl: cluster not reachable"}
		}
		return componentStatus{Name: name, Backend: backend, OK: true, Detail: "k8s cluster reachable"}
	case "ona":
		return componentStatus{Name: name, Backend: backend, OK: true, Detail: "cloud-managed (Ona)"}
	default:
		return componentStatus{Name: name, Backend: backend, OK: false, Detail: "unknown backend"}
	}
}

// checkStorage verifies the storage backend is accessible.
func checkStorage(root, backend, path string) componentStatus {
	switch backend {
	case "local", "":
		if path == "" {
			path = root + "/data/storage"
		}
		if fileExists(path) {
			return componentStatus{Name: "storage", Backend: "local", OK: true, Detail: path}
		}
		return componentStatus{Name: "storage", Backend: "local", OK: false, Detail: fmt.Sprintf("path not found: %s", path)}
	case "ipfs":
		return componentStatus{Name: "storage", Backend: "ipfs", OK: true, Detail: "delegated to IPFS check"}
	case "cloud":
		return componentStatus{Name: "storage", Backend: "cloud", OK: true, Detail: "cloud-managed (not checked locally)"}
	case "chain":
		return componentStatus{Name: "storage", Backend: "chain", OK: true, Detail: "chain backend (local + fallback)"}
	default:
		return componentStatus{Name: "storage", Backend: backend, OK: false, Detail: "unknown backend"}
	}
}

// checkIPFS probes the IPFS node API.
func checkIPFS(client *http.Client, enabled bool, node string) componentStatus {
	if !enabled {
		return componentStatus{Name: "ipfs", Backend: "kubo", OK: true, Detail: "disabled"}
	}
	if node == "" {
		node = "http://localhost:5001"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, node+"/api/v0/id", nil)
	resp, err := client.Do(req)
	if err != nil {
		return componentStatus{Name: "ipfs", Backend: "kubo", OK: false, Detail: fmt.Sprintf("node not reachable at %s", node)}
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return componentStatus{Name: "ipfs", Backend: "kubo", OK: true, Detail: fmt.Sprintf("node at %s", node)}
	}
	return componentStatus{Name: "ipfs", Backend: "kubo", OK: false, Detail: fmt.Sprintf("HTTP %d from %s", resp.StatusCode, node)}
}

// printStatusTable renders the status as a human-readable table.
func printStatusTable(checks []componentStatus) {
	fmt.Println()
	fmt.Printf("  %-14s  %-16s  %-8s  %s\n", "COMPONENT", "BACKEND", "STATUS", "DETAIL")
	fmt.Printf("  %s\n", strings.Repeat("─", 72))

	allOK := true
	for _, c := range checks {
		status := "\033[32m ok \033[0m"
		if !c.OK {
			status = "\033[31mfail\033[0m"
			allOK = false
		}
		backend := c.Backend
		if backend == "" {
			backend = "—"
		}
		fmt.Printf("  %-14s  %-16s  %s  %s\n", c.Name, backend, status, c.Detail)
	}

	fmt.Printf("  %s\n", strings.Repeat("─", 72))
	if allOK {
		printOK("all components healthy")
	} else {
		printWarn("some components are not running — use 'gitlab-enhanced up' to start them")
	}
	fmt.Println()
}

// checkOptionalHTTP checks an optional service that may be disabled.
// When disabled it reports "disabled" rather than a failure.
func checkOptionalHTTP(client *http.Client, name, backend string, enabled bool, url string) componentStatus {
	if !enabled {
		return componentStatus{Name: name, Backend: backend, OK: true, Detail: "disabled"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return componentStatus{Name: name, Backend: backend, OK: false, Detail: fmt.Sprintf("not reachable at %s", url)}
	}
	resp.Body.Close()
	if resp.StatusCode < 500 {
		return componentStatus{Name: name, Backend: backend, OK: true, Detail: fmt.Sprintf("HTTP %d at %s", resp.StatusCode, url)}
	}
	return componentStatus{Name: name, Backend: backend, OK: false, Detail: fmt.Sprintf("HTTP %d at %s", resp.StatusCode, url)}
}

func adblockAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:6060"
	}
	return addr
}

func rewardsStatusAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:6061"
	}
	return addr
}

func bandwidthStatusAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:6062"
	}
	return addr
}

// printStatusJSON renders the status as JSON (no external dependency).
func printStatusJSON(checks []componentStatus) {
	fmt.Println("[")
	for i, c := range checks {
		comma := ","
		if i == len(checks)-1 {
			comma = ""
		}
		ok := "false"
		if c.OK {
			ok = "true"
		}
		detail := strings.ReplaceAll(c.Detail, `"`, `\"`)
		backend := strings.ReplaceAll(c.Backend, `"`, `\"`)
		fmt.Printf("  {\"component\":%q,\"backend\":%q,\"ok\":%s,\"detail\":%q}%s\n",
			c.Name, backend, ok, detail, comma)
	}
	fmt.Println("]")
}
