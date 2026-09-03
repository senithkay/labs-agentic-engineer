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

# setup-dev.sh — creates a clean local OC dev cluster from scratch.
#
# Follows the OpenChoreo k3d docs (release-v1.2) for cluster and component
# versions, the Thunder v0.34 provisioning guide for auth, and installs the
# WSO2 API Platform gateway operator.
#
# Safe to re-run after an interrupted install: helm idempotency guards skip
# already-deployed releases, and all kubectl applies are dry-run safe.
#
# Prerequisites: k3d, helm, kubectl, docker (Colima), openssl, base64, python3
#
# Usage:
#   ./setup-dev.sh                       # create cluster named "openchoreo"
#   CLUSTER_NAME=mydev ./setup-dev.sh    # override cluster name

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

# ── Version pins (OC docs release-v1.2) ─────────────────────────────────────
# env.sh pins OPENCHOREO_VERSION=1.1.1 for the full platform setup;
# this script targets the OC release-v1.2 track for the dev cluster.
OC_VERSION="1.2.3"
THUNDER_RELEASE="release-v1.1"    # release-v1.1 base has scripts 50-55; override adds 56-60
GATEWAY_API_VERSION="v1.5.1"      # standard channel — OC v1.2.x docs
KGATEWAY_VERSION="v2.3.1"         # OC v1.2.x docs
API_PLATFORM_OPERATOR_VERSION="0.6.0"
# Use the version tag (not the branch) for raw GitHub URLs — the branch path
# layout differs from the tagged release layout for some files.
OC_RAW="https://raw.githubusercontent.com/openchoreo/openchoreo/v${OC_VERSION}"

# Temp files to clean up on exit
_CLEANUP=()
_cleanup() { for f in "${_CLEANUP[@]:-}"; do rm -f "$f"; done; }
trap _cleanup EXIT

echo "╔════════════════════════════════════════════════════╗"
echo "║    OpenChoreo Dev Cluster Setup (v${OC_VERSION})          ║"
echo "╚════════════════════════════════════════════════════╝"
echo ""
echo "  Cluster:    ${CLUSTER_NAME}  (${CLUSTER_CONTEXT})"
echo "  OC:         ${OC_VERSION}"
echo "  Thunder:    ${THUNDER_VERSION}"
echo "  kgateway:   ${KGATEWAY_VERSION}"
echo ""

# ============================================================================
# 0. Preflight
# ============================================================================
echo "0️⃣  Preflight checks"

if ! docker info &>/dev/null; then
    echo "⚠️  Docker not running — starting Colima..."
    colima start
    sleep 5
    if ! docker info &>/dev/null; then
        echo "❌ Docker still not available after starting Colima"
        exit 1
    fi
fi

if k3d cluster list 2>/dev/null | grep -qE "^${CLUSTER_NAME}[[:space:]]"; then
    echo "❌ Cluster '${CLUSTER_NAME}' already exists."
    echo "   Delete it first: k3d cluster delete ${CLUSTER_NAME}"
    exit 1
fi

check_required_ports
echo ""

# ============================================================================
# 1. k3d cluster
# ============================================================================
echo "1️⃣  Creating k3d cluster (${CLUSTER_NAME})"
# k3d resolves `files:` sources relative to the config file's directory, so
# stage the config alongside k3s-resolv.conf (which it references) before
# running k3d create. This is the same staging pattern as setup-k3d.sh.
_K3D_STAGE=$(mktemp -d)
cp "${SCRIPT_DIR}/../k3d-local-config.yaml" "${_K3D_STAGE}/k3d-local-config.yaml"
cp "${SCRIPT_DIR}/../k3s-resolv.conf"       "${_K3D_STAGE}/k3s-resolv.conf"
# K3D_FIX_DNS=0 prevents k3d from modifying /etc/resolv.conf on the host,
# which is required when running on Colima (macOS VM-based Docker).
( cd "${_K3D_STAGE}" && K3D_FIX_DNS=0 k3d cluster create "${CLUSTER_NAME}" \
    --config "${_K3D_STAGE}/k3d-local-config.yaml" )
rm -rf "${_K3D_STAGE}"
refresh_kubeconfig
wait_for_cluster
kubectl config use-context "${CLUSTER_CONTEXT}"
fix_node_dns
generate_machine_ids "${CLUSTER_NAME}"
echo "✅ Cluster ready"
echo ""

# ============================================================================
# 2. CoreDNS
# ============================================================================
echo "2️⃣  CoreDNS"
# Apply OC's coredns-custom.yaml which creates the ConfigMap that the main
# Corefile imports via `import /etc/coredns/custom/*.server`.
kubectl apply -f "${OC_RAW}/install/k3d/common/coredns-custom.yaml" &>/dev/null
# Extend the custom ConfigMap so host.k3d.internal resolves from pods
# AND both *.openchoreo.localhost / *.openchoreoapis.localhost resolve to
# the kgateway Service FQDN (not host.k3d.internal, which is unreachable
# inside the `.:53` block).
ensure_host_k3d_internal_in_coredns
ensure_openchoreo_localhost_in_coredns
echo ""

# ============================================================================
# 3. Gateway API CRDs (standard channel)
# ============================================================================
echo "3️⃣  Gateway API CRDs (standard ${GATEWAY_API_VERSION})"
kubectl apply --server-side --force-conflicts \
    -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/standard-install.yaml" \
    &>/dev/null
echo "✅ Gateway API CRDs applied"
echo ""

# ============================================================================
# 4. cert-manager
# ============================================================================
echo "4️⃣  cert-manager"
helm_install_if_not_exists "cert-manager" "cert-manager" \
    "oci://quay.io/jetstack/charts/cert-manager" \
    --version v1.19.4 --set crds.enabled=true
kubectl wait --for=condition=Available deployment/cert-manager \
    -n cert-manager --context "${CLUSTER_CONTEXT}" --timeout=120s
echo "✅ cert-manager ready"
echo ""

# ============================================================================
# 5. External Secrets Operator
# ============================================================================
echo "5️⃣  External Secrets Operator"
helm_install_if_not_exists "external-secrets" "external-secrets" \
    "oci://ghcr.io/external-secrets/charts/external-secrets" \
    --version 2.0.1 --set installCRDs=true
kubectl wait --for=condition=Available deployment --all \
    -n external-secrets --context "${CLUSTER_CONTEXT}" --timeout=180s
echo "✅ External Secrets Operator ready"
echo ""

# ============================================================================
# 6. kgateway
# ============================================================================
echo "6️⃣  kgateway ${KGATEWAY_VERSION}"
helm_install_if_not_exists "kgateway-crds" "openchoreo-control-plane" \
    "oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds" \
    --version "${KGATEWAY_VERSION}"
helm_install_if_not_exists "kgateway" "openchoreo-control-plane" \
    "oci://cr.kgateway.dev/kgateway-dev/charts/kgateway" \
    --version "${KGATEWAY_VERSION}" \
    --set controller.extraEnv.KGW_ENABLE_GATEWAY_API_EXPERIMENTAL_FEATURES=true
echo "✅ kgateway installed"
echo ""

# ============================================================================
# 7. OpenBao
# ============================================================================
echo "7️⃣  OpenBao 0.25.6"
helm_install_if_not_exists "openbao" "openbao" \
    "oci://ghcr.io/openbao/charts/openbao" \
    --version 0.25.6 \
    --values "${SCRIPT_DIR}/../single-cluster/values-openbao.yaml" \
    --set "server.service.type=NodePort" \
    --set "server.service.nodePort=30820"
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=openbao \
    -n openbao --context "${CLUSTER_CONTEXT}" --timeout=120s

# Wait for the postStart hook to finish seeding (it enables kubernetes auth
# and creates the reader/writer policies and roles).
echo "⏳ Waiting for OpenBao postStart to seed kubernetes auth..."
_bao_seeded=0
for i in $(seq 1 60); do
    if kubectl exec -n openbao --context "${CLUSTER_CONTEXT}" openbao-0 -- \
        sh -c 'BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=root bao auth list 2>/dev/null | grep -q kubernetes'; then
        _bao_seeded=1
        break
    fi
    sleep 3
done
if [ "$_bao_seeded" -eq 0 ]; then
    echo "❌ OpenBao kubernetes auth not enabled after 3 minutes"
    exit 1
fi
echo "✅ OpenBao ready"
echo ""

# ============================================================================
# 8. ClusterSecretStore (ESO → OpenBao)
# ============================================================================
echo "8️⃣  ClusterSecretStore"
# The external-secrets-openbao SA in the openbao namespace is used by ESO
# to authenticate against OpenBao via kubernetes auth. The writer role
# (bound to bound_service_account_namespaces=openbao) allows this SA to
# read/write secret/* paths.
kubectl --context "${CLUSTER_CONTEXT}" apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-secrets-openbao
  namespace: openbao
---
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: default
spec:
  provider:
    vault:
      server: "http://openbao.openbao.svc:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "openchoreo-secret-writer-role"
          serviceAccountRef:
            name: "external-secrets-openbao"
            namespace: "openbao"
EOF
echo "✅ ClusterSecretStore configured"
echo ""

# ============================================================================
# 9. WSO2 API Platform
# ============================================================================
echo "9️⃣  WSO2 API Platform operator v${API_PLATFORM_OPERATOR_VERSION}"
helm_install_if_not_exists "api-platform-operator" "openchoreo-data-plane" \
    "oci://ghcr.io/wso2/api-platform/helm-charts/gateway-operator" \
    --version "${API_PLATFORM_OPERATOR_VERSION}" \
    --set gatewayApi.installStandardCRDs=false \
    --values "${SCRIPT_DIR}/../manifests/api-platform/operator-values.yaml"

# The gateway controller v1.0.0 reads a raw 32-byte AES-GCM key from a
# mounted Secret. Using --from-literal would store the base64-encoded string
# (44 bytes), causing the controller to crash with "invalid key size: expected
# 32 bytes, got 44 bytes". Use --from-file to store raw bytes.
if ! kubectl --context "${CLUSTER_CONTEXT}" get secret \
    api-platform-controller-aesgcm-key -n openchoreo-data-plane &>/dev/null; then
    _aesgcm_tmp=$(mktemp)
    _CLEANUP+=("$_aesgcm_tmp")
    openssl rand 32 > "${_aesgcm_tmp}"
    kubectl --context "${CLUSTER_CONTEXT}" create secret generic \
        api-platform-controller-aesgcm-key \
        -n openchoreo-data-plane \
        --from-file="default-aesgcm256-v1.bin=${_aesgcm_tmp}"
    echo "✅ AES-GCM key provisioned (32 raw bytes)"
fi

kubectl --context "${CLUSTER_CONTEXT}" apply \
    -f "${SCRIPT_DIR}/../manifests/api-platform/gateway-config.yaml"
kubectl --context "${CLUSTER_CONTEXT}" apply \
    -f "${SCRIPT_DIR}/../manifests/api-platform/rbac.yaml"
kubectl --context "${CLUSTER_CONTEXT}" apply \
    -f "${SCRIPT_DIR}/../manifests/api-platform/api-gateway.yaml"
echo "✅ API Platform configured"
echo ""

# ============================================================================
# 10. CloudNativePG
# ============================================================================
echo "🔟  CloudNativePG"
helm_install_if_not_exists "cnpg" "cnpg-system" \
    "oci://ghcr.io/cloudnative-pg/charts/cloudnative-pg" \
    --version "${CNPG_VERSION}"
kubectl wait --for=condition=Available deployment --all \
    -n cnpg-system --context "${CLUSTER_CONTEXT}" --timeout=120s
echo "✅ CloudNativePG ready"
echo ""

# ============================================================================
# 11. Thunder v0.34
# ============================================================================
echo "1️⃣1️⃣  Thunder ${THUNDER_VERSION}"
if ! helm_release_deployed thunder thunder; then
    _thunder_override=$(mktemp -t thunder-override.XXXXXX.yaml)
    _CLEANUP+=("$_thunder_override")
    # Write override with single-quoted heredoc to prevent shell variable expansion.
    # The bootstrap scripts inside use ${THUNDER_API_BASE:-...} which must reach
    # Thunder as literal bash variables, not be expanded here.
    cat > "${_thunder_override}" << 'THUNDER_OVERRIDE_EOF'
# Thunder v0.34 override atop the release-v1.1 values-thunder.yaml base.
#
# Breaking changes v0.28 → v0.34:
#   1. sqlite config/runtime/user paths are now under nested sqlite.* keys
#   2. Consent service is new in v0.34, defaults to postgres — override to sqlite
#   3. readOnlyRootFilesystem must be false (sqlite writes to the filesystem)
#   4. All API fields renamed from snake_case to camelCase
#   5. ouId is now REQUIRED in POST /applications

deployment:
  replicaCount: 1
  securityContext:
    readOnlyRootFilesystem: false

hpa:
  enabled: false

configuration:
  database:
    config:
      type: sqlite
      sqlite:
        path: "repository/database/configdb.db"
        options: "_journal_mode=WAL&_busy_timeout=5000&_pragma=foreign_keys(1)"
    runtime:
      type: sqlite
      sqlite:
        path: "repository/database/runtimedb.db"
        options: "_journal_mode=WAL&_busy_timeout=5000&_pragma=foreign_keys(1)"
    user:
      type: sqlite
      sqlite:
        path: "repository/database/userdb.db"
        options: "_journal_mode=WAL&_busy_timeout=5000&_pragma=foreign_keys(1)"
  consent:
    database:
      type: sqlite
      sqlitePath: "repository/database/consentdb.db"
      sqliteOptions: "_pragma=journal_mode(WAL)&_pragma=cache_size(-16000)"

bootstrap:
  scripts:
    50-user-schema-and-users.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      log_info "Checking if default organization unit exists..."
      if ! curl -ks --max-time 10 "${BASE}/organization-units" | grep -q '"handle" *: *"default"'; then
        log_info "Creating default organization unit..."
        curl -ks --location "${BASE}/organization-units" \
          --header 'Content-Type: application/json' \
          --header 'Accept: application/json' \
          --data '{
            "name": "Default",
            "handle": "default",
            "description": "Default organizational unit"
          }' \
          --fail-with-body --max-time 30 --retry 3 --retry-delay 5
        log_info "Default organization unit created successfully"
      else
        log_info "Default organization unit already exists"
      fi
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      if [ -z "$ORG_UNIT_ID" ]; then
        log_error "Failed to resolve default organization unit ID from ${BASE}"
        exit 1
      fi
      log_info "Using organization unit ID: $ORG_UNIT_ID"
      log_info "Checking if user schema 'openchoreo-user' already exists..."
      existing_schemas=$(curl -ks --max-time 10 "${BASE}/user-schemas")
      if echo "$existing_schemas" | grep -q '"name" *: *"openchoreo-user"'; then
        log_info "User schema 'openchoreo-user' already exists, skipping creation"
      else
        log_info "Creating user schema..."
        curl -ks --location "${BASE}/user-schemas" \
          --header 'accept: application/json' \
          --header 'Content-Type: application/json' \
          --data "{
            \"name\": \"openchoreo-user\",
            \"ouId\": \"$ORG_UNIT_ID\",
            \"allowSelfRegistration\": true,
            \"systemAttributes\": {\"display\": \"username\"},
            \"schema\": {
              \"username\": {\"type\": \"string\", \"required\": true, \"displayName\": \"Username\"},
              \"password\": {\"type\": \"string\", \"required\": true, \"credential\": true, \"displayName\": \"Password\"},
              \"given_name\": {\"type\": \"string\", \"required\": true, \"displayName\": \"First Name\"},
              \"family_name\": {\"type\": \"string\", \"required\": true, \"displayName\": \"Last Name\"},
              \"email\": {\"type\": \"string\", \"required\": true, \"unique\": true, \"displayName\": \"Email\"}
            }
          }" \
          --fail-with-body --max-time 30 --retry 3 --retry-delay 5
        log_info "User schema created successfully"
      fi
      ensure_user() {
        local username="$1" password="$2" given_name="$3" family_name="$4"
        local existing_users user_id response
        existing_users=$(curl -ks --max-time 10 "${BASE}/users")
        if echo "$existing_users" | grep -q "\"username\" *: *\"${username}\""; then
          log_info "User '${username}' already exists, getting ID..." >&2
          user_id=$(echo "$existing_users" | tr '\n' ' ' | sed 's/" *: *"/":"/g' \
            | grep -o "\"username\":\"${username}\"[^}]*\"id\":\"[^\"]*\"\|\"id\":\"[^\"]*\"[^}]*\"username\":\"${username}\"" \
            | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
        else
          log_info "Creating user '${username}'..." >&2
          response=$(curl -ks --location "${BASE}/users" \
            --header 'accept: application/json' \
            --header 'Content-Type: application/json' \
            --data "{
              \"type\": \"openchoreo-user\",
              \"ouId\": \"$ORG_UNIT_ID\",
              \"attributes\": {
                \"username\": \"${username}\",
                \"email\": \"${username}\",
                \"password\": \"${password}\",
                \"given_name\": \"${given_name}\",
                \"family_name\": \"${family_name}\"
              }
            }" \
            --fail-with-body --max-time 30 --retry 3 --retry-delay 5)
          log_info "User '${username}' created successfully" >&2
          user_id=$(echo "$response" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
        fi
        echo "$user_id"
      }
      ensure_group() {
        local group_name="$1" description="$2" member_id="$3"
        existing_groups=$(curl -ks --max-time 10 "${BASE}/groups")
        if echo "$existing_groups" | grep -q "\"name\" *: *\"${group_name}\""; then
          log_info "Group '${group_name}' already exists, skipping creation"
          return
        fi
        local group_members=""
        if [ -n "$member_id" ]; then
          group_members="{\"type\":\"user\",\"id\":\"${member_id}\"}"
        fi
        log_info "Creating group '${group_name}'..."
        curl -ks --location "${BASE}/groups" \
          --header 'accept: application/json' \
          --header 'Content-Type: application/json' \
          --data "{
            \"name\": \"${group_name}\",
            \"ouId\": \"$ORG_UNIT_ID\",
            \"description\": \"${description}\",
            \"members\": [${group_members}]
          }" \
          --fail-with-body --max-time 30 --retry 3 --retry-delay 5
        log_info "Group '${group_name}' created successfully"
      }
      ADMIN_ID=$(ensure_user     "admin@openchoreo.dev"            "Admin@123" "Admin"    "User")
      DEVELOPER_ID=$(ensure_user "developer@openchoreo.dev"        "Dev@123"   "Developer" "User")
      PE_ID=$(ensure_user        "platform-engineer@openchoreo.dev" "PE@123"   "Platform" "Engineer")
      SRE_ID=$(ensure_user       "sre@openchoreo.dev"              "SRE@123"   "SRE"      "User")
      ensure_group "admins"             "Openchoreo Admins group"             "$ADMIN_ID"
      ensure_group "developers"         "Openchoreo Developers group"         "$DEVELOPER_ID"
      ensure_group "platform-engineers" "Openchoreo Platform Engineers group" "$PE_ID"
      ensure_group "sres"               "Openchoreo SREs group"               "$SRE_ID"

    51-backstage-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"name":"Backstage"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"Backstage"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{
        \"name\": \"Backstage\",
        \"description\": \"OpenChoreo Backstage Portal\",
        \"ouId\": \"${ORG_UNIT_ID}\",
        \"logoUrl\": \"https://cdn.statically.io/gh/openchoreo/openchoreo.github.io@main/static/img/openchoreo-logo.png\",
        \"allowedUserTypes\": [\"openchoreo-user\"],
        \"assertion\": {\"validityPeriod\": 3600},
        \"inboundAuthConfig\": [{
          \"type\": \"oauth2\",
          \"config\": {
            \"clientId\": \"openchoreo-backstage-client\",
            \"clientSecret\": \"backstage-portal-secret\",
            \"redirectUris\": [\"http://openchoreo.localhost:8080/api/auth/openchoreo-auth/handler/frame\"],
            \"grantTypes\": [\"authorization_code\", \"client_credentials\", \"refresh_token\"],
            \"responseTypes\": [\"code\"],
            \"tokenEndpointAuthMethod\": \"client_secret_post\",
            \"pkceRequired\": false,
            \"publicClient\": false,
            \"token\": {
              \"accessToken\": {\"validityPeriod\": 86400, \"userAttributes\": [\"given_name\",\"family_name\",\"username\",\"groups\"]},
              \"idToken\": {\"validityPeriod\": 86400, \"userAttributes\": [\"given_name\",\"family_name\",\"username\",\"groups\"]}
            },
            \"scopeClaims\": {\"email\": [\"email\"], \"groups\": [\"groups\"], \"profile\": [\"username\",\"given_name\",\"family_name\",\"picture\"]}
          }
        }]
      }"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    52-default-apps.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"clientId":"customer-portal-client"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"clientId":"customer-portal-client"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"Customer Portal\",\"description\":\"Customer Portal Application\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"customer-portal-client\",\"clientSecret\":\"supersecret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    53-rca-agent-client.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"clientId":"openchoreo-rca-agent"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"clientId":"openchoreo-rca-agent"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"RCA Agent\",\"description\":\"OpenChoreo RCA Agent Client\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-rca-agent\",\"clientSecret\":\"openchoreo-rca-agent-secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    54-cli-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"name":"OpenChoreo CLI"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"OpenChoreo CLI"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"OpenChoreo CLI\",\"description\":\"OpenChoreo CLI Default Application\",\"ouId\":\"${ORG_UNIT_ID}\",\"logoUrl\":\"https://cdn.statically.io/gh/openchoreo/openchoreo.github.io@main/static/img/openchoreo-logo.png\",\"allowedUserTypes\":[\"openchoreo-user\"],\"assertion\":{\"validityPeriod\":3600},\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-cli\",\"redirectUris\":[\"http://127.0.0.1:55152/auth-callback\"],\"grantTypes\":[\"authorization_code\",\"refresh_token\"],\"responseTypes\":[\"code\"],\"tokenEndpointAuthMethod\":\"none\",\"pkceRequired\":true,\"publicClient\":true,\"token\":{\"accessToken\":{\"validityPeriod\":3600,\"userAttributes\":[\"given_name\",\"family_name\",\"username\",\"groups\"]},\"idToken\":{\"validityPeriod\":3600,\"userAttributes\":[\"given_name\",\"family_name\",\"username\",\"groups\"]}},\"scopeClaims\":{\"email\":[\"email\"],\"groups\":[\"groups\"],\"profile\":[\"username\",\"given_name\",\"family_name\",\"picture\",\"groups\"]}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    55-system-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"clientId":"openchoreo-system-app"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"clientId":"openchoreo-system-app"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"System Application\",\"description\":\"Generic system application for automation and integrations\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-system-app\",\"clientSecret\":\"openchoreo-system-app-secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    56-user-mcp-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"name":"User MCP App"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"User MCP App"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"User MCP App\",\"description\":\"User MCP app to be used by terminals and user facing AI agents\",\"ouId\":\"${ORG_UNIT_ID}\",\"allowedUserTypes\":[\"openchoreo-user\"],\"assertion\":{\"validityPeriod\":3600},\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"user_mcp_client\",\"redirectUris\":[\"http://localhost:8075/callback\",\"cursor://anysphere.cursor-mcp/oauth/callback\",\"http://127.0.0.1:19876/mcp/oauth/callback\"],\"grantTypes\":[\"authorization_code\",\"refresh_token\"],\"responseTypes\":[\"code\"],\"tokenEndpointAuthMethod\":\"none\",\"pkceRequired\":true,\"publicClient\":true,\"token\":{\"accessToken\":{\"validityPeriod\":86400,\"userAttributes\":[\"given_name\",\"family_name\",\"username\",\"groups\"]},\"idToken\":{\"validityPeriod\":86400,\"userAttributes\":[\"given_name\",\"family_name\",\"username\",\"groups\"]}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    57-service-mcp-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"name":"Service MCP App"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"Service MCP App"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"Service MCP App\",\"description\":\"Service MCP app to be used by backend services which can securely store the secret\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"service_mcp_client\",\"clientSecret\":\"service_mcp_client_secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_basic\",\"token\":{\"accessToken\":{\"validityPeriod\":86400}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    58-workload-publisher-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"clientId":"openchoreo-workload-publisher-client"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"clientId":"openchoreo-workload-publisher-client"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"Workload Publisher\",\"description\":\"OpenChoreo Workload Publisher Client for creating workloads from CI workflows\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-workload-publisher-client\",\"clientSecret\":\"openchoreo-workload-publisher-secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    59-openchoreo-observer-app.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"name":"OpenChoreo Observer Resource Reader"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"OpenChoreo Observer Resource Reader"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"OpenChoreo Observer Resource Reader\",\"description\":\"OpenChoreo Observer Resource Reader Client\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-observer-resource-reader-client\",\"clientSecret\":\"openchoreo-observer-resource-reader-client-secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi

    60-finops-agent-client.sh: |
      #!/bin/bash
      set -e
      BASE="${THUNDER_API_BASE:-https://localhost:8090}"
      ORG_UNIT_ID=$(curl -ks --max-time 10 "${BASE}/organization-units/tree/default" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      [ -z "$ORG_UNIT_ID" ] && { log_error "Failed to resolve ORG_UNIT_ID"; exit 1; }
      existing_apps=$(curl -ks --max-time 10 "${BASE}/applications")
      app_id=$(echo "$existing_apps" | tr '\n' ' ' | sed 's/" *: *"/":"/g' | grep -o '"clientId":"openchoreo-finops-agent"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"clientId":"openchoreo-finops-agent"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
      APP_PAYLOAD="{\"name\":\"FinOps Agent\",\"description\":\"OpenChoreo FinOps Agent Client\",\"ouId\":\"${ORG_UNIT_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"openchoreo-finops-agent\",\"clientSecret\":\"openchoreo-finops-agent-secret\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
      if [ -n "$app_id" ]; then
        curl -ks --location -X PUT "${BASE}/applications/$app_id" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      else
        curl -ks --location "${BASE}/applications" -H 'Content-Type: application/json' --data "$APP_PAYLOAD" --fail-with-body --max-time 30
      fi
THUNDER_OVERRIDE_EOF

    echo "📦 Installing Thunder ${THUNDER_VERSION}..."
    helm upgrade --install thunder \
        oci://ghcr.io/asgardeo/helm-charts/thunder \
        --version "${THUNDER_VERSION}" \
        --namespace thunder --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}" \
        -f "https://raw.githubusercontent.com/openchoreo/openchoreo/${THUNDER_RELEASE}/install/k3d/common/values-thunder.yaml" \
        -f "${_thunder_override}" \
        --timeout 15m
fi
echo "⏳ Waiting for Thunder deployments..."
kubectl wait --for=condition=Available deployment --all \
    -n thunder --context "${CLUSTER_CONTEXT}" --timeout=300s
echo "✅ Thunder ready"
echo ""

# ============================================================================
# 12. Control Plane
# ============================================================================
echo "1️⃣2️⃣  Control Plane v${OC_VERSION}"

# Load PUBLIC_THUNDER_URL / PUBLIC_CONSOLE_URL for rendering values-cp.yaml.
# Falls back to local defaults when no .env is present (first install).
load_public_urls "${SCRIPT_DIR}/../.env"

# Create backstage-secrets via ExternalSecret so the values come from
# OpenBao (already seeded by its postStart hook) rather than hard-coded
# literals. Wait for the sync before installing the control plane so
# the Backstage pod can mount the secret.
kubectl create namespace openchoreo-control-plane \
    --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f -
if ! kubectl --context "${CLUSTER_CONTEXT}" get secret backstage-secrets \
    -n openchoreo-control-plane &>/dev/null; then
    echo "🔑 Provisioning backstage-secrets via ExternalSecret..."
    kubectl --context "${CLUSTER_CONTEXT}" apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: backstage-secrets
  namespace: openchoreo-control-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: default
    kind: ClusterSecretStore
  target:
    name: backstage-secrets
    creationPolicy: Owner
  data:
    - secretKey: backend-secret
      remoteRef:
        key: backstage-backend-secret
        property: value
    - secretKey: client-secret
      remoteRef:
        key: backstage-client-secret
        property: value
    - secretKey: jenkins-api-key
      remoteRef:
        key: backstage-jenkins-api-key
        property: value
EOF
    echo "⏳ Waiting for backstage-secrets to sync from OpenBao..."
    for i in $(seq 1 60); do
        if kubectl --context "${CLUSTER_CONTEXT}" get secret backstage-secrets \
            -n openchoreo-control-plane &>/dev/null; then
            break
        fi
        sleep 3
    done
    kubectl --context "${CLUSTER_CONTEXT}" get secret backstage-secrets \
        -n openchoreo-control-plane &>/dev/null || {
        echo "❌ backstage-secrets failed to sync from OpenBao"
        echo "   Check: kubectl get externalsecret backstage-secrets -n openchoreo-control-plane"
        exit 1
    }
    echo "✅ backstage-secrets synced"
fi

RENDERED_CP_VALUES="$(render_values_file "${SCRIPT_DIR}/../single-cluster/values-cp.yaml")"
_CLEANUP+=("$RENDERED_CP_VALUES")

# The control plane install races its own validating webhook: the chart
# applies webhook-gated CRs (ClusterAuthzRoleBinding) in the same helm
# pass that first creates the controller-manager pod. On a cold image pull
# the webhook service has no endpoints yet and helm errors, leaving the
# release in `failed`. Retry once the webhook endpoints are healthy.
if ! helm_release_deployed openchoreo-control-plane openchoreo-control-plane; then
    echo "📦 Installing OpenChoreo Control Plane (may take up to 10 minutes)..."
    _cp_attempt=0
    until helm upgrade --install openchoreo-control-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-control-plane \
        --version "${OC_VERSION}" \
        --namespace openchoreo-control-plane --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}" \
        --values "${RENDERED_CP_VALUES}"; do
        _cp_attempt=$((_cp_attempt + 1))
        if [ "$_cp_attempt" -ge 3 ]; then
            echo "❌ Control Plane install failed after ${_cp_attempt} attempts"
            exit 1
        fi
        echo "⚠️  Attempt ${_cp_attempt} hit the webhook race — waiting for controller-manager..."
        kubectl wait -n openchoreo-control-plane --for=condition=Available --timeout=300s \
            deployment/controller-manager --context "${CLUSTER_CONTEXT}" || true
        for _ in $(seq 1 30); do
            if [ -n "$(kubectl --context "${CLUSTER_CONTEXT}" get endpoints \
                controller-manager-webhook-service -n openchoreo-control-plane \
                -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)" ]; then
                break
            fi
            sleep 5
        done
    done
fi

echo "⏳ Waiting for Control Plane core components..."
kubectl wait -n openchoreo-control-plane --for=condition=Available --timeout=300s \
    --context "${CLUSTER_CONTEXT}" \
    deployment/controller-manager \
    deployment/openchoreo-api \
    deployment/cluster-gateway \
    deployment/gateway-default
echo "✅ Control Plane ready"
echo ""

# ============================================================================
# 13. Default resources
# ============================================================================
echo "1️⃣3️⃣  Default resources (v${OC_VERSION})"
# Applies default Organization, Environment, Project, and ClusterWorkflowTemplates
# from the OC release-v1.2 single-cluster install bundle.
kubectl --context "${CLUSTER_CONTEXT}" apply \
    -f "${OC_RAW}/install/k3d/single-cluster/default-resources/all.yaml" || true
echo "✅ Default resources applied"
echo ""

# ============================================================================
# 14. Data Plane
# ============================================================================
echo "1️⃣4️⃣  Data Plane v${OC_VERSION}"
if ! helm_release_deployed openchoreo-data-plane openchoreo-data-plane; then
    kubectl --context "${CLUSTER_CONTEXT}" create namespace openchoreo-data-plane \
        --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f -

    # Wait for the control-plane cert-manager to issue the cluster-gateway-ca
    # Certificate, then copy the CA cert into the data-plane namespace as both
    # a Secret (needed by the data-plane chart's cert-manager Issuer to issue
    # the cluster-agent-tls cert) and a ConfigMap (used by the cluster agent
    # to verify the gateway TLS connection).
    echo "⏳ Waiting for cluster-gateway-ca certificate..."
    kubectl wait -n openchoreo-control-plane --for=condition=Ready \
        certificate/cluster-gateway-ca --timeout=120s --context "${CLUSTER_CONTEXT}"
    _dp_ca=$(kubectl --context "${CLUSTER_CONTEXT}" get secret cluster-gateway-ca \
        -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)

    kubectl --context "${CLUSTER_CONTEXT}" create secret generic cluster-gateway-ca \
        -n openchoreo-data-plane \
        --from-literal="ca.crt=${_dp_ca}" \
        --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f -
    kubectl --context "${CLUSTER_CONTEXT}" create configmap cluster-gateway-ca \
        -n openchoreo-data-plane \
        --from-literal="ca.crt=${_dp_ca}" \
        --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f -

    echo "📦 Installing OpenChoreo Data Plane..."
    helm upgrade --install openchoreo-data-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-data-plane \
        --version "${OC_VERSION}" \
        --namespace openchoreo-data-plane --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}" \
        --values "${SCRIPT_DIR}/../single-cluster/values-dp.yaml"
fi

echo "⏳ Waiting for Data Plane..."
kubectl wait -n openchoreo-data-plane --for=condition=Available --timeout=300s \
    deployment --all --context "${CLUSTER_CONTEXT}"
echo "✅ Data Plane ready"

if ! kubectl --context "${CLUSTER_CONTEXT}" get clusterdataplane default &>/dev/null; then
    echo "🔗 Registering Data Plane..."
    _dp_agent_ca=$(kubectl --context "${CLUSTER_CONTEXT}" get secret cluster-agent-tls \
        -n openchoreo-data-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl --context "${CLUSTER_CONTEXT}" apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterDataPlane
metadata:
  name: default
  namespace: default
spec:
  planeID: "default"
  secretStoreRef:
    name: "default"
  clusterAgent:
    clientCA:
      value: |
$(echo "$_dp_agent_ca" | sed 's/^/        /')
  gateway:
    ingress:
      external:
        name: gateway-default
        namespace: openchoreo-data-plane
        http:
          host: "openchoreoapis.localhost"
          listenerName: http
          port: 19080
        https:
          host: "openchoreoapis.localhost"
          listenerName: https
          port: 19443
EOF
    echo "✅ ClusterDataPlane registered"
fi

# After the data plane is connected, restart the API Platform gateway-runtime
# so it can establish a clean xDS connection to the controller.
echo "🔄 Restarting API Platform gateway-runtime for clean xDS connection..."
kubectl --context "${CLUSTER_CONTEXT}" rollout restart \
    deployment/api-platform-default-gateway-gateway-runtime \
    -n openchoreo-data-plane 2>/dev/null || true
echo ""

# ============================================================================
# 15. Workflow Plane
# ============================================================================
echo "1️⃣5️⃣  Workflow Plane v${OC_VERSION}"
if ! helm_release_deployed openchoreo-workflow-plane openchoreo-workflow-plane; then
    # create_plane_cert_resources creates the namespace + copies cluster-gateway-ca
    # as a ConfigMap only (workflow plane needs ConfigMap, not Secret).
    create_plane_cert_resources openchoreo-workflow-plane

    echo "📦 Installing workflow-plane container registry..."
    _wp_registry_values=$(mktemp)
    _CLEANUP+=("$_wp_registry_values")
    fetch_gh_raw \
        "${OC_RAW}/install/k3d/single-cluster/values-registry.yaml" \
        "${_wp_registry_values}"
    helm upgrade --install registry docker-registry \
        --repo https://twuni.github.io/docker-registry.helm \
        --namespace openchoreo-workflow-plane --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}" \
        --values "${_wp_registry_values}"

    echo "📦 Installing OpenChoreo Workflow Plane..."
    helm upgrade --install openchoreo-workflow-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-workflow-plane \
        --version "${OC_VERSION}" \
        --namespace openchoreo-workflow-plane --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}"
fi

echo "⏳ Waiting for Workflow Plane..."
kubectl wait -n openchoreo-workflow-plane --for=condition=Available --timeout=300s \
    deployment --all --context "${CLUSTER_CONTEXT}"
echo "✅ Workflow Plane ready"

if ! kubectl --context "${CLUSTER_CONTEXT}" get clusterworkflowplane default &>/dev/null; then
    echo "🔗 Registering Workflow Plane..."
    _wp_ca=$(kubectl --context "${CLUSTER_CONTEXT}" get secret cluster-agent-tls \
        -n openchoreo-workflow-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    register_workflow_plane "$_wp_ca" "default" "default"
fi

# Apply workflow templates for the build system (Argo Workflows templates
# used by the workflow-plane to run component builds).
echo "📋 Applying workflow templates..."
kubectl --context "${CLUSTER_CONTEXT}" apply \
    -f "${OC_RAW}/install/k3d/single-cluster/workflow-templates/" || true

# Configure containerd on all k3d nodes to mirror the workflow-plane registry
# by ClusterIP (kubelet cannot resolve Kubernetes service DNS).
configure_registry_mirror
echo ""

# ============================================================================
# 16. Post-install patches
# ============================================================================
echo "1️⃣6️⃣  Post-install patches"

# ── Thunder HTTPRoute CORS filter ────────────────────────────────────────────
# kgateway exposes HTTPRoute status under .status.parents[].conditions (Gateway
# API spec), NOT .status.conditions. `kubectl wait --for=condition=Accepted`
# reads the wrong path and always times out. Poll .status.parents[0] instead.
echo "⏳ Waiting for Thunder HTTPRoute to be accepted by kgateway..."
for _hr in $(seq 1 60); do
    if [ "$(kubectl --context "${CLUSTER_CONTEXT}" get httproute \
            -n thunder thunder-httproute \
            -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}' \
            2>/dev/null)" = "True" ]; then
        break
    fi
    sleep 2
done

echo "🔧 Patching Thunder HTTPRoute with CORS filter..."
_CORS_PATCH=$(cat <<EOF
[{"op":"replace","path":"/spec/rules/0/filters","value":[{"type":"CORS","cors":{"allowOrigins":["http://localhost:19080","http://*.openchoreoapis.localhost:19080","${PUBLIC_CONSOLE_URL}","${PUBLIC_THUNDER_URL}"],"allowMethods":["GET","POST","PUT","PATCH","DELETE","OPTIONS"],"allowHeaders":["Content-Type","Authorization","Accept","Origin"],"allowCredentials":true,"maxAge":3600}}]}]
EOF
)
_cors_applied=0
_cors_last_err=""
for _attempt in 1 2 3 4 5; do
    _cors_last_err=$(kubectl --context "${CLUSTER_CONTEXT}" patch httproute \
        -n thunder thunder-httproute --type=json -p="${_CORS_PATCH}" \
        2>&1 >/dev/null || true)
    if [ "$(kubectl --context "${CLUSTER_CONTEXT}" get httproute \
            -n thunder thunder-httproute \
            -o jsonpath='{.spec.rules[0].filters[0].type}' 2>/dev/null)" = "CORS" ]; then
        echo "✅ Thunder HTTPRoute CORS filter applied (attempt ${_attempt})"
        _cors_applied=1
        break
    fi
    echo "   attempt ${_attempt} did not land — retrying in 5s..."
    [ -n "$_cors_last_err" ] && echo "     ↳ ${_cors_last_err}"
    sleep 5
done
if [ "${_cors_applied}" -ne 1 ]; then
    echo "❌ Thunder HTTPRoute CORS patch failed after 5 attempts" >&2
    echo "   Console login will fail with CORS errors." >&2
    echo "   Inspect: kubectl get httproute -n thunder thunder-httproute -o yaml" >&2
    exit 1
fi

# ── Backstage in-cluster token URL ───────────────────────────────────────────
# The OC chart sets OPENCHOREO_AUTH_TOKEN_URL to the browser-facing Thunder
# hostname (thunder.openchoreo.localhost:8080), which is not resolvable from
# inside the cluster. Override it with the Thunder ClusterIP service address.
echo "🔧 Patching Backstage token URL to in-cluster Thunder service..."
kubectl --context "${CLUSTER_CONTEXT}" set env deployment/backstage \
    -n openchoreo-control-plane \
    OPENCHOREO_AUTH_TOKEN_URL="http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token"
kubectl --context "${CLUSTER_CONTEXT}" rollout status deployment/backstage \
    -n openchoreo-control-plane --timeout=120s
echo "✅ Backstage token URL patched"
echo ""

# ============================================================================
# 17. Summary
# ============================================================================
echo "╔════════════════════════════════════════════════════╗"
echo "║   Setup complete!                                  ║"
echo "╚════════════════════════════════════════════════════╝"
echo ""
echo "  Console:   http://openchoreo.localhost:8080"
echo "  Thunder:   http://thunder.openchoreo.localhost:8080"
echo "  OC API:    http://api.openchoreo.localhost:8080"
echo "  Data Plane: http://openchoreoapis.localhost:19080"
echo ""
echo "  Default users:"
echo "    admin@openchoreo.dev         / Admin@123"
echo "    developer@openchoreo.dev     / Dev@123"
echo "    platform-engineer@openchoreo.dev / PE@123"
echo "    sre@openchoreo.dev           / SRE@123"
echo ""
echo "📊 Pod status:"
for _ns in openchoreo-control-plane openchoreo-data-plane openchoreo-workflow-plane thunder; do
    echo "--- ${_ns} ---"
    kubectl get pods -n "${_ns}" --no-headers \
        --context "${CLUSTER_CONTEXT}" 2>/dev/null || echo "  (no pods)"
    echo ""
done
