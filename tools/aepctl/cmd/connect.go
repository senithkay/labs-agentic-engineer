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
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var connectServerURL string

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Set the AEP server URL",
	Long: `Saves the AEP management server URL to ~/.aep/config.yaml.

All AEP configuration is stored in that file. Edit it directly to customise
Thunder endpoints or configure OpenBao KMS auto-unseal for production:

  thunder:
    namespace: thunder
    release: thunder
    url: http://thunder-service.thunder.svc.cluster.local:8090
    config_map: thunder-config-map
    deployment: thunder-deployment
    admin_client_id: openchoreo-system-app
    admin_client_secret: openchoreo-system-app-secret
    public_url: http://thunder.openchoreo.localhost:8080

  # Production: configure one KMS provider for automatic OpenBao unsealing.
  # Without this, OpenBao uses Shamir and requires manual unsealing after restarts.
  openbao:
    seal:
      type: awskms           # or gcpckms / azurekeyvault
      awskms:
        region: us-east-1
        kms_key_id: arn:aws:kms:us-east-1:123456789012:key/mrk-...
      # gcpckms:
      #   project: my-project
      #   region: global
      #   key_ring: my-key-ring
      #   crypto_key: my-crypto-key
      # azurekeyvault:
      #   vault_name: my-key-vault
      #   key_name: my-key`,
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
	connectCmd.Flags().StringVar(&connectServerURL, "server", "", "AEP server gRPC URL (required)")
	_ = connectCmd.MarkFlagRequired("server")
}

func runConnect(cmd *cobra.Command, args []string) error {
	viper.Set("server", connectServerURL)

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	cfgDir := filepath.Join(home, ".aep")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	cfgFile := filepath.Join(cfgDir, "config.yaml")
	if err := viper.WriteConfigAs(cfgFile); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "Server:  %s\n", connectServerURL)
	_, _ = fmt.Fprintf(os.Stdout, "Config:  %s\n", cfgFile)
	_, _ = fmt.Fprintln(os.Stdout, "\nTo customise Thunder endpoints edit the config file directly.")
	return nil
}
