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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var devStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show dev mode state and per-service deployment images",
	RunE:  runDevStatus,
}

func init() {
	devCmd.AddCommand(devStatusCmd)
}

func runDevStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	enabled := viper.GetBool("dev.enabled")
	fmt.Fprintf(os.Stdout, "Dev mode:  %s\n", enabledLabel(enabled))
	fmt.Fprintf(os.Stdout, "Project:   %s\n", orNotSet(viper.GetString("dev.project_path")))
	fmt.Fprintf(os.Stdout, "Cluster:   %s (k3d)\n", viper.GetString("dev.k3d_cluster"))
	ns := viper.GetString("dev.namespace")
	fmt.Fprintf(os.Stdout, "Namespace: %s\n\n", ns)

	client, err := k8s.NewClient("")
	if err != nil {
		fmt.Fprintf(os.Stdout, "(cluster unreachable: %v)\n", err)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tDEPLOYMENT\tREADY\tIMAGE")
	for _, s := range devServices {
		d, err := client.AppsV1().Deployments(ns).Get(ctx, s.Deployment, metav1.GetOptions{})
		if err != nil {
			state := "error"
			if apierrors.IsNotFound(err) {
				state = "not installed"
			}
			fmt.Fprintf(w, "%s\t%s\t-\t(%s)\n", s.Name, s.Deployment, state)
			continue
		}
		image := "-"
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == s.Container {
				image = c.Image
				break
			}
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\n", s.Name, s.Deployment, d.Status.ReadyReplicas, desired, image)
	}
	return w.Flush()
}

func orNotSet(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func enabledLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
