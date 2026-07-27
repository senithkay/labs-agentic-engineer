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
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aepctl/internal/config"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the in-cluster AEP configuration",
}

var configImportFile string

var configImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Apply a local config file to the in-cluster aep-cli-config ConfigMap",
	Long: `Reads a local YAML config file and upserts the aep-cli-config ConfigMap
in the wso2-aep namespace. Use this to set or update AEP configuration
without re-running aep platform install.

Only keys present in the file are written; existing keys not in the file
are left untouched. thunder.admin_client_secret is intentionally ignored
— it is managed by OpenBao/ESO and never stored in the ConfigMap.

Example config file:

  server: http://aep-server.openchoreo.localhost:8080

  thunder:
    url: http://thunder-service.thunder.svc.cluster.local:8090
    public_url: https://thunder.example.com
    admin_client_id: openchoreo-system-app
    namespace: thunder
    config_map: thunder-config-map
    deployment: thunder-deployment

  oc:
    api_url: http://openchoreo-api.openchoreo-control-plane.svc.cluster.local:8080
    org_namespace: default
    local_org_provisioning:
      enabled: false

  platform:
    workspaces:
      access_mode: ReadWriteMany

  codingagent:
    local_stubs:
      enabled: false
    cluster_gateway_proxy:
      url: https://cgp.example.com
    secret_manager_api:
      url: https://sma.example.com

  webhook:
    delivery_url: https://webhook.example.com
    local_smee:
      enabled: false`,
	RunE: runConfigImport,
}

// configUseCmd records a local config file so future commands load it
// automatically — no need to pass --config every time.
var configUseCmd = &cobra.Command{
	Use:   "use <path>",
	Short: "Set the config file future commands use (no --config needed)",
	Long: `Records <path> as the active config file. Subsequent aep commands load it
automatically (equivalent to passing --config <path> each time). Override for a
single command with --config, or drop it with 'aep platform config clear'.`,
	Args: cobra.ExactArgs(1),
	// Local operation — no cluster needed.
	Annotations: map[string]string{"skipClusterConfig": "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		abs, err := config.SetActiveConfig(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Active config file set: %s\n", abs)
		fmt.Println("Future aep commands will use it (override with --config, or run 'aep platform config clear').")
		return nil
	},
}

// configWhichCmd prints the active config file, if any.
var configWhichCmd = &cobra.Command{
	Use:         "which",
	Short:       "Print the active config file (set by 'config use')",
	Annotations: map[string]string{"skipClusterConfig": "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if p := config.ActiveConfig(); p != "" {
			fmt.Println(p)
		} else {
			fmt.Println("no active config file set")
		}
		return nil
	},
}

// configClearCmd removes the active config file pointer.
var configClearCmd = &cobra.Command{
	Use:         "clear",
	Short:       "Unset the active config file (set by 'config use')",
	Annotations: map[string]string{"skipClusterConfig": "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.ClearActiveConfig(); err != nil {
			return err
		}
		fmt.Println("Active config file cleared.")
		return nil
	},
}

func init() {
	platformCmd.AddCommand(configCmd)
	configCmd.AddCommand(configImportCmd)
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configWhichCmd)
	configCmd.AddCommand(configClearCmd)
	configImportCmd.Flags().StringVar(&configImportFile, "config", "", "path to local config YAML file (required)")
	_ = configImportCmd.MarkFlagRequired("config")
}

func runConfigImport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Read the local file into a standalone viper instance so it does not
	// interfere with the global viper used by the rest of the CLI.
	fv := viper.New()
	fv.SetConfigFile(configImportFile)
	if err := fv.ReadInConfig(); err != nil {
		return fmt.Errorf("read config file %s: %w", configImportFile, err)
	}

	if fv.IsSet("thunder.admin_client_secret") {
		_, _ = fmt.Fprintln(os.Stderr, "warning: thunder.admin_client_secret is managed by OpenBao/ESO — ignoring")
	}

	// Collect only the keys that are explicitly set in the file.
	data := make(map[string]string)
	for _, k := range config.ConfigMapKeys {
		if fv.IsSet(k) {
			data[k] = fv.GetString(k)
		}
	}
	if len(data) == 0 {
		return fmt.Errorf("no recognised config keys found in %s", configImportFile)
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	const aepNamespace = "wso2-aep"
	existing, err := client.CoreV1().ConfigMaps(aepNamespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get %s: %w", config.ConfigMapName, err)
		}
		_, err = client.CoreV1().ConfigMaps(aepNamespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.ConfigMapName,
				Namespace: aepNamespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "aepctl"},
			},
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create %s: %w", config.ConfigMapName, err)
		}
	} else {
		if existing.Data == nil {
			existing.Data = make(map[string]string)
		}
		for k, v := range data {
			existing.Data[k] = v
		}
		_, err = client.CoreV1().ConfigMaps(aepNamespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update %s: %w", config.ConfigMapName, err)
		}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	_, _ = fmt.Fprintf(os.Stdout, "Applied %d key(s) to %s/%s:\n", len(keys), aepNamespace, config.ConfigMapName)
	for _, k := range keys {
		_, _ = fmt.Fprintf(os.Stdout, "  %s\n", k)
	}
	return nil
}
