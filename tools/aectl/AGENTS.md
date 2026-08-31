# AGENTS.md — tools/aectl

AEP Control Plane CLI. Single binary (`aectl`) that installs AEP onto an existing
OpenChoreo cluster, provisions OpenBao secrets, and registers Thunder OAuth clients.

This is the **production / existing-cluster** deployment path. For local dev on a
k3d cluster, use Skaffold instead (`make dev-cluster` — see `deployments/README.md`).

## Build

```bash
cd tools/aectl && go build -o aectl .
```

## Deploy AEP onto an existing OC cluster

```bash
# 1. Write cluster config to the in-cluster ConfigMap (required before install)
aectl platform config import --config ~/my-aectl-config.yaml

# 2. Install: seeds OpenBao secrets, installs the Helm chart, registers Thunder clients
aectl platform install

# Tear down
aectl uninstall
```

`platform install` is idempotent — re-running upgrades in place. Pass
`--reuse-secrets` to skip secret prompts when upgrading an existing install.

## Commands

| Command | Does |
|---|---|
| `aectl platform config import --config <file>` | Write a local YAML config file to the `aep-cli-config` ConfigMap in `wso2-aep` |
| `aectl platform config export` | Print the current in-cluster config |
| `aectl platform config validate` | Validate the in-cluster config |
| `aectl platform install` | Seed OpenBao + install/upgrade Helm chart + wait for pods + register Thunder clients + install addons |
| `aectl platform status` | Show platform component status |
| `aectl rollback` | Roll back the platform Helm release |
| `aectl secret import` | Import secrets into OpenBao |
| `aectl sre` | SRE tooling commands |
| `aectl thunder` | Thunder OAuth client management |

## Config

All config is stored in the `aep-cli-config` ConfigMap in the `wso2-aep`
namespace. Every command loads it at startup via `PersistentPreRunE`
(`config.LoadFromCluster`) and applies values as viper defaults — CLI flags and
`AEP_*` env vars always take precedence.

**Populate before first install:**
```bash
aectl platform config import --config my-config.yaml
```

**Key config values** (`aectl platform config export` shows current values):

| Key | Purpose |
|---|---|
| `thunder.url` | In-cluster Thunder URL |
| `thunder.public_url` | Public Thunder URL (browser-facing) |
| `thunder.admin_client_id` | Thunder admin OAuth client ID |
| `oc.api_url` | In-cluster OpenChoreo platform API URL |
| `oc.system_namespace` | OC control-plane namespace |
| `webhook.delivery_url` | Public URL registered on each repo's GitHub webhook |
| `codingagent.openbao_direct.enabled` | Set `true` for local/OSS installs without a secret-manager service |

**Sensitive values** are never stored in the ConfigMap. `platform install` reads:
- `ANTHROPIC_API_KEY` env var (or interactive prompt)
- `AEP_THUNDER_ADMIN_CLIENT_SECRET` env var (or interactive prompt)

The Thunder admin client secret is persisted in OpenBao and subsequently
read from the ESO-synced `aep-thunder-admin-creds` Secret for subsequent
commands.

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands (`platform install/config/status`, `rollback`, `secret`, `sre`, `thunder`) |
| `internal/config/` | ConfigMap read/write, viper env-prefix binding, key validation |
| `internal/helm/` | Helm chart install/upgrade helpers |
| `internal/addons/` | Optional addon registry (thunder-app-operator, postgres-cnpg) with interactive selector |
| `internal/bootstrap/` | Crypto helpers — RSA key gen, random password gen |
| `internal/openbao/` | OpenBao HTTP client (port-forward, k8s auth, secret read/write) |
| `internal/thunder/` | Thunder OAuth client registration via Job + CORS patch |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward, apply, exec) |
