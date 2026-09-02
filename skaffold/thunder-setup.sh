#!/bin/sh
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

# Registers all AEP OAuth clients in Thunder and patches its CORS config.
# Reads client secrets from the aep-thunder-secrets K8s Secret applied by
# skaffold/secrets.yaml. Called as a Skaffold post-Helm deploy hook.
# Idempotent: re-running is safe.
set -e

PLATFORM_NS="${PLATFORM_NS:-wso2-aep}"
THUNDER_NS="${THUNDER_NS:-thunder}"
THUNDER_DEPLOYMENT="${THUNDER_DEPLOYMENT:-thunder-deployment}"
THUNDER_CONFIG_MAP="${THUNDER_CONFIG_MAP:-thunder-config-map}"
CONSOLE_URL="${CONSOLE_URL:-http://console.openchoreo.localhost:8080}"

# PyYAML is required for the Thunder CORS config patch.
# Install it if missing rather than failing mid-hook after OAuth registration.
if ! python3 -c "import yaml" 2>/dev/null; then
  echo "PyYAML not found — installing..."
  python3 -m pip install --quiet pyyaml || { echo "ERROR: could not install PyYAML. Run: python3 -m pip install pyyaml"; exit 1; }
fi

AEP_THUNDER_SECRETS="aep-thunder-secrets"
AEP_THUNDER_ADMIN_CREDS="aep-thunder-admin-creds"

# ── Read secrets from cluster ────────────────────────────────────────────────
echo "Reading secrets from ${PLATFORM_NS}..."

secret_val() {
  kubectl get secret "$1" -n "${PLATFORM_NS}" -o "jsonpath={.data.${2}}" | base64 -d
}

THUNDER_ADMIN_CLIENT_ID=$(secret_val "${AEP_THUNDER_ADMIN_CREDS}" client-id)
THUNDER_ADMIN_CLIENT_SECRET=$(secret_val "${AEP_THUNDER_ADMIN_CREDS}" client-secret)

OC_WORKLOAD_PUBLISHER_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" OC_WORKLOAD_PUBLISHER_SECRET)
OC_OBSERVER_READER_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" OC_OBSERVER_READER_SECRET)
AEP_API_CLIENT_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" AEP_API_CLIENT_SECRET)
BFF_TO_GIT_SERVICE_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" BFF_TO_GIT_SERVICE_SECRET)
BFF_TO_REMOTE_WORKER_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" BFF_TO_REMOTE_WORKER_SECRET)
LOCAL_DEV_SEEDER_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" LOCAL_DEV_SEEDER_SECRET)
THUNDER_SYSTEM_CLIENT_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" THUNDER_SYSTEM_CLIENT_SECRET)
OC_RCA_AGENT_SECRET=$(secret_val "${AEP_THUNDER_SECRETS}" OC_RCA_AGENT_SECRET)

# ── Port-forward to Thunder ──────────────────────────────────────────────────
THUNDER_PF_PORT=18090
THUNDER_URL="http://localhost:${THUNDER_PF_PORT}"
THUNDER_PF_PID=""

cleanup() {
  [ -n "${THUNDER_PF_PID}" ] && { kill "${THUNDER_PF_PID}" 2>/dev/null || true; }
}
trap cleanup EXIT

kubectl port-forward -n "${THUNDER_NS}" svc/thunder-service \
  "${THUNDER_PF_PORT}:8090" >/dev/null 2>&1 &
THUNDER_PF_PID=$!

# ── Wait for Thunder readiness + authenticate ────────────────────────────────
# Poll the token endpoint — it's both the readiness probe and the auth step.
BEARER_TOKEN=""
TOKEN_RESP=""
for i in $(seq 1 30); do
  TOKEN_RESP=$(curl -s -X POST "${THUNDER_URL}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "grant_type=client_credentials" \
    --data-urlencode "client_id=${THUNDER_ADMIN_CLIENT_ID}" \
    --data-urlencode "client_secret=${THUNDER_ADMIN_CLIENT_SECRET}" \
    --data-urlencode "scope=system" 2>/dev/null || true)
  BEARER_TOKEN=$(echo "$TOKEN_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])" 2>/dev/null || true)
  [ -n "$BEARER_TOKEN" ] && break
  sleep 2
done
[ -z "$BEARER_TOKEN" ] && { echo "ERROR: Could not obtain Bearer token after 60s. Response: $TOKEN_RESP"; exit 1; }

# ── Fetch OU ID ──────────────────────────────────────────────────────────────
OU_RESP=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/organization-units/tree/default" 2>/dev/null || true)
OU_ID=$(echo "$OU_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
[ -z "$OU_ID" ] && { echo "ERROR: Could not fetch OU ID. Response: $OU_RESP"; exit 1; }

# ── Fetch auth flow ID ───────────────────────────────────────────────────────
FLOWS_RESP=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200" 2>/dev/null || true)
AUTH_FLOW_ID=$(echo "$FLOWS_RESP" | tr '\n' ' ' \
  | grep -o '"id":"[^"]*"[^}]*"handle":"default-basic-flow"' \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
[ -z "$AUTH_FLOW_ID" ] && { echo "ERROR: Could not find default-basic-flow. Response: $FLOWS_RESP"; exit 1; }

# ── Load existing apps ───────────────────────────────────────────────────────
APPS=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)
LAST_APP_ID=""

# find_app_id <json> <clientId> — wrapper-agnostic lookup (handles {"applications":[...]} wrapper)
find_app_id() {
  local json="$1" cid="$2"
  echo "$json" | sed 's/},{/}\n{/g' \
    | grep "\"clientId\":\"${cid}\"" \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true
}

# find_app_id_by_name <json> <name>
find_app_id_by_name() {
  local json="$1" name="$2"
  echo "$json" | sed 's/},{/}\n{/g' \
    | grep "\"name\":\"${name}\"" \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true
}

thunder_upsert_app() {
  local client_id="$1" payload="$2"
  local app_id resp http_code body
  app_id=$(find_app_id "$APPS" "$client_id")
  if [ -n "$app_id" ]; then
    resp=$(curl -s -X PUT \
      -H "Authorization: Bearer ${BEARER_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications/${app_id}" -w "\n%{http_code}" 2>/dev/null || true)
    http_code=$(echo "$resp" | tail -1)
    body=$(echo "$resp" | head -1)
    case "$http_code" in 2*) ;; *) echo "ERROR: Update failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
    LAST_APP_ID=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "$app_id")
  else
    resp=$(curl -s -X POST \
      -H "Authorization: Bearer ${BEARER_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications" -w "\n%{http_code}" 2>/dev/null || true)
    http_code=$(echo "$resp" | tail -1)
    body=$(echo "$resp" | head -1)
    if echo "$body" | grep -q "APP-1020"; then
      # APP-1020 = name or clientId already exists. Fetch fresh list and try by clientId first.
      local fresh_apps
      fresh_apps=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)
      app_id=$(find_app_id "$fresh_apps" "$client_id")
      if [ -z "$app_id" ]; then
        # Name conflict with a stale app (e.g. old setup-local.sh used a different clientId).
        # Find by name, delete the stale app, then create fresh with the correct clientId.
        local app_name stale_id
        app_name=$(echo "$payload" | python3 -c "import sys,json; print(json.load(sys.stdin).get('name',''))" 2>/dev/null || true)
        stale_id=$(find_app_id_by_name "$fresh_apps" "$app_name")
        if [ -n "$stale_id" ]; then
          echo "  replacing stale app for ${client_id} (name conflict, id=${stale_id})..."
          curl -s -X DELETE \
            -H "Authorization: Bearer ${BEARER_TOKEN}" \
            "${THUNDER_URL}/applications/${stale_id}" >/dev/null 2>&1 || true
          resp=$(curl -s -X POST \
            -H "Authorization: Bearer ${BEARER_TOKEN}" \
            -H "Content-Type: application/json" \
            -d "$payload" "${THUNDER_URL}/applications" -w "\n%{http_code}" 2>/dev/null || true)
          http_code=$(echo "$resp" | tail -1)
          body=$(echo "$resp" | head -1)
          case "$http_code" in 2*) ;; *) echo "ERROR: Create (after stale-delete) failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
        else
          echo "ERROR: APP-1020 but could not resolve conflict for ${client_id}"; exit 1
        fi
      else
        resp=$(curl -s -X PUT \
          -H "Authorization: Bearer ${BEARER_TOKEN}" \
          -H "Content-Type: application/json" \
          -d "$payload" "${THUNDER_URL}/applications/${app_id}" -w "\n%{http_code}" 2>/dev/null || true)
        http_code=$(echo "$resp" | tail -1)
        body=$(echo "$resp" | head -1)
        case "$http_code" in 2*) ;; *) echo "ERROR: Update (after APP-1020) failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
      fi
    else
      case "$http_code" in 2*) ;; *) echo "ERROR: Create failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
    fi
    LAST_APP_ID=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")
  fi
  echo "  ✓ ${client_id}"
}

thunder_confidential() {
  local name="$1" desc="$2" cid="$3" secret="$4"
  local payload
  payload=$(python3 -c "
import json, sys
name, desc, ou_id, cid, secret = sys.argv[1:]
print(json.dumps({
  'name': name, 'description': desc, 'ouId': ou_id,
  'inboundAuthConfig': [{'type': 'oauth2', 'config': {
    'clientId': cid, 'clientSecret': secret,
    'grantTypes': ['client_credentials'],
    'tokenEndpointAuthMethod': 'client_secret_post',
    'pkceRequired': False, 'publicClient': False,
    'token': {'accessToken': {'validityPeriod': 3600}}
  }}]
}))" "$name" "$desc" "$OU_ID" "$cid" "$secret")
  thunder_upsert_app "$cid" "$payload"
}

# ── Confidential clients ─────────────────────────────────────────────────────
echo "Registering AEP OAuth clients in Thunder..."
thunder_confidential "Workload Publisher"                  "OC Workload Publisher Client"              "openchoreo-workload-publisher-client"       "${OC_WORKLOAD_PUBLISHER_SECRET}"
thunder_confidential "OpenChoreo Observer Resource Reader" "BFF token for OC Observer service"         "openchoreo-observer-resource-reader-client" "${OC_OBSERVER_READER_SECRET}"
thunder_confidential "AEP API Service"                     "AEP API service-to-service client"         "aep-api-client"                             "${AEP_API_CLIENT_SECRET}"
thunder_confidential "AEP BFF to git-service"              "BFF outbound JWT, audience: git-service"   "bff-git-service"                            "${BFF_TO_GIT_SERVICE_SECRET}"
thunder_confidential "AEP BFF to remote-worker"            "BFF outbound JWT, audience: remote-worker" "bff-remote-worker"                          "${BFF_TO_REMOTE_WORKER_SECRET}"
thunder_confidential "AEP Local Dev Seeder"                "Local-dev convenience client"              "local-dev-seeder"                           "${LOCAL_DEV_SEEDER_SECRET}"
thunder_confidential "AEP System Client"                   "System-level Thunder admin client"         "aep-system-client"                          "${THUNDER_SYSTEM_CLIENT_SECRET}"
thunder_confidential "OpenChoreo RCA Agent"                "SRE/RCA agent service-account identity"    "openchoreo-rca-agent"                       "${OC_RCA_AGENT_SECRET}"

# ── Console PKCE client ──────────────────────────────────────────────────────
USER_ATTRS='["given_name","family_name","username","groups","ouId","ouName","ouHandle"]'
thunder_upsert_app "aep-console-client" "{
  \"name\":\"AEP Console\",\"description\":\"AEP Platform Console\",
  \"ouId\":\"$OU_ID\",\"authFlowId\":\"$AUTH_FLOW_ID\",
  \"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{
    \"clientId\":\"aep-console-client\",
    \"redirectUris\":[\"${CONSOLE_URL}\",\"${CONSOLE_URL}/\",\"${CONSOLE_URL}/callback\"],
    \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
    \"responseTypes\":[\"code\"],
    \"tokenEndpointAuthMethod\":\"none\",
    \"pkceRequired\":true,\"publicClient\":true,
    \"token\":{
      \"accessToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}},
      \"idToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}}
    }
  }}]}"

# ── CLI PKCE client ──────────────────────────────────────────────────────────
thunder_upsert_app "aep-cli-client" "{
  \"name\":\"AEP CLI\",\"description\":\"AEP CLI tool — PKCE login\",
  \"ouId\":\"$OU_ID\",\"authFlowId\":\"$AUTH_FLOW_ID\",
  \"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{
    \"clientId\":\"aep-cli-client\",
    \"redirectUris\":[\"http://localhost\",\"http://127.0.0.1\"],
    \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
    \"responseTypes\":[\"code\"],
    \"tokenEndpointAuthMethod\":\"none\",
    \"pkceRequired\":true,\"publicClient\":true,
    \"token\":{\"accessToken\":{\"validityPeriod\":86400}}
  }}]}"

# ── Grant aep-system-client the Thunder 'system' permission ──────────────────
# POST /roles/{id}/assignments/add 500s (ROL-5000) when the target is an app,
# so we create the role with the assignment inline instead. Idempotent: skip
# when the role already exists.
APPS2=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)
SYS_APP_ID=$(find_app_id "$APPS2" "aep-system-client")
[ -z "$SYS_APP_ID" ] && { echo "ERROR: Could not resolve aep-system-client app id"; exit 1; }

# /roles rejects a limit param (ROL-1008) — fetch without one.
ROLES=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/roles" 2>/dev/null || true)
if echo "$ROLES" | sed 's/},{/}\n{/g' | grep -q '"name":"aep-system"'; then
  echo "  ✓ aep-system role exists (skipping)"
else
  SYS_RS_ID=$(curl -s -H "Authorization: Bearer ${BEARER_TOKEN}" "${THUNDER_URL}/resource-servers" 2>/dev/null | sed 's/},{/}\n{/g' \
    | grep '"identifier":"system"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
  [ -z "$SYS_RS_ID" ] && { echo "ERROR: Could not resolve system resource server"; exit 1; }
  GRANT_CODE=$(curl -s -X POST \
    -H "Authorization: Bearer ${BEARER_TOKEN}" \
    -H "Content-Type: application/json" -d "{
    \"name\": \"aep-system\",
    \"description\": \"Grants aep-system-client the Thunder 'system' permission (thunder-app operator).\",
    \"ouId\": \"${OU_ID}\",
    \"permissions\": [{\"resourceServerId\": \"${SYS_RS_ID}\", \"permissions\": [\"system\"]}],
    \"assignments\": [{\"id\": \"${SYS_APP_ID}\", \"type\": \"app\"}]
  }" "${THUNDER_URL}/roles" -o /dev/null -w "%{http_code}" 2>/dev/null || true)
  case "$GRANT_CODE" in
    200|201) echo "  ✓ aep-system role created" ;;
    *) echo "ERROR: Failed to create aep-system role (HTTP ${GRANT_CODE})"; exit 1 ;;
  esac
fi

echo "AEP Thunder OAuth clients registered"

# ── Thunder CORS patch ────────────────────────────────────────────────────────
# Outputs "UNCHANGED" when the console URL is already in allowed_origins so
# we can skip the ConfigMap patch and avoid restarting Thunder unnecessarily.
# Restarting on every skaffold dev iteration interrupts active sessions.
echo "Patching Thunder CORS for ${CONSOLE_URL}..."

CURRENT_YAML=$(kubectl get configmap "${THUNDER_CONFIG_MAP}" \
  -n "${THUNDER_NS}" -o jsonpath='{.data.deployment\.yaml}')

PATCHED_YAML=$(echo "${CURRENT_YAML}" | python3 -c "
import sys, yaml
cfg = yaml.safe_load(sys.stdin.read()) or {}
origins = cfg.setdefault('cors', {}).setdefault('allowed_origins', [])
url = '${CONSOLE_URL}'
if url in origins:
    print('UNCHANGED')
else:
    origins.append(url)
    print(yaml.dump(cfg, default_flow_style=False))
")

if [ "${PATCHED_YAML}" = "UNCHANGED" ]; then
  echo "Thunder CORS already includes ${CONSOLE_URL} — skipping restart"
else
  ESCAPED=$(echo "${PATCHED_YAML}" | python3 -c "import sys, json; print(json.dumps(sys.stdin.read()))")
  kubectl patch configmap "${THUNDER_CONFIG_MAP}" \
    -n "${THUNDER_NS}" --type=merge \
    -p "{\"data\":{\"deployment.yaml\":${ESCAPED}}}"

  kubectl rollout restart deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}"
  kubectl rollout status deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" --timeout=120s >/dev/null

  echo "Thunder CORS updated"
fi
