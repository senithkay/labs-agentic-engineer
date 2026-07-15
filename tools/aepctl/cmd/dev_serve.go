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
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	devServeReal        bool
	devServeAPIPort     int
	devServeSkipPrepare bool
)

var devServeCmd = &cobra.Command{
	Use:   "serve <service>",
	Short: "Run a service's local dev server (HMR) instead of building an image",
	Long: `Runs a service's local development server with hot-reload, as a fast
alternative to the in-cluster build/redeploy of 'aep dev reload'.

Only edge/leaf services (the console frontend) can run locally: nothing in the
cluster calls into them. Backend services are reached by other services via
cluster DNS and need in-cluster dependencies, so they have no local dev server —
use 'aep dev reload <service>' for those.

  aep dev serve console            # Vite dev server on :8090 with mock data
  aep dev serve console --real     # proxy to the in-cluster aep-api (auto port-forward)

Runs in the foreground; press Ctrl-C to stop.`,
	Args: cobra.ExactArgs(1),
	RunE: runDevServe,
}

func init() {
	devCmd.AddCommand(devServeCmd)
	devServeCmd.Flags().BoolVar(&devServeReal, "real", false, "Proxy to the in-cluster aep-api (auto kubectl port-forward) instead of mock data")
	devServeCmd.Flags().IntVar(&devServeAPIPort, "api-port", 9090, "Local port for the aep-api port-forward (with --real)")
	devServeCmd.Flags().BoolVar(&devServeSkipPrepare, "skip-prepare", false, "Skip building the service's workspace deps before starting (faster when already built)")
}

func runDevServe(cmd *cobra.Command, args []string) error {
	root, err := requireDevMode()
	if err != nil {
		return err
	}
	s, ok := lookupDevService(args[0])
	if !ok {
		return fmt.Errorf("unknown service %q — valid: %s", args[0], devServiceNames())
	}
	if len(s.DevServerCmd) == 0 {
		return fmt.Errorf("%s has no local dev server. Backend services run in-cluster "+
			"because other services reach them by cluster DNS — use `aep dev reload %s`.", s.Name, s.Name)
	}
	if _, err := exec.LookPath(s.DevServerCmd[0]); err != nil {
		return fmt.Errorf("%q is required to run the %s dev server but was not found in PATH", s.DevServerCmd[0], s.Name)
	}

	// Ctrl-C cancels the context, which stops the dev server (and the
	// port-forward child, if any) cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Bring the local checkout to the state the dev server needs: install the
	// dependency closure (node_modules may lag the lockfile) then build the
	// workspace deps whose exports resolve to ./dist (Vite can't resolve them
	// otherwise) — the same prerequisites the Dockerfile handles. Both are fast
	// when already satisfied; --skip-prepare bypasses them once warm.
	if len(s.DevServerPrepare) > 0 && !devServeSkipPrepare {
		fmt.Fprintf(os.Stdout, "Preparing dependencies for %s...\n", s.Name)
		for _, cmdArgs := range s.DevServerPrepare {
			prep := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
			prep.Dir = root
			prep.Stdout = os.Stdout
			prep.Stderr = os.Stderr
			if err := prep.Run(); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("prepare %s (%s): %w", s.Name, cmdArgs[0], err)
			}
		}
	}

	env := os.Environ()
	if devServeReal {
		cleanup, err := startAPIPortForward(ctx)
		if err != nil {
			return err
		}
		defer cleanup()
		env = append(env, fmt.Sprintf("API_PROXY_TARGET=http://localhost:%d", devServeAPIPort))
		fmt.Fprintf(os.Stdout, "Data source: in-cluster aep-api (via port-forward :%d)\n", devServeAPIPort)
	} else {
		env = append(env, "VITE_API_MODE=mock")
		fmt.Fprintln(os.Stdout, "Data source: mock (VITE_API_MODE=mock) — no cluster. Use --real for live data.")
	}

	if s.DevServerPort > 0 {
		fmt.Fprintf(os.Stdout, "Starting %s dev server → http://localhost:%d (Ctrl-C to stop)\n", s.Name, s.DevServerPort)
	}

	c := exec.CommandContext(ctx, s.DevServerCmd[0], s.DevServerCmd[1:]...)
	c.Dir = root
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		// A Ctrl-C (context canceled) is a clean stop, not an error.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("%s dev server: %w", s.Name, err)
	}
	return nil
}

// startAPIPortForward spawns `kubectl port-forward` for the in-cluster aep-api
// and returns a cleanup func that stops it. Best-effort readiness wait so the
// first proxied request from the dev server connects.
func startAPIPortForward(ctx context.Context) (func(), error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return nil, fmt.Errorf("kubectl is required for --real but was not found in PATH")
	}
	ns := viper.GetString("dev.namespace")
	pf := exec.CommandContext(ctx, "kubectl", "-n", ns, "port-forward",
		"svc/aep-api", fmt.Sprintf("%d:9090", devServeAPIPort))
	pf.Stdout = os.Stdout
	pf.Stderr = os.Stderr
	if err := pf.Start(); err != nil {
		return nil, fmt.Errorf("start aep-api port-forward: %w", err)
	}
	// Give the tunnel a moment to establish before the dev server starts.
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
	cleanup := func() {
		if pf.Process != nil {
			_ = pf.Process.Kill()
		}
		_ = pf.Wait()
	}
	return cleanup, nil
}
