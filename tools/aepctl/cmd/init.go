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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aepctl/internal/adminpb"
	"github.com/wso2/aep/aepctl/internal/dev"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

// buildOpenBaoSealValues constructs a Helm values YAML snippet that injects
// the configured KMS seal stanza into the OpenBao standalone config.
// Returns ("", nil) when no seal type is configured (Shamir mode, local dev).
func buildOpenBaoSealValues() (string, error) {
	sealType := viper.GetString("openbao.seal.type")
	if sealType == "" {
		return "", nil
	}

	var sealBlock string
	switch sealType {
	case "awskms":
		region := viper.GetString("openbao.seal.awskms.region")
		keyID := viper.GetString("openbao.seal.awskms.kms_key_id")
		if region == "" || keyID == "" {
			return "", fmt.Errorf("openbao.seal.awskms.region and openbao.seal.awskms.kms_key_id are required")
		}
		sealBlock = fmt.Sprintf("seal \"awskms\" {\n  region     = %q\n  kms_key_id = %q\n}", region, keyID)
	case "gcpckms":
		project := viper.GetString("openbao.seal.gcpckms.project")
		region := viper.GetString("openbao.seal.gcpckms.region")
		keyRing := viper.GetString("openbao.seal.gcpckms.key_ring")
		cryptoKey := viper.GetString("openbao.seal.gcpckms.crypto_key")
		if project == "" || keyRing == "" || cryptoKey == "" {
			return "", fmt.Errorf("openbao.seal.gcpckms.project, key_ring, and crypto_key are required")
		}
		sealBlock = fmt.Sprintf("seal \"gcpckms\" {\n  project    = %q\n  region     = %q\n  key_ring   = %q\n  crypto_key = %q\n}", project, region, keyRing, cryptoKey)
	case "azurekeyvault":
		vaultName := viper.GetString("openbao.seal.azurekeyvault.vault_name")
		keyName := viper.GetString("openbao.seal.azurekeyvault.key_name")
		if vaultName == "" || keyName == "" {
			return "", fmt.Errorf("openbao.seal.azurekeyvault.vault_name and openbao.seal.azurekeyvault.key_name are required")
		}
		sealBlock = fmt.Sprintf("seal \"azurekeyvault\" {\n  vault_name = %q\n  key_name   = %q\n}", vaultName, keyName)
	default:
		return "", fmt.Errorf("unknown openbao.seal.type %q — must be awskms, gcpckms, or azurekeyvault", sealType)
	}

	// Full standalone config: mirrors the subchart default plus the seal stanza.
	hcl := "ui = true\n\nlistener \"tcp\" {\n  tls_disable     = 1\n  address         = \"[::]:8200\"\n  cluster_address = \"[::]:8201\"\n}\nstorage \"file\" {\n  path = \"/openbao/data\"\n}\n\n" + sealBlock + "\n"

	var b strings.Builder
	b.WriteString("aep-openbao:\n  server:\n    standalone:\n      config: |\n")
	for _, line := range strings.Split(hcl, "\n") {
		if line != "" {
			b.WriteString("        " + line + "\n")
		} else {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

var (
	initPlatformChart        string
	initPlatformVersion      string
	initPlatformRelease      string
	initPlatformNamespace    string
	initConsoleURL           string
	initAPIURL               string
	initWorkspacesAccessMode string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision OpenBao, install the platform, and configure Thunder",
	Long: `Full AEP initialisation in one command:
  1. Waits for the OpenBao pod to be ready
  2. Provisions OpenBao and generates all secrets
  3. Installs the platform Helm chart
  4. Waits for all platform pods to be ready
  5. Registers AEP OAuth clients in Thunder

Configure the server URL first:
  aep connect --server http://aep-server.openchoreo.localhost:8080`,
	RunE: runAEPInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initPlatformChart, "platform-chart", "", "Local path to the platform Helm chart (for local/dev installs; takes precedence over --platform-version)")
	initCmd.Flags().StringVar(&initPlatformVersion, "platform-version", "latest", "Platform version to pull from GHCR (ignored when --platform-chart is set)")
	initCmd.Flags().StringVar(&initPlatformRelease, "platform-release", "aep-platform", "Helm release name for the platform chart")
	initCmd.Flags().StringVar(&initPlatformNamespace, "namespace", "wso2-aep", "Kubernetes namespace")
	initCmd.Flags().StringVar(&initConsoleURL, "console-url", "http://console.openchoreo.localhost:8080", "Public URL of the AEP console")
	initCmd.Flags().StringVar(&initAPIURL, "api-url", "http://api.openchoreo.localhost:8080", "Public URL of the AEP API")
	initCmd.Flags().StringVar(&initWorkspacesAccessMode, "workspaces-access-mode", "", "PVC access mode for the shared workspaces volume (e.g. ReadWriteOnce for local k3d, ReadWriteMany for production)")
	_ = viper.BindPFlag("platform.workspaces.access_mode", initCmd.Flags().Lookup("workspaces-access-mode"))
	initCmd.Flags().String("oc-api-url", "", "In-cluster URL of the OpenChoreo platform API (overrides config file)")
	_ = viper.BindPFlag("oc.api_url", initCmd.Flags().Lookup("oc-api-url"))
	initCmd.Flags().String("server", "", "AEP server gRPC URL (overrides config file)")
	_ = viper.BindPFlag("server", initCmd.Flags().Lookup("server"))
	registerThunderFlags(initCmd)
}

func runAEPInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH\nInstall it from https://helm.sh/docs/intro/install/ and try again")
	}

	k8sClient, err := k8s.NewClient("")
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// 1. Wait for OpenBao pod.
	if err := waitForOpenBaoPod(ctx, k8sClient, initPlatformNamespace); err != nil {
		return err
	}

	// 2. Prompt for secrets.
	anthropicKey, err := readMaskedInput("Anthropic API key")
	if err != nil {
		return fmt.Errorf("read Anthropic API key: %w", err)
	}
	if anthropicKey == "" {
		return fmt.Errorf("an Anthropic API key is required")
	}

	// 3. Provision OpenBao via the management server.
	client, ctx, closeConn, err := dialServer(ctx)
	if err != nil {
		return err
	}
	defer closeConn()

	_, _ = fmt.Fprintln(os.Stdout, "Provisioning OpenBao...")
	stream, err := client.Init(ctx, &adminpb.InitRequest{
		AnthropicApiKey: anthropicKey,
	})
	if err != nil {
		return fmt.Errorf("call Init: %w", err)
	}

	var complete *adminpb.InitComplete
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		switch p := event.Payload.(type) {
		case *adminpb.InitEvent_Progress:
			_, _ = fmt.Fprintf(os.Stdout, "  %s\n", p.Progress)
		case *adminpb.InitEvent_Error:
			return fmt.Errorf("server error: %s", p.Error)
		case *adminpb.InitEvent_Complete:
			complete = p.Complete
		}
	}

	if complete != nil {
		if len(complete.RecoveryKeys) > 0 {
			printOpenBaoRecoveryKeys(complete.RecoveryKeys)
		} else if len(complete.UnsealKeys) > 0 {
			printOpenBaoUnsealKeys(complete.UnsealKeys)
		}
	}

	// 4. Install the platform chart.
	sealValues, err := buildOpenBaoSealValues()
	if err != nil {
		return fmt.Errorf("build OpenBao seal config: %w", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, "Installing platform chart...")
	thunderURL := viper.GetString("thunder.url")
	helmArgs := []string{
		"install", initPlatformRelease,
		"-n", initPlatformNamespace,
		"--set", "console.publicURL=" + initConsoleURL,
		"--set", "aepApi.publicURL=" + initAPIURL,
		"--set", "console.thunderPublicURL=" + viper.GetString("thunder.public_url"),
		"--set", "thunder.adminURL=" + thunderURL,
		"--set", "thunder.jwksURL=" + thunderURL + "/oauth2/jwks",
		"--set", "platformAPI.baseURL=" + viper.GetString("oc.api_url"),
	}
	if initPlatformChart != "" {
		// Local chart path — used for dev/local testing.
		helmArgs = append([]string{helmArgs[0], helmArgs[1], initPlatformChart}, helmArgs[2:]...)
	} else {
		// Pull chart from GHCR.
		helmArgs = append([]string{helmArgs[0], helmArgs[1], "oci://ghcr.io/wso2/aep/charts/platform"}, helmArgs[2:]...)
		if initPlatformVersion != "latest" {
			helmArgs = append(helmArgs, "--version", initPlatformVersion)
		}
	}
	if mode := viper.GetString("platform.workspaces.access_mode"); mode != "" {
		helmArgs = append(helmArgs, "--set", "workspaces.accessMode="+mode)
	}
	// Coding-agent dispatch: deploy the local cluster-gateway-proxy stub (reads
	// pod logs/job status for live streaming + JobWatcher) unless disabled. Prod
	// installs set codingagent.local_stubs.enabled=false and supply the real
	// endpoint URLs — see ~/.aep/config.yaml.
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("codingAgentDispatch.localStubs.enabled=%t", viper.GetBool("codingagent.local_stubs.enabled")))
	if u := viper.GetString("codingagent.cluster_gateway_proxy.url"); u != "" {
		helmArgs = append(helmArgs, "--set", "codingAgentDispatch.clusterGatewayProxy.url="+u)
	}
	if u := viper.GetString("codingagent.secret_manager_api.url"); u != "" {
		helmArgs = append(helmArgs, "--set", "codingAgentDispatch.secretManagerApi.url="+u)
	}
	// GitHub webhook delivery: register deliveryURL on repos and, locally, deploy
	// the smee-client that forwards the smee.io channel into the cluster. Prod
	// sets webhook.local_smee.enabled=false and delivery_url to a real ingress.
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("webhook.localSmee.enabled=%t", viper.GetBool("webhook.local_smee.enabled")))
	if u := viper.GetString("webhook.delivery_url"); u != "" {
		helmArgs = append(helmArgs, "--set", "webhook.deliveryURL="+u)
	}
	// Local OpenChoreo org-unit provisioning: create the per-org namespaced
	// ComponentTypes + api-configuration trait aep-api references (cloud does this
	// via platform-api ProvisionOrgUnit). Prod sets enabled=false.
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("localOrgProvisioning.enabled=%t", viper.GetBool("oc.local_org_provisioning.enabled")))
	if ns := viper.GetString("oc.org_namespace"); ns != "" {
		helmArgs = append(helmArgs, "--set", "localOrgProvisioning.orgNamespace="+ns)
	}
	// KMS auto-unseal: if a seal type is configured, inject the seal stanza into
	// the OpenBao standalone config via a temporary values override file.
	if sealValues != "" {
		sealFile, err := os.CreateTemp("", "aep-openbao-seal-*.yaml")
		if err != nil {
			return fmt.Errorf("create OpenBao seal values file: %w", err)
		}
		defer os.Remove(sealFile.Name())
		if _, err := sealFile.WriteString(sealValues); err != nil {
			return fmt.Errorf("write OpenBao seal values: %w", err)
		}
		if err := sealFile.Close(); err != nil {
			return fmt.Errorf("close OpenBao seal values file: %w", err)
		}
		helmArgs = append(helmArgs, "-f", sealFile.Name())
		_, _ = fmt.Fprintf(os.Stdout, "OpenBao: KMS auto-unseal enabled (%s)\n", viper.GetString("openbao.seal.type"))
	}

	// Dev mode: if enabled and a dev-values override exists (written by
	// `aep dev reload`), pass it last so locally-built images survive this
	// install/upgrade instead of reverting to the registry images.
	if viper.GetBool("dev.enabled") {
		if devValues, err := dev.ValuesPath(); err == nil {
			if _, statErr := os.Stat(devValues); statErr == nil {
				helmArgs = append(helmArgs, "-f", devValues)
				_, _ = fmt.Fprintf(os.Stdout, "Dev mode: applying image overrides from %s\n", devValues)
			}
		}
	}
	var helmOut bytes.Buffer
	helmCmd := exec.CommandContext(ctx, "helm", helmArgs...)
	helmCmd.Stdout = &helmOut
	helmCmd.Stderr = &helmOut
	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm install platform: %w\n%s", err, helmOut.String())
	}
	_, _ = fmt.Fprintln(os.Stdout, "Platform chart installed.")

	// 5. Wait for all platform pods.
	if err := waitForAllPodsReady(ctx, k8sClient, initPlatformNamespace, 10*time.Minute); err != nil {
		return err
	}

	// 6. Register AEP OAuth clients in Thunder.
	_, _ = fmt.Fprintln(os.Stdout, "Configuring Thunder OAuth clients...")
	if err := doThunderSetup(ctx, k8sClient, initPlatformNamespace,
		viper.GetString("thunder.namespace"),
		viper.GetString("thunder.url"),
		viper.GetString("thunder.config_map"),
		viper.GetString("thunder.deployment"),
		viper.GetString("thunder.admin_client_id"),
		initConsoleURL,
	); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nAEP is ready. Open the console to get started.")
	return nil
}

// waitForOpenBaoPod blocks until the OpenBao pod is Running. Does NOT wait for
// Ready — the readiness probe fails until after init completes, so waiting for
// Ready would deadlock.
func waitForOpenBaoPod(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	_, _ = fmt.Fprintf(os.Stdout, "Waiting for OpenBao pod")
	for {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=aep-openbao",
		})
		if err == nil && len(pods.Items) > 0 {
			pod := pods.Items[0]
			if pod.Status.Phase == "Running" && len(pod.Status.ContainerStatuses) > 0 {
				started := pod.Status.ContainerStatuses[0].Started
				if started != nil && *started {
					_, _ = fmt.Fprintln(os.Stdout, " ready")
					return nil
				}
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, ".")
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(os.Stdout)
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// waitForAllPodsReady blocks until every pod in the namespace is Ready, or the
// timeout elapses.
func waitForAllPodsReady(ctx context.Context, client *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	_, _ = fmt.Fprintf(os.Stdout, "Waiting for platform pods")

	var lastNotReady []string
	for {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(os.Stdout)
			return fmt.Errorf("list pods: %w", err)
		}

		var notReady []string
		for _, p := range pods.Items {
			ready := false
			switch p.Status.Phase {
			case "Succeeded":
				ready = true
			case "Running":
				ready = true
				for _, c := range p.Status.ContainerStatuses {
					if !c.Ready {
						ready = false
						break
					}
				}
			}
			if !ready {
				notReady = append(notReady, p.Name)
			}
		}

		if len(pods.Items) > 0 && len(notReady) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, " ready")
			return nil
		}
		lastNotReady = notReady

		if time.Now().After(deadline) {
			_, _ = fmt.Fprintln(os.Stdout)
			return fmt.Errorf("timed out after %s waiting for pods: %s", timeout, strings.Join(lastNotReady, ", "))
		}

		_, _ = fmt.Fprintf(os.Stdout, ".")
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(os.Stdout)
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func printOpenBaoUnsealKeys(keys []string) {
	_, _ = fmt.Fprintln(os.Stdout, "\n+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout, "|  STORE THESE SECURELY - they cannot be retrieved later           |")
	_, _ = fmt.Fprintln(os.Stdout, "|  Unseal keys: need 3 of 5 to unseal after every pod restart      |")
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	for i, k := range keys {
		_, _ = fmt.Fprintf(os.Stdout, "  Key %d: %s\n", i+1, k)
	}
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout)
}

func printOpenBaoRecoveryKeys(keys []string) {
	_, _ = fmt.Fprintln(os.Stdout, "\n+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout, "|  STORE THESE SECURELY - they cannot be retrieved later           |")
	_, _ = fmt.Fprintln(os.Stdout, "|  Recovery keys: break-glass emergency use only                   |")
	_, _ = fmt.Fprintln(os.Stdout, "|  KMS auto-unseal is active — NOT needed for normal restarts      |")
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	for i, k := range keys {
		_, _ = fmt.Fprintf(os.Stdout, "  Key %d: %s\n", i+1, k)
	}
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout)
}

// readMaskedInput prompts on stderr and reads hidden input from the terminal.
func readMaskedInput(prompt string) (string, error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
