#!/usr/bin/env sh
# Updates all 0.0.0-dev placeholders to the target release version.
# Usage: update-versions.sh <target_version>   (e.g. 0.5.0 — no "v" prefix)
set -eu

TARGET="${1:?Usage: update-versions.sh <target_version>}"
VERSIONED="v${TARGET}"

CHART_DIR="deployments/helm-charts"

# ── Chart.yaml files ──────────────────────────────────────────────────────────
for f in \
  "${CHART_DIR}/wso2-agentic-engineer-bundle/Chart.yaml" \
  "${CHART_DIR}/wso2-ae-platform/Chart.yaml" \
  "${CHART_DIR}/wso2-ae-oc-extensions/Chart.yaml"
do
  sed -i "s/version: 0\.0\.0-dev/version: ${VERSIONED}/g" "$f"
  sed -i "s/appVersion: 0\.0\.0-dev/appVersion: \"${VERSIONED}\"/g" "$f"
done

# ── Sub-chart dependency versions in bundle Chart.yaml ────────────────────────
# The dependencies block references sub-charts by version — update those too.
sed -i "s/  version: 0\.0\.0-dev/  version: ${VERSIONED}/g" \
  "${CHART_DIR}/wso2-agentic-engineer-bundle/Chart.yaml"

# ── Image tags in values.yaml ─────────────────────────────────────────────────
sed -i "s/tag: 0\.0\.0-dev/tag: ${VERSIONED}/g" \
  "${CHART_DIR}/wso2-ae-platform/values.yaml"

echo "Updated all versions to ${VERSIONED}"
