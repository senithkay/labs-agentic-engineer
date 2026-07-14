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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wso2/aep/aepctl/internal/dev"
)

// devCmd is the parent for the local source-driven inner loop. After editing a
// service's code, `aep dev reload <service>` builds the image from the local
// checkout, imports it into the k3d cluster, and redeploys the running service.
//
// Dev mode is opt-in: enable it once with `aep dev enable --project-path <dir>`.
// Commands that mutate the cluster (reload/restart/logs) refuse unless enabled.
var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Local source-driven dev mode (build, import, redeploy services)",
	Long: `Dev mode rebuilds AEP platform services from a local source checkout and
redeploys them into the running k3d cluster.

Enable it once, pointing at your AEP repo:

  aep dev enable --project-path ~/src/aep

Then, after editing a service's code:

  aep dev reload aep-api      # build image + import to cluster + redeploy
  aep dev restart aep-api     # rollout restart only (no rebuild)
  aep dev status              # show dev mode + per-service images
  aep dev logs aep-api -f     # tail the service logs

Reloaded images are local-only (imported into k3d, never pushed) and are
persisted to ~/.aep/dev-values.yaml so a later 'aep init'/upgrade keeps them.`,
}

func init() {
	rootCmd.AddCommand(devCmd)
}

// devService maps a service name to how it is built (dockerfile + context,
// relative to the project root — matching .github/workflows/build-images.yml)
// and how it is deployed (Deployment + container name in the platform namespace,
// and the top-level values key used for the dev-values.yaml image override).
type devService struct {
	Name       string
	Dockerfile string
	Context    string
	Deployment string
	Container  string
	ValuesKey  string
	// DevServerCmd, when non-empty, is the local dev server for this service
	// (run from the project root by `aep dev serve`). Only edge/leaf services
	// like the console frontend can run locally — backends are called by other
	// services via cluster DNS, so they stay in-cluster (empty here).
	DevServerCmd  []string
	DevServerPort int
	// DevServerPrepare are commands run in order before the dev server (from the
	// project root) to bring the local checkout to the state the dev server
	// needs — the same prerequisites the Dockerfile handles: (1) install the
	// service's dependency closure (the local node_modules may lag the
	// lockfile), and (2) build the workspace deps whose exports resolve to ./dist
	// (Vite can't resolve them otherwise).
	DevServerPrepare [][]string
}

// devServices is the set of platform runtime Deployments dev mode can rebuild.
// collab's chart is not on every branch yet (it lands with the collab work);
// reload verifies the Deployment exists first and errors clearly if it does not.
var devServices = []devService{
	{Name: "aep-api", Dockerfile: "services/aep-api/Dockerfile", Context: "services/aep-api", Deployment: "aep-api", Container: "aep-api", ValuesKey: "aepApi"},
	{Name: "agents", Dockerfile: "services/agents/Dockerfile", Context: ".", Deployment: "aep-agents", Container: "aep-agents", ValuesKey: "aepAgents"},
	{Name: "aep-mcp-server", Dockerfile: "services/aep-mcp-server/Dockerfile", Context: ".", Deployment: "aep-mcp-server", Container: "aep-mcp-server", ValuesKey: "aepMcpServer"},
	{
		Name: "console", Dockerfile: "apps/console/Dockerfile", Context: ".",
		Deployment: "aep-console", Container: "aep-console", ValuesKey: "console",
		DevServerCmd:  []string{"pnpm", "--filter", "@aep/console", "dev"},
		DevServerPort: 8090,
		DevServerPrepare: [][]string{
			{"pnpm", "install", "--frozen-lockfile", "--filter", "@aep/console..."},
			{"pnpm", "--filter", "@aep/console^...", "build"},
		},
	},
	{Name: "collab", Dockerfile: "services/collab/Dockerfile", Context: ".", Deployment: "collab-server", Container: "collab-server", ValuesKey: "collab"},
}

func lookupDevService(name string) (devService, bool) {
	for _, s := range devServices {
		if s.Name == name {
			return s, true
		}
	}
	return devService{}, false
}

func devServiceNames() []string {
	names := make([]string, len(devServices))
	for i, s := range devServices {
		names[i] = s.Name
	}
	return names
}

// resolveDevServices turns a command argument into the services to act on:
// "all" expands to every registered service; any other value must name one.
func resolveDevServices(arg string) ([]devService, error) {
	if arg == "all" {
		return devServices, nil
	}
	s, ok := lookupDevService(arg)
	if !ok {
		return nil, fmt.Errorf("unknown service %q — valid: %s, all",
			arg, strings.Join(devServiceNames(), ", "))
	}
	return []devService{s}, nil
}

// devImageTag is the deterministic local tag for a service's dev image, e.g.
// aep-dev/aep-api:dev. The stable tag keeps ~/.aep/dev-values.yaml stable across
// reloads; each reload overwrites the tag's content in the k3d node.
func devImageTag(s devService) string {
	return fmt.Sprintf("%s/%s:dev", viper.GetString("dev.image_prefix"), s.Name)
}

// devProjectRoot returns the configured local source directory, validating that
// it exists and looks like the AEP repo root.
func devProjectRoot() (string, error) {
	root := viper.GetString("dev.project_path")
	if root == "" {
		return "", fmt.Errorf("no project path set — run `aep dev enable --project-path <dir>` or `aep dev set-path <dir>`")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project path %q is not a directory", root)
	}
	// A light sanity check: the AEP repo root has an AGENTS.md and a Go/pnpm
	// workspace marker. Catches pointing at the wrong directory before a build.
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		return "", fmt.Errorf("project path %q does not look like the AEP repo root (no AGENTS.md)", root)
	}
	return root, nil
}

// requireDevMode is the guard for cluster-mutating dev subcommands: dev mode
// must be enabled and the project path valid. Returns the validated root.
func requireDevMode() (string, error) {
	if !viper.GetBool("dev.enabled") {
		return "", fmt.Errorf("dev mode is disabled — enable it with `aep dev enable --project-path <dir>`")
	}
	return devProjectRoot()
}

// requireK3d verifies the k3d binary is available and the configured cluster
// exists — the prerequisite for importing locally-built images.
func requireK3d(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("k3d"); err != nil {
		return "", fmt.Errorf("k3d is required for dev mode but was not found in PATH\nInstall it from https://k3d.io and try again")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker is required for dev mode but was not found in PATH")
	}
	cluster := viper.GetString("dev.k3d_cluster")
	exists, err := dev.K3dClusterExists(ctx, cluster)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("k3d cluster %q not found — set dev.k3d_cluster in ~/.aep/config.yaml or create it", cluster)
	}
	return cluster, nil
}

// saveConfig writes the current viper config to ~/.aep/config.yaml (same pattern
// as `aep connect`). Shared by the dev enable/disable/set-path subcommands.
func saveConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	cfgDir := filepath.Join(home, ".aep")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	cfgFile := filepath.Join(cfgDir, "config.yaml")
	if err := viper.WriteConfigAs(cfgFile); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return cfgFile, nil
}
