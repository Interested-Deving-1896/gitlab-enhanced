package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/rewards"
)

func newRewardsCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rewards",
		Short: "Manage BAT contributor rewards (opt-in)",
		Long: `Manage the opt-in BAT (Basic Attention Token) rewards service.

The rewards service is DISABLED by default. Enable it in config/local.yaml:

  rewards:
    enabled: true
    publisher_id: "your-brave-publisher-id"
    wallet_address: "0xYourERC20WalletAddress"

Contributors register their wallet address once, then automatically receive
BAT tips for merged MRs, closed issues, and successful CI pipelines.`,
	}

	cmd.AddCommand(
		newRewardsServeCmd(cfgRoot),
		newRewardsStatusCmd(cfgRoot),
		newRewardsPendingCmd(cfgRoot),
		newRewardsPayoutCmd(cfgRoot),
		newRewardsRatesCmd(cfgRoot),
		newRewardsWalletCmd(cfgRoot),
	)
	return cmd
}

// rewards serve — start the rewards HTTP service
func newRewardsServeCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the rewards service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			if !cfg.Rewards.Enabled {
				return fmt.Errorf("rewards service is disabled — set rewards.enabled: true in config/local.yaml")
			}

			svc, err := rewards.New(rewards.Config{
				Enabled:            cfg.Rewards.Enabled,
				PublisherID:        cfg.Rewards.PublisherID,
				WalletAddress:      cfg.Rewards.WalletAddress,
				UpholdClientID:     cfg.Rewards.UpholdClientID,
				UpholdClientSecret: cfg.Rewards.UpholdClientSecret,
				MinPayoutBAT:       cfg.Rewards.MinPayoutBAT,
				ListenAddr:         cfg.Rewards.ListenAddr,
			})
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			printSection("Starting rewards service")
			printInfo(fmt.Sprintf("Publisher ID: %s", cfg.Rewards.PublisherID))
			printInfo(fmt.Sprintf("Wallet:       %s", cfg.Rewards.WalletAddress))
			printInfo(fmt.Sprintf("Listen:       http://%s", cfg.Rewards.ListenAddr))
			fmt.Println()
			printInfo("Configure GitLab system hook → http://" + cfg.Rewards.ListenAddr + "/webhook/gitlab")

			return svc.Start(ctx)
		},
	}
}

// rewards status — check if the service is running
func newRewardsStatusCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check rewards service health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			if !cfg.Rewards.Enabled {
				printWarn("rewards service is disabled (rewards.enabled: false)")
				return nil
			}
			addr := cfg.Rewards.ListenAddr
			if addr == "" {
				addr = "127.0.0.1:6061"
			}
			resp, err := httpGet("http://" + addr + "/health")
			if err != nil {
				printWarn(fmt.Sprintf("rewards service not reachable at %s: %v", addr, err))
				return nil
			}
			printOK(fmt.Sprintf("rewards service healthy at http://%s", addr))
			fmt.Println(resp)
			return nil
		},
	}
}

// rewards pending — list pending rewards
func newRewardsPendingCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pending",
		Short: "List pending BAT rewards",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			addr := rewardsAddr(cfg.Rewards.ListenAddr)
			body, err := httpGet("http://" + addr + "/rewards/pending")
			if err != nil {
				return fmt.Errorf("rewards service not reachable: %w", err)
			}
			fmt.Println(body)
			return nil
		},
	}
}

// rewards payout — trigger payout of pending rewards
func newRewardsPayoutCmd(cfgRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "payout",
		Short: "Trigger payout of pending rewards",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			addr := rewardsAddr(cfg.Rewards.ListenAddr)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post("http://"+addr+"/rewards/payout", "application/json", nil)
			if err != nil {
				return fmt.Errorf("rewards service not reachable: %w", err)
			}
			defer resp.Body.Close()
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
}

// rewards rates — show or update reward rates
func newRewardsRatesCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rates",
		Short: "Show current BAT reward rates per event type",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			addr := rewardsAddr(cfg.Rewards.ListenAddr)
			body, err := httpGet("http://" + addr + "/rewards/rates")
			if err != nil {
				return fmt.Errorf("rewards service not reachable: %w", err)
			}
			fmt.Println(body)
			return nil
		},
	}
	return cmd
}

// rewards wallet — register or look up a contributor wallet
func newRewardsWalletCmd(cfgRoot *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: "Register or look up a contributor BAT wallet",
	}

	var username, walletAddr string

	registerCmd := &cobra.Command{
		Use:   "register",
		Short: "Register a BAT wallet address for a GitLab username",
		RunE: func(cmd *cobra.Command, args []string) error {
			if username == "" || walletAddr == "" {
				return fmt.Errorf("--username and --wallet are required")
			}
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			addr := rewardsAddr(cfg.Rewards.ListenAddr)
			payload := fmt.Sprintf(`{"username":%q,"wallet_address":%q}`, username, walletAddr)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post("http://"+addr+"/wallet/register",
				"application/json", stringReader(payload))
			if err != nil {
				return fmt.Errorf("rewards service not reachable: %w", err)
			}
			defer resp.Body.Close()
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return err
			}
			out, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(out))
			return nil
		},
	}
	registerCmd.Flags().StringVar(&username, "username", "", "GitLab username")
	registerCmd.Flags().StringVar(&walletAddr, "wallet", "", "ERC-20 wallet address (0x...)")

	getCmd := &cobra.Command{
		Use:   "get <username>",
		Short: "Look up the registered wallet for a GitLab username",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*cfgRoot)
			if err != nil {
				return err
			}
			addr := rewardsAddr(cfg.Rewards.ListenAddr)
			body, err := httpGet("http://" + addr + "/wallet/" + args[0])
			if err != nil {
				return fmt.Errorf("rewards service not reachable: %w", err)
			}
			fmt.Println(body)
			return nil
		},
	}

	cmd.AddCommand(registerCmd, getCmd)
	return cmd
}

func rewardsAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:6061"
	}
	return addr
}
