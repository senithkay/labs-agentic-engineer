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

// Package dev holds the helpers for aepctl's local source-driven dev mode:
// building a service image from a local checkout, importing it into the k3d
// cluster, and persisting the image override so it survives helm upgrades.
package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildImage builds a service image from the local source tree.
// dockerfile and buildContext are paths relative to projectRoot (matching the
// CI image matrix). Build output is streamed to out.
func BuildImage(ctx context.Context, projectRoot, dockerfile, buildContext, tag string, out io.Writer) error {
	df := filepath.Join(projectRoot, dockerfile)
	cxt := filepath.Join(projectRoot, buildContext)
	fmt.Fprintf(out, "→ docker build -f %s -t %s %s\n", df, tag, cxt)
	c := exec.CommandContext(ctx, "docker", "build", "-f", df, "-t", tag, cxt)
	// BuildKit is required for the Dockerfiles' `--mount=type=cache` steps, which
	// keep dev rebuilds fast (skip reinstall / reuse the pnpm + Vite caches).
	c.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	c.Stdout = out
	c.Stderr = out
	if err := c.Run(); err != nil {
		return fmt.Errorf("docker build %s: %w", tag, err)
	}
	return nil
}

// ImportImage loads a locally-built image into the named k3d cluster's nodes so
// pods can run it without a registry pull (pullPolicy: Never).
func ImportImage(ctx context.Context, tag, cluster string, out io.Writer) error {
	fmt.Fprintf(out, "→ k3d image import %s -c %s\n", tag, cluster)
	c := exec.CommandContext(ctx, "k3d", "image", "import", tag, "-c", cluster)
	c.Stdout = out
	c.Stderr = out
	if err := c.Run(); err != nil {
		return fmt.Errorf("k3d image import %s: %w", tag, err)
	}
	return nil
}

// K3dClusterExists reports whether a k3d cluster with the given name exists.
func K3dClusterExists(ctx context.Context, cluster string) (bool, error) {
	out, err := exec.CommandContext(ctx, "k3d", "cluster", "list", "-o", "json").Output()
	if err != nil {
		return false, fmt.Errorf("k3d cluster list: %w", err)
	}
	var clusters []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &clusters); err != nil {
		return false, fmt.Errorf("parse k3d cluster list: %w", err)
	}
	for _, c := range clusters {
		if c.Name == cluster {
			return true, nil
		}
	}
	return false, nil
}
