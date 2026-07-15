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
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aepctl/internal/dev"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var devReloadCmd = &cobra.Command{
	Use:   "reload <service|all>",
	Short: "Build a service from local source, import it, and redeploy it",
	Long: `Rebuilds one service (or all) from the local AEP source, imports the image
into the k3d cluster, records the image override in ~/.aep/dev-values.yaml, and
rolls the running Deployment to the new image.

  aep dev reload aep-api
  aep dev reload all

Valid services: ` + fmt.Sprint(devServiceNames()),
	Args: cobra.ExactArgs(1),
	RunE: runDevReload,
}

func init() {
	devCmd.AddCommand(devReloadCmd)
}

func runDevReload(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	root, err := requireDevMode()
	if err != nil {
		return err
	}
	services, err := resolveDevServices(args[0])
	if err != nil {
		return err
	}
	cluster, err := requireK3d(ctx)
	if err != nil {
		return err
	}
	client, err := k8s.NewClient("")
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	valuesPath, err := dev.ValuesPath()
	if err != nil {
		return err
	}
	ns := viper.GetString("dev.namespace")

	for _, s := range services {
		if err := reloadService(ctx, client, root, cluster, ns, valuesPath, s); err != nil {
			return fmt.Errorf("reload %s: %w", s.Name, err)
		}
	}

	fmt.Fprintf(os.Stdout, "\n✅ Reloaded %d service(s). Images are local-only and recorded in %s.\n",
		len(services), valuesPath)
	return nil
}

func reloadService(ctx context.Context, client *kubernetes.Clientset, root, cluster, ns, valuesPath string, s devService) error {
	fmt.Fprintf(os.Stdout, "\n=== %s ===\n", s.Name)

	// Fail fast if the Deployment isn't present (e.g. `collab` before its chart
	// lands) — before spending minutes on a build.
	if _, err := client.AppsV1().Deployments(ns).Get(ctx, s.Deployment, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("deployment %s/%s not found — is this service installed in the cluster?", ns, s.Deployment)
		}
		return fmt.Errorf("get deployment %s/%s: %w", ns, s.Deployment, err)
	}

	tag := devImageTag(s)
	if err := dev.BuildImage(ctx, root, s.Dockerfile, s.Context, tag, os.Stdout); err != nil {
		return err
	}
	if err := dev.ImportImage(ctx, tag, cluster, os.Stdout); err != nil {
		return err
	}
	if err := dev.MergeImageOverride(valuesPath, s.ValuesKey, imageRepo(tag), "dev", "Never"); err != nil {
		return err
	}
	if err := setImageAndRoll(ctx, client, ns, s, tag); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "→ waiting for rollout of deployment/%s...\n", s.Deployment)
	if err := waitForRolloutComplete(ctx, client, ns, s.Deployment, 5*time.Minute); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "✅ %s running %s\n", s.Name, tag)
	return nil
}

// imageRepo strips the ":dev" tag suffix, returning just the repository — the
// form Helm values want (image.repository + image.tag are separate keys).
func imageRepo(tag string) string {
	return tag[:len(tag)-len(":dev")]
}

// setImageAndRoll patches the Deployment's container image + pull policy and
// stamps a reload annotation. The annotation forces a new ReplicaSet even though
// the ":dev" tag is unchanged (only its content in the node changed).
func setImageAndRoll(ctx context.Context, client *kubernetes.Clientset, ns string, s devService, tag string) error {
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"aepctl.wso2.com/reloadedAt": time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
				"spec": map[string]any{
					"containers": []map[string]any{
						{"name": s.Container, "image": tag, "imagePullPolicy": "Never"},
					},
				},
			},
		},
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err := client.AppsV1().Deployments(ns).Patch(ctx, s.Deployment, types.StrategicMergePatchType, b, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch deployment %s/%s: %w", ns, s.Deployment, err)
	}
	return nil
}

// waitForRolloutComplete blocks until the Deployment has rolled all replicas to
// the latest revision, or the timeout elapses.
func waitForRolloutComplete(ctx context.Context, client *kubernetes.Clientset, ns, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		d, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get deployment %s/%s: %w", ns, name, err)
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		st := d.Status
		rolled := st.ObservedGeneration >= d.Generation &&
			st.UpdatedReplicas == desired &&
			st.AvailableReplicas == desired &&
			st.Replicas == desired
		if rolled {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for rollout of %s/%s (updated=%d available=%d desired=%d)",
				timeout, ns, name, st.UpdatedReplicas, st.AvailableReplicas, desired)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
