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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestLoadFilePrecedence verifies the resolution order a --config file must
// obey for `install` and every other command: code default < config file <
// AEP_* env var. (A bound CLI flag sits above env; that binding is exercised
// by the command wiring, not this unit.)
func TestLoadFilePrecedence(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "aep.yaml")
	if err := os.WriteFile(cfg, []byte(`
oc:
  namespace: from-file
platform:
  version: 9.9.9
`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("default when nothing set", func(t *testing.T) {
		viper.Reset()
		Init()
		viper.SetDefault("oc.namespace", "openchoreo-system") // mirrors the flag default
		if got := viper.GetString("oc.namespace"); got != "openchoreo-system" {
			t.Fatalf("default: got %q, want openchoreo-system", got)
		}
	})

	t.Run("config file overrides default", func(t *testing.T) {
		viper.Reset()
		Init()
		viper.SetDefault("oc.namespace", "openchoreo-system")
		if err := LoadFile(cfg); err != nil {
			t.Fatal(err)
		}
		if got := viper.GetString("oc.namespace"); got != "from-file" {
			t.Fatalf("file over default: got %q, want from-file", got)
		}
		if got := viper.GetString("platform.version"); got != "9.9.9" {
			t.Fatalf("file value: got %q, want 9.9.9", got)
		}
	})

	t.Run("env var overrides config file", func(t *testing.T) {
		viper.Reset()
		Init() // sets AEP_ prefix + AutomaticEnv
		t.Setenv("AEP_OC_NAMESPACE", "from-env")
		if err := LoadFile(cfg); err != nil {
			t.Fatal(err)
		}
		if got := viper.GetString("oc.namespace"); got != "from-env" {
			t.Fatalf("env over file: got %q, want from-env", got)
		}
	})

	t.Run("missing file is an error", func(t *testing.T) {
		viper.Reset()
		Init()
		if err := LoadFile(filepath.Join(dir, "does-not-exist.yaml")); err == nil {
			t.Fatal("expected error for missing config file, got nil")
		}
	})
}
