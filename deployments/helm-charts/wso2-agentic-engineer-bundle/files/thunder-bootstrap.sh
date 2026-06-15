#!/bin/sh
# ASDLC Thunder OAuth client bootstrap — runs as a post-install Helm hook.
# Registers all ASDLC OAuth applications in OC's Thunder instance and assigns
# the system client to the Administrator role. Fully idempotent.
#
# Targets OC's bundled Thunder (NOT a separate AE Thunder instance).
# Uses the snake_case Thunder 0.34.0 API format.
#
# Required env vars (set by the Job template):
#   THUNDER_ADMIN_URL           — in-cluster OC Thunder base URL (no trailing slash)
#   THUNDER_ADMIN_CLIENT_ID     — OC Thunder admin client (e.g. openchoreo-system-app)
#   THUNDER_ADMIN_CLIENT_SECRET — OC Thunder admin client secret
#   THUNDER_OU_HANDLE           — organisation unit handle (e.g. "default")
#   THUNDER_SYSTEM_CLIENT_ID    — ASDLC system client ID to register
#   THUNDER_SYSTEM_CLIENT_SECRET — ASDLC system client secret
#   CONSOLE_PUBLIC_URL          — browser-facing console URL (PKCE redirect URI)

set -eu

# Install jq if missing (alpine/curl image has apk)
if ! command -v jq >/dev/null 2>&1; then
    apk add -q --no-cache jq
fi

THUNDER_URL="${THUNDER_ADMIN_URL}"
CONSOLE_URL="${CONSOLE_PUBLIC_URL}"

log()     { echo "[$(date -u +%H:%M:%S)] $*"; }
log_ok()  { echo "[$(date -u +%H:%M:%S)] ✓ $*"; }
log_err() { echo "[$(date -u +%H:%M:%S)] ✗ $*" >&2; }

# Bypass any cluster-injected HTTP proxy — Thunder is in-cluster (no proxy needed)
export NO_PROXY="*"
export no_proxy="*"
export HTTP_PROXY=""
export HTTPS_PROXY=""
export http_proxy=""
export https_proxy=""

# ── Wait for OC Thunder and obtain admin Bearer token ───────────────────────
log "Waiting for OC Thunder at ${THUNDER_URL} ..."
TOKEN=""
i=0
while [ "$i" -lt 60 ]; do
    TOKEN=$(curl -sf --max-time 10 --noproxy '*' \
        -X POST "${THUNDER_URL}/oauth2/token" \
        -d "grant_type=client_credentials&client_id=${THUNDER_ADMIN_CLIENT_ID}&client_secret=${THUNDER_ADMIN_CLIENT_SECRET}&scope=system" \
        2>/dev/null | jq -r '.access_token // empty' || true)
    if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
        log "OC Thunder is ready — admin token obtained"
        break
    fi
    i=$((i + 1))
    if [ "$i" -eq 60 ]; then
        log_err "OC Thunder not reachable after 300 s — aborting"
        exit 1
    fi
    log "  not ready yet (attempt $i/60), retrying in 5 s..."
    sleep 5
done

# ── Fetch default OU ID ─────────────────────────────────────────────────────
log "Fetching organisation unit '${THUNDER_OU_HANDLE}'..."
OU_RESP=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/organization-units/tree/${THUNDER_OU_HANDLE}") \
    || { log_err "Failed to fetch OU tree"; exit 1; }
OU_ID=$(printf '%s' "$OU_RESP" | jq -r '.id // empty')
if [ -z "$OU_ID" ] || [ "$OU_ID" = "null" ]; then
    log_err "Could not extract OU ID. Response: $(printf '%s' "$OU_RESP" | head -c 300)"
    exit 1
fi
log "OU ID: $OU_ID"

# ── Fetch default-basic-flow auth flow ID (needed for PKCE client) ──────────
log "Fetching authentication flows..."
FLOWS_RESP=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200") \
    || { log_err "Failed to fetch auth flows"; exit 1; }
AUTH_FLOW_ID=$(printf '%s' "$FLOWS_RESP" \
    | jq -r '(if type == "array" then . else (.data // .flows // .list // .items // .) | if type == "array" then . else [] end end) | .[] | select(.handle == "default-basic-flow") | .id // empty' \
    | head -1)
if [ -z "$AUTH_FLOW_ID" ] || [ "$AUTH_FLOW_ID" = "null" ]; then
    log_err "Could not find default-basic-flow. Response (first 400 chars): $(printf '%s' "$FLOWS_RESP" | head -c 400)"
    exit 1
fi
log "Auth flow ID: $AUTH_FLOW_ID"

# ── Load existing apps once ──────────────────────────────────────────────────
# OC Thunder returns { "applications": [...], "totalResults": N, "count": N }
APPS=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/applications?limit=200") \
    || { log_err "Failed to fetch applications list"; exit 1; }

# ── Upsert helper ────────────────────────────────────────────────────────────
upsert_app() {
    local client_id="$1"
    local payload="$2"

    # client_id is a top-level field in each app object in the GET response
    local existing_id
    existing_id=$(printf '%s' "$APPS" \
        | jq -r --arg cid "$client_id" \
            '(.applications // .data // .list // .items // if type == "array" then . else [] end)
             | .[] | select(.client_id == $cid) | .id // empty' \
        | head -1)

    if [ -n "$existing_id" ] && [ "$existing_id" != "null" ]; then
        log "  updating ${client_id} (id=${existing_id})..."
        curl -sf --noproxy '*' -X PUT \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications/${existing_id}" -o /dev/null \
            || { log_err "Failed to update ${client_id}"; exit 1; }
    else
        log "  creating ${client_id}..."
        curl -sf --noproxy '*' -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications" -o /dev/null \
            || { log_err "Failed to create ${client_id}"; exit 1; }
    fi
    log_ok "${client_id}"
}

ensure_confidential() {
    local name="$1" desc="$2" cid="$3" csecret="$4"
    upsert_app "$cid" \
        "{\"name\":\"${name}\",\"description\":\"${desc}\",\"ou_id\":\"${OU_ID}\",\"inbound_auth_config\":[{\"type\":\"oauth2\",\"config\":{\"client_id\":\"${cid}\",\"client_secret\":\"${csecret}\",\"grant_types\":[\"client_credentials\"],\"token_endpoint_auth_method\":\"client_secret_post\",\"pkce_required\":false,\"public_client\":false,\"token\":{\"access_token\":{\"validity_period\":3600}}}}]}"
}

# ── Confidential clients (client_credentials) ────────────────────────────────
log "Registering confidential OAuth clients in OC Thunder..."

ensure_confidential \
    "ASDLC API Client" \
    "ASDLC BFF service identity for OpenChoreo API calls" \
    "asdlc-api-client" \
    "asdlc-api-client-secret"

ensure_confidential \
    "ASDLC System Client" \
    "ASDLC platform system client for per-org OAuth app lifecycle" \
    "${THUNDER_SYSTEM_CLIENT_ID}" \
    "${THUNDER_SYSTEM_CLIENT_SECRET}"

ensure_confidential \
    "ASDLC BFF to agents-service" \
    "BFF outbound service JWT, audience: agents-service" \
    "asdlc-bff-to-agents-service" \
    "asdlc-bff-to-agents-service-secret"

ensure_confidential \
    "ASDLC BFF to git-service" \
    "BFF outbound service JWT, audience: git-service" \
    "asdlc-bff-to-git-service" \
    "asdlc-bff-to-git-service-secret"

ensure_confidential \
    "ASDLC BFF to remote-worker" \
    "BFF outbound service JWT, audience: remote-worker" \
    "asdlc-bff-to-remote-worker" \
    "asdlc-bff-to-remote-worker-secret"

# ── Public PKCE client (console) ─────────────────────────────────────────────
log "Registering console PKCE client..."
USER_ATTRS='["given_name","family_name","username","groups","ouId","ouName","ouHandle"]'
upsert_app "asdlc-console-client" \
    "{\"name\":\"ASDLC Console\",\"description\":\"ASDLC Platform Console\",\"ou_id\":\"${OU_ID}\",\"auth_flow_id\":\"${AUTH_FLOW_ID}\",\"inbound_auth_config\":[{\"type\":\"oauth2\",\"config\":{\"client_id\":\"asdlc-console-client\",\"redirect_uris\":[\"${CONSOLE_URL}\"],\"grant_types\":[\"authorization_code\",\"refresh_token\"],\"response_types\":[\"code\"],\"token_endpoint_auth_method\":\"none\",\"pkce_required\":true,\"public_client\":true,\"token\":{\"access_token\":{\"validity_period\":86400,\"user_attributes\":${USER_ATTRS}},\"id_token\":{\"validity_period\":86400,\"user_attributes\":${USER_ATTRS}}}}}]}"

# ── Assign ASDLC system client to Administrator role ─────────────────────────
log "Assigning ${THUNDER_SYSTEM_CLIENT_ID} to Administrator role..."
ROLE_RESP=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/roles?limit=200") || true
ADMIN_ROLE_ID=$(printf '%s' "$ROLE_RESP" \
    | jq -r '(if type == "array" then . else (.data // .list // .items // .) | if type == "array" then . else [] end end) | .[] | select(.name == "Administrator") | .id // empty' \
    | head -1)

if [ -n "$ADMIN_ROLE_ID" ] && [ "$ADMIN_ROLE_ID" != "null" ]; then
    # Get the application ID for the system client
    SYS_APP_ID=$(printf '%s' "$APPS" \
        | jq -r --arg cid "$THUNDER_SYSTEM_CLIENT_ID" \
            '(.applications // .data // .list // .items // if type == "array" then . else [] end)
             | .[] | select(.client_id == $cid) | .id // empty' \
        | head -1)
    if [ -n "$SYS_APP_ID" ] && [ "$SYS_APP_ID" != "null" ]; then
        curl -sf --noproxy '*' -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"role_id\":\"${ADMIN_ROLE_ID}\",\"application_id\":\"${SYS_APP_ID}\"}" \
            "${THUNDER_URL}/role-assignments" -o /dev/null 2>/dev/null || true
        log_ok "${THUNDER_SYSTEM_CLIENT_ID} → Administrator role"
    else
        log "  system client app ID not found in APPS (may have been created in this run); role assignment skipped — re-run helm upgrade to retry"
    fi
else
    log "  Administrator role not found in Thunder; role assignment skipped"
fi

log_ok "OC Thunder OAuth bootstrap complete"
