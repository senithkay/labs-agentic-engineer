#!/bin/bash
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Tear down the full v1 dev environment.
# Usage: cd deployments && bash scripts/teardown.sh
#
# Equivalent of agent-manager/deployments/scripts/teardown.sh — stops the
# compose stack (with volumes) then deletes the k3d cluster. Does NOT touch
# Colima / Docker themselves.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

DEPLOY_DIR="$SCRIPT_DIR/.."
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.yml"

echo "=== Tearing Down App Factory v1 Environment ==="

# ── Stop OpenBao port-forward (started by start.sh as a fallback) ────────
echo ""
echo "1️⃣  Stop OpenBao port-forward"
if pgrep -f "port-forward.*openbao.*8200" > /dev/null 2>&1; then
    pkill -f "port-forward.*openbao.*8200" 2>/dev/null || true
    echo "✅ port-forward stopped"
else
    echo "⏭️  no openbao port-forward running"
fi

# ── Compose stack ────────────────────────────────────────────────────────
echo ""
echo "2️⃣  Stop Docker Compose services (with volumes)"
if [ -f "$COMPOSE_FILE" ]; then
    docker compose -f "$COMPOSE_FILE" down -v --remove-orphans
    echo "✅ Compose stack down"
else
    echo "⚠️  docker-compose.yml not found at $COMPOSE_FILE — skipping"
fi

# ── k3d cluster ──────────────────────────────────────────────────────────
echo ""
echo "3️⃣  Delete k3d cluster"
if command -v k3d &>/dev/null; then
    if k3d cluster list 2>/dev/null | grep -q "${CLUSTER_NAME}"; then
        k3d cluster delete "${CLUSTER_NAME}"
        echo "✅ Cluster deleted"
    else
        echo "⏭️  Cluster '${CLUSTER_NAME}' not found"
    fi
else
    echo "⚠️  k3d not installed — skipping"
fi

# ── Generated artifacts the operator typically wants gone ───────────────
echo ""
echo "4️⃣  Clean generated artifacts"
rm -f "$DEPLOY_DIR/.kube/config" 2>/dev/null && echo "   removed deployments/.kube/config" || true
rm -rf "$DEPLOY_DIR/.kube" 2>/dev/null || true

# asdlc-api stores cloned workspaces at the host bind mount declared in
# docker-compose.yml `volumes:` (REPO_BASE_PATH=/data/repos). compose down
# -v wipes named volumes but NOT bind mounts, so without this the next
# project create can hit stale workspace dirs.
# Read the host path from compose (currently ./data/repos) so this can't drift.
REPOS_HOST_PATH=$(awk '
  /^  asdlc-api:/        { in_svc=1; next }
  in_svc && /^  [a-z]/   { in_svc=0 }
  in_svc && /:\/data\/repos$/ {
    sub(/^[[:space:]]*-[[:space:]]*/, "")
    sub(/:\/data\/repos$/, "")
    print; exit
  }' "$COMPOSE_FILE")
# Expand ${HOME} (and similar) without sourcing the file.
REPOS_HOST_PATH="${REPOS_HOST_PATH//\$\{HOME\}/$HOME}"
REPOS_HOST_PATH="${REPOS_HOST_PATH//\$HOME/$HOME}"
if [[ "$REPOS_HOST_PATH" == ./* ]]; then
    REPOS_HOST_PATH="$DEPLOY_DIR/${REPOS_HOST_PATH#./}"
elif [[ -n "$REPOS_HOST_PATH" && "$REPOS_HOST_PATH" != /* ]]; then
    REPOS_HOST_PATH="$DEPLOY_DIR/$REPOS_HOST_PATH"
fi
if [ -n "$REPOS_HOST_PATH" ] && [ -d "$REPOS_HOST_PATH" ]; then
    rm -rf "$REPOS_HOST_PATH" 2>/dev/null \
        && echo "   removed repo workspaces at $REPOS_HOST_PATH" \
        || echo "   ⚠️  failed to remove $REPOS_HOST_PATH — clean it manually before next setup"
fi

echo "   keeping deployments/.env, deployments/keys/, deployments/github-app-private-key.pem"

echo ""
echo "✅ Teardown complete!"
echo "   Re-create with:  cd deployments && bash scripts/setup.sh && bash scripts/start.sh"
