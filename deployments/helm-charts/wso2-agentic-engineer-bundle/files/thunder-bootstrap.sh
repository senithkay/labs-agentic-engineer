#!/bin/sh
# Thunder OAuth client bootstrap — runs as a post-install Helm hook.
# Registers all OAuth applications the ASDLC platform needs and assigns
# the system client to the Administrator role. Fully idempotent.
#
# Required env vars (set by the Job template):
#   THUNDER_ADMIN_URL       — in-cluster Thunder base URL (no trailing slash)
#   CONSOLE_PUBLIC_URL      — browser-facing console URL (used as PKCE redirect URI)
#
# Auth: uses asdlc-system-client (client_credentials, scope=system) which is
# bootstrapped by the Thunder helm values setup scripts. This client has the
# Administrator role and can manage all OAuth applications.

set -eu

THUNDER_URL="${THUNDER_ADMIN_URL}"
CONSOLE_URL="${CONSOLE_PUBLIC_URL}"

log()     { echo "[$(date -u +%H:%M:%S)] $*"; }
log_ok()  { echo "[$(date -u +%H:%M:%S)] ✓ $*"; }
log_err() { echo "[$(date -u +%H:%M:%S)] ✗ $*" >&2; }

# ── Get admin Bearer token ──────────────────────────────────────────────────
get_token() {
    curl -sf --max-time 10 \
        -X POST "${THUNDER_URL}/oauth2/token" \
        -d "grant_type=client_credentials&client_id=asdlc-system-client&client_secret=asdlc-system-client-secret&scope=system" \
        | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}

# ── Wait for Thunder and obtain token ───────────────────────────────────────
log "Waiting for Thunder at ${THUNDER_URL} ..."
TOKEN=""
i=0
while [ "$i" -lt 60 ]; do
    TOKEN=$(get_token 2>/dev/null || true)
    if [ -n "$TOKEN" ]; then
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
OU_RESPONSE=$(curl -sf -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/organization-units/tree/default")
OU_ID=$(printf '%s' "$OU_RESPONSE" \
    | grep -o '"handle":"default"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"handle":"default"' \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$OU_ID" ]; then
    log_err "Could not extract default OU ID from: $OU_RESPONSE"
    exit 1
fi
log "OU ID: $OU_ID"

# ── Fetch default-basic-flow auth flow ID ───────────────────────────────────
log "Fetching authentication flows..."
FLOWS=$(curl -sf -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200")
AUTH_FLOW_ID=$(printf '%s' "$FLOWS" \
    | sed 's/},{/}\n{/g' \
    | grep '"handle":"default-basic-flow"' \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$AUTH_FLOW_ID" ]; then
    log_err "Could not find default-basic-flow auth flow"
    exit 1
fi
log "Auth flow ID: $AUTH_FLOW_ID"

# ── Load existing apps once ──────────────────────────────────────────────────
APPS=$(curl -sf -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/applications")

upsert_app() {
    local client_id="$1"
    local payload="$2"

    local existing_id
    existing_id=$(printf '%s' "$APPS" \
        | sed 's/},{/}\n{/g' \
        | grep "\"clientId\":\"${client_id}\"" \
        | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

    if [ -n "$existing_id" ]; then
        log "  updating ${client_id} (id=${existing_id})..."
        curl -sf -X PUT \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications/${existing_id}" -o /dev/null
    else
        log "  creating ${client_id}..."
        curl -sf -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$payload" \
            "${THUNDER_URL}/applications" -o /dev/null
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
    "Workload Publisher" \
    "OpenChoreo Workload Publisher Client (dockerfile-builder generate-workload-cr step)" \
    "openchoreo-workload-publisher-client" \
    "openchoreo-workload-publisher-secret"

ensure_confidential \
    "OC Observer Resource Reader" \
    "Client used by the BFF to fetch tokens for the OC Observer service (live workflow logs)" \
    "openchoreo-observer-resource-reader-client" \
    "openchoreo-observer-resource-reader-client-secret"

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

# ── Assign asdlc-system-client to Administrator role ─────────────────────────
log "Assigning asdlc-system-client to Administrator role..."
APPS=$(curl -sf -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/applications")
SYSTEM_APP_ID=$(printf '%s' "$APPS" \
    | sed 's/},{/}\n{/g' \
    | grep '"clientId":"asdlc-system-client"' \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$SYSTEM_APP_ID" ]; then
    log_err "Could not find asdlc-system-client"
    exit 1
fi

ROLES=$(curl -sf -H "Authorization: Bearer $TOKEN" "${THUNDER_URL}/roles")
ADMIN_ROLE_ID=$(printf '%s' "$ROLES" \
    | sed 's/},{/}\n{/g' \
    | grep '"name":"Administrator"' \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$ADMIN_ROLE_ID" ]; then
    log_err "Could not find Administrator role"
    exit 1
fi

# Check if already assigned (Thunder returns 500 instead of 409 on duplicate — pre-check to avoid it)
EXISTING=$(curl -sf -H "Authorization: Bearer $TOKEN" \
    "${THUNDER_URL}/roles/${ADMIN_ROLE_ID}/assignments" || true)
ALREADY_BOUND=$(printf '%s' "$EXISTING" \
    | grep -c "\"id\":\"${SYSTEM_APP_ID}\"" || true)
if [ "${ALREADY_BOUND:-0}" -gt 0 ]; then
    log "asdlc-system-client already bound to Administrator role — skipping"
else
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "{\"assignments\":[{\"id\":\"${SYSTEM_APP_ID}\",\"type\":\"app\"}]}" \
        "${THUNDER_URL}/roles/${ADMIN_ROLE_ID}/assignments/add")
    case "$HTTP_CODE" in
        200|201|204) log_ok "asdlc-system-client bound to Administrator role" ;;
        409)         log   "asdlc-system-client already bound — skipping" ;;
        *)           log_err "Role assignment failed (HTTP ${HTTP_CODE})"; exit 1 ;;
    esac
fi

log_ok "Thunder OAuth bootstrap complete"
