#!/bin/sh
# Thunder OAuth client bootstrap — runs as a post-install Helm hook.
# Registers all ASDLC OAuth applications in AE Thunder and assigns
# the system client to the Administrator role. Fully idempotent.
#
# Runs against AE Thunder (wso2-ae-thunder-extension), NOT OC's platform Thunder.
# OC system clients (openchoreo-workload-publisher-client,
# openchoreo-observer-resource-reader-client) are managed by OC in its own
# Thunder instance and must NOT be created here.
#
# Required env vars (set by the Job template):
#   THUNDER_ADMIN_URL       — in-cluster AE Thunder base URL (no trailing slash)
#   CONSOLE_PUBLIC_URL      — browser-facing console URL (used as PKCE redirect URI)

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

# ── Get admin Bearer token ──────────────────────────────────────────────────
get_token() {
    curl -sf --max-time 10 --noproxy '*' \
        -X POST "${THUNDER_URL}/oauth2/token" \
        -d "grant_type=client_credentials&client_id=asdlc-system-client&client_secret=asdlc-system-client-secret&scope=system" \
        | jq -r '.access_token'
}

# ── Wait for Thunder and obtain token ───────────────────────────────────────
log "Waiting for Thunder at ${THUNDER_URL} ..."
TOKEN=""
i=0
while [ "$i" -lt 60 ]; do
    TOKEN=$(get_token 2>/dev/null || true)
    if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
        log "Thunder is ready — token obtained"
        break
    fi
    i=$((i + 1))
    if [ "$i" -eq 60 ]; then
        log_err "Thunder not reachable / token not obtainable after 300 s — aborting"
        exit 1
    fi
    log "  not ready yet (attempt $i/60), retrying in 5 s..."
    sleep 5
done

# ── Fetch default OU ID ─────────────────────────────────────────────────────
log "Fetching default organisation unit..."
OU_RESP=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/organization-units/tree/default") \
    || { log_err "curl failed (code $?) fetching OU tree"; exit 1; }
OU_ID=$(printf '%s' "$OU_RESP" | jq -r '.id')
if [ -z "$OU_ID" ] || [ "$OU_ID" = "null" ]; then
    log_err "Could not extract default OU ID from: $(printf '%s' "$OU_RESP" | head -c 200)"
    exit 1
fi
log "OU ID: $OU_ID"

# ── Fetch default-basic-flow auth flow ID ───────────────────────────────────
log "Fetching authentication flows..."
FLOWS_RESP=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200") \
    || { log_err "curl failed (code $?) fetching auth flows"; exit 1; }
AUTH_FLOW_ID=$(printf '%s' "$FLOWS_RESP" \
    | jq -r '(if type == "array" then . else (.data // .flows // .list // .items // .) | if type == "array" then . else [] end end) | .[] | select(.handle == "default-basic-flow") | .id')
if [ -z "$AUTH_FLOW_ID" ] || [ "$AUTH_FLOW_ID" = "null" ]; then
    log_err "Could not find default-basic-flow auth flow. Response (first 400 chars): $(printf '%s' "$FLOWS_RESP" | head -c 400)"
    exit 1
fi
log "Auth flow ID: $AUTH_FLOW_ID"

# ── Load existing apps once ──────────────────────────────────────────────────
APPS=$(curl -sf --noproxy '*' -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/applications") \
    || { log_err "curl failed (code $?) fetching applications list"; exit 1; }

upsert_app() {
    local client_id="$1"
    local payload="$2"

    local existing_id
    existing_id=$(printf '%s' "$APPS" \
        | jq -r --arg cid "$client_id" \
            '(if type == "array" then . else (.data // .list // .items // .) | if type == "array" then . else [] end end) | .[] | select(.inboundAuthConfig[]?.config?.clientId == $cid) | .id' \
        | head -1)

    if [ -n "$existing_id" ] && [ "$existing_id" != "null" ]; then
        log "  updating ${client_id} (id=${existing_id})..."
        curl -sf --noproxy '*' -X PUT \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications/${existing_id}" -o /dev/null \
            || { log_err "curl failed (code $?) updating ${client_id}"; exit 1; }
    else
        log "  creating ${client_id}..."
        curl -sf --noproxy '*' -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications" -o /dev/null \
            || { log_err "curl failed (code $?) creating ${client_id}"; exit 1; }
    fi
    log_ok "${client_id}"
}

ensure_confidential() {
    local name="$1" desc="$2" cid="$3" csecret="$4"
    upsert_app "$cid" \
        "{\"name\":\"${name}\",\"description\":\"${desc}\",\"ouId\":\"${OU_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"${cid}\",\"clientSecret\":\"${csecret}\",\"grantTypes\":[\"client_credentials\"],\"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":false,\"publicClient\":false,\"token\":{\"accessToken\":{\"validityPeriod\":3600}}}}]}"
}

# ── Confidential clients (client_credentials) ───────────────────────────────
log "Registering confidential OAuth clients..."
ensure_confidential \
    "ASDLC API Service" \
    "ASDLC API BFF service-to-service principal for OpenChoreo" \
    "asdlc-api-client" \
    "asdlc-api-client-secret"

ensure_confidential \
    "ASDLC BFF to git-service" \
    "BFF outbound Service JWT, audience: git-service" \
    "asdlc-bff-to-git-service" \
    "asdlc-bff-to-git-service-secret"

ensure_confidential \
    "ASDLC BFF to agents-service" \
    "BFF outbound Service JWT, audience: agents-service" \
    "asdlc-bff-to-agents-service" \
    "asdlc-bff-to-agents-service-secret"

ensure_confidential \
    "ASDLC BFF to remote-worker" \
    "BFF outbound Service JWT, audience: remote-worker" \
    "asdlc-bff-to-remote-worker" \
    "asdlc-bff-to-remote-worker-secret"

# ── Public PKCE client (console) ─────────────────────────────────────────────
log "Registering console PKCE client..."
USER_ATTRS='["given_name","family_name","username","groups","ouId","ouName","ouHandle"]'
upsert_app "asdlc-console-client" \
    "{\"name\":\"ASDLC Console\",\"description\":\"ASDLC Platform Console\",\"ouId\":\"${OU_ID}\",\"authFlowId\":\"${AUTH_FLOW_ID}\",\"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{\"clientId\":\"asdlc-console-client\",\"redirectUris\":[\"${CONSOLE_URL}\"],\"grantTypes\":[\"authorization_code\",\"refresh_token\"],\"responseTypes\":[\"code\"],\"tokenEndpointAuthMethod\":\"none\",\"pkceRequired\":true,\"publicClient\":true,\"token\":{\"accessToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}},\"idToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}}}}}]}"

# Note: asdlc-system-client role assignment (Administrator) is handled by ae-thunder-extension's
# 51-asdlc-default-roles.sh which runs during Thunder's own setup — no action needed here.

log_ok "Thunder OAuth bootstrap complete"
