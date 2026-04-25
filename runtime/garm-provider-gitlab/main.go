// SPDX-License-Identifier: Apache-2.0
//
// garm-provider-gitlab — GARM external provider for ephemeral GitLab CI runners.
//
// GARM calls this binary as a subprocess, passing the command via the
// GARM_COMMAND environment variable and configuration via GARM_PROVIDER_CONFIG_FILE.
//
// See runtime/garm-provider-gitlab/README.md for setup instructions.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudbase/garm-provider-common/execution"
	commonExecution "github.com/cloudbase/garm-provider-common/execution/common"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/runtime/garm-provider-gitlab/provider"
)

var Version = "v0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := execution.GetEnvironment()
	if err != nil {
		log.Fatalf("garm-provider-gitlab: get environment: %v", err)
	}

	prov, err := provider.New(env.ProviderConfigFile, env.ControllerID)
	if err != nil {
		log.Fatalf("garm-provider-gitlab: init provider: %v", err)
	}

	result, err := env.Run(ctx, prov)
	if err != nil {
		fmt.Fprintf(os.Stderr, "garm-provider-gitlab: run command: %v\n", err)
		os.Exit(commonExecution.ResolveErrorToExitCode(err))
	}
	if len(result) > 0 {
		fmt.Fprint(os.Stdout, result)
	}
}
