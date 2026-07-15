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

package dev

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ValuesPath returns the path to the Helm values override file that persists
// dev image overrides (~/.aep/dev-values.yaml). `aep init`/upgrade pass this via
// `-f` when dev mode is enabled so reloaded images survive a reinstall.
func ValuesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".aep", "dev-values.yaml"), nil
}

// MergeImageOverride sets <valuesKey>.image = {repository, tag, pullPolicy} in
// the dev-values file, preserving any other services' overrides already present.
func MergeImageOverride(path, valuesKey, repository, tag, pullPolicy string) error {
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if root == nil {
			root = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	svc, _ := root[valuesKey].(map[string]any)
	if svc == nil {
		svc = map[string]any{}
	}
	svc["image"] = map[string]any{
		"repository": repository,
		"tag":        tag,
		"pullPolicy": pullPolicy,
	}
	root[valuesKey] = svc

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal dev values: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
