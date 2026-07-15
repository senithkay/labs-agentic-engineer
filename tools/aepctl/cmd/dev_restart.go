// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var devRestartCmd = &cobra.Command{
	Use:   "restart <service|all>",
	Short: "Rollout-restart a service without rebuilding its image",
	Long: `Triggers a rolling restart of one service (or all) using the currently
deployed image — no rebuild. Useful after a ConfigMap/Secret change.

  aep dev restart aep-api
  aep dev restart all`,
	Args: cobra.ExactArgs(1),
	RunE: runDevRestart,
}

func init() {
	devCmd.AddCommand(devRestartCmd)
}

func runDevRestart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if !viper.GetBool("dev.enabled") {
		return fmt.Errorf("dev mode is disabled — enable it with `aep dev enable`")
	}
	services, err := resolveDevServices(args[0])
	if err != nil {
		return err
	}
	client, err := k8s.NewClient("")
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	ns := viper.GetString("dev.namespace")

	for _, s := range services {
		if err := k8s.RolloutRestart(ctx, client, ns, s.Deployment); err != nil {
			return fmt.Errorf("restart %s: %w", s.Name, err)
		}
		fmt.Fprintf(os.Stdout, "→ restarting deployment/%s...\n", s.Deployment)
		if err := waitForRolloutComplete(ctx, client, ns, s.Deployment, 5*time.Minute); err != nil {
			return fmt.Errorf("restart %s: %w", s.Name, err)
		}
		fmt.Fprintf(os.Stdout, "✅ %s restarted\n", s.Name)
	}
	return nil
}
