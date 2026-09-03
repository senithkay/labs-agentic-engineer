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

# delete-dev.sh — destroys the dev cluster created by setup-dev.sh.
#
# Deletes the k3d cluster (all namespaces, PVCs, and volumes go with it)
# and removes any dangling port-forwards targeting the cluster API server.
#
# Usage:
#   ./delete-dev.sh          # prompts for confirmation
#   ./delete-dev.sh --force  # skips the prompt

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

FORCE=0
for arg in "$@"; do
    [ "$arg" = "--force" ] && FORCE=1
done

echo "╔════════════════════════════════════════════════════╗"
echo "║    Delete Dev Cluster — delete-dev.sh             ║"
echo "╚════════════════════════════════════════════════════╝"
echo ""
echo "  Cluster: ${CLUSTER_NAME}  (${CLUSTER_CONTEXT})"
echo ""
echo "  This will PERMANENTLY delete the k3d cluster and all its data."
echo "  Re-create with: ./setup-dev.sh"
echo ""

if [ "$FORCE" -eq 0 ]; then
    read -r -p "  Delete '${CLUSTER_NAME}'? [y/N] " _confirm
    case "$_confirm" in
        [yY][eE][sS]|[yY]) ;;
        *)
            echo "Aborted."
            exit 0
            ;;
    esac
fi

# ── 1. Kill stale port-forwards pointing at this cluster's API server ────────
echo "1️⃣  Stopping port-forwards..."
pkill -f "port-forward.*openbao.*8200" 2>/dev/null || true
pkill -f "kubectl.*port-forward.*${CLUSTER_NAME}" 2>/dev/null || true
echo "✅ Port-forwards cleared"

# ── 2. Delete k3d cluster ────────────────────────────────────────────────────
echo "2️⃣  Deleting k3d cluster '${CLUSTER_NAME}'..."
if k3d cluster list 2>/dev/null | grep -qE "^${CLUSTER_NAME}[[:space:]]"; then
    k3d cluster delete "${CLUSTER_NAME}"
    echo "✅ Cluster deleted"
else
    echo "⏭️  Cluster '${CLUSTER_NAME}' not found — nothing to delete"
fi

echo ""
echo "✅ Done. Re-create with: ./setup-dev.sh"
