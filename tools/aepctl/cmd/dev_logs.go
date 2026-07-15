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
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"

	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var (
	devLogsFollow bool
	devLogsTail   int64
)

var devLogsCmd = &cobra.Command{
	Use:   "logs <service>",
	Short: "Tail the logs of a dev-mode service",
	Args:  cobra.ExactArgs(1),
	RunE:  runDevLogs,
}

func init() {
	devCmd.AddCommand(devLogsCmd)
	devLogsCmd.Flags().BoolVarP(&devLogsFollow, "follow", "f", false, "Stream new log output")
	devLogsCmd.Flags().Int64Var(&devLogsTail, "tail", 200, "Number of recent lines to show")
}

func runDevLogs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if !viper.GetBool("dev.enabled") {
		return fmt.Errorf("dev mode is disabled — enable it with `aep dev enable`")
	}
	s, ok := lookupDevService(args[0])
	if !ok {
		return fmt.Errorf("unknown service %q", args[0])
	}
	client, err := k8s.NewClient("")
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	ns := viper.GetString("dev.namespace")

	pod, err := k8s.FindRunningPod(ctx, client, ns, s.Deployment)
	if err != nil {
		return fmt.Errorf("find running pod for %s: %w", s.Name, err)
	}

	opts := &corev1.PodLogOptions{
		Container: s.Container,
		Follow:    devLogsFollow,
	}
	if devLogsTail >= 0 {
		opts.TailLines = &devLogsTail
	}
	stream, err := client.CoreV1().Pods(ns).GetLogs(pod, opts).Stream(ctx)
	if err != nil {
		return fmt.Errorf("stream logs for %s: %w", pod, err)
	}
	defer func() { _ = stream.Close() }()

	_, err = io.Copy(os.Stdout, stream)
	return err
}
