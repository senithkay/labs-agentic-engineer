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

var devEnableProjectPath string

var devEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable dev mode (optionally set the source directory)",
	Long: `Turns dev mode on and persists it to ~/.aep/config.yaml. Optionally set the
local AEP source directory that images are built from:

  aep dev enable --project-path ~/src/aep`,
	RunE: runDevEnable,
}

var devDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable dev mode",
	RunE:  runDevDisable,
}

var devSetPathCmd = &cobra.Command{
	Use:   "set-path <dir>",
	Short: "Set the local AEP source directory used by dev mode",
	Args:  cobra.ExactArgs(1),
	RunE:  runDevSetPath,
}

func init() {
	devCmd.AddCommand(devEnableCmd, devDisableCmd, devSetPathCmd)
	devEnableCmd.Flags().StringVar(&devEnableProjectPath, "project-path", "", "Path to the local AEP source repo (built by `aep dev reload`)")
}

func runDevEnable(cmd *cobra.Command, args []string) error {
	if devEnableProjectPath != "" {
		if err := setProjectPath(devEnableProjectPath); err != nil {
			return err
		}
	}
	viper.Set("dev.enabled", true)
	cfgFile, err := saveConfig()
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Dev mode: enabled")
	if p := viper.GetString("dev.project_path"); p != "" {
		fmt.Fprintf(os.Stdout, "Project:  %s\n", p)
	} else {
		fmt.Fprintln(os.Stdout, "Project:  (not set) — run `aep dev set-path <dir>` before reloading")
	}
	fmt.Fprintf(os.Stdout, "Config:   %s\n", cfgFile)
	return nil
}

func runDevDisable(cmd *cobra.Command, args []string) error {
	viper.Set("dev.enabled", false)
	cfgFile, err := saveConfig()
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Dev mode: disabled")
	fmt.Fprintf(os.Stdout, "Config:   %s\n", cfgFile)
	return nil
}

func runDevSetPath(cmd *cobra.Command, args []string) error {
	if err := setProjectPath(args[0]); err != nil {
		return err
	}
	cfgFile, err := saveConfig()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Project: %s\n", viper.GetString("dev.project_path"))
	fmt.Fprintf(os.Stdout, "Config:  %s\n", cfgFile)
	return nil
}

// setProjectPath resolves path to an absolute path, validates it looks like the
// AEP repo root, and stores it in viper (persisted by the caller via saveConfig).
func setProjectPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	viper.Set("dev.project_path", abs)
	if _, err := devProjectRoot(); err != nil {
		return err
	}
	return nil
}
