# AEP — local dev setup (Skaffold)

> **This is the local dev path.** For installing AEP on an existing OpenChoreo
> cluster, use `aectl platform install` instead — see [`tools/aectl/`](../tools/aectl/AGENTS.md).

Runs the full AEP stack on a single laptop: a k3d cluster with OpenChoreo,
Thunder (identity), Temporal, and the AEP services deployed in-cluster via
Helm + Skaffold.

## Setup overview

| Step | Command | Does |
|---|---|---|
| 1. Create cluster | `bash scripts/setup-dev.sh` | k3d + prereqs + OC + Thunder + Temporal |
| 2. Register secrets & clients | `make setup-local` | K8s Secrets, Thunder OAuth clients, resource-type catalog |
| 3. Inner dev loop | `skaffold dev` | build images → load into k3d → Helm deploy → watch for changes |

Run steps 1 and 2 once per cluster. Step 3 is the everyday dev loop.

## How it works

AEP services (BFF, agents, console, collab, MCP server) run as in-cluster
workloads managed by Skaffold and the platform Helm chart
(`helm-charts/platform`). Coding agents run as ephemeral OpenChoreo Job
Components in the project's data-plane namespace.

```
┌──────────────────────── k3d cluster ──────────────────────────┐
│ OC Control / Data / Workflow planes                           │
│ Thunder IDP   OpenBao   ESO   kgateway   Temporal             │
│                                                               │
│ AEP services (Skaffold/Helm):                                 │
│   aep-api :9090   agents :4000   console :8080   collab       │
│   aep-mcp-server :3401                                        │
│                                                               │
│ Coding agent: OC Job Component (AGENT_RUNNER_IMAGE) ← BFF    │
│ ClusterWorkflow: dockerfile-builder         ← BFF dispatches  │
└───────────────────────────────────────────────────────────────┘
```

## Files

| Path | Purpose |
|---|---|
| `scripts/setup-dev.sh` | One-shot cluster setup: k3d → prereqs → OC → Thunder → Temporal |
| `scripts/delete-dev.sh` | Tear down the cluster |
| `scripts/setup-local.sh` | **(Skaffold)** K8s Secrets + Thunder clients + resource-type catalog (`make setup-local`) |
| `../skaffold.yaml` | In-cluster build/deploy (`skaffold dev`) |
| `helm-charts/platform/values.local.dev.yaml.example` | Per-developer override template (webhook/smee, etc.) |
| `manifests/docker-build-workflow.yaml` | `dockerfile-builder` ClusterWorkflow (Argo CWTs) |
| `single-cluster/values-thunder.yaml` | Thunder helm values + bootstrap scripts (users, OAuth apps) |
| `single-cluster/values-cp.yaml` | OC Control Plane helm values |
| `single-cluster/values-dp.yaml` | OC Data Plane helm values |

## Webhooks (component builds on PR merge)

Copy `helm-charts/platform/values.local.dev.yaml.example` to `values.local.dev.yaml`
(git-ignored) and set a smee.io `webhook.deliveryURL` — see that file's comments.
Webhooks register at project-create time.

## Credentials

The Thunder default admin (`admin` / `admin`) is in the **Administrators** group,
which `setup-local.sh` binds to the OC `admin` ClusterAuthzRole.

For GitHub repo provisioning, connect a PAT (or GitHub App) at **Settings → GitHub Integration**.
For AI generation, connect an Anthropic key at **Settings → Anthropic Integration** — per-org, with no platform fallback.

## Tear down

```bash
# Stop the Skaffold watch: Ctrl-C the `skaffold dev` process
bash scripts/delete-dev.sh    # destroy cluster (loses all OC + Temporal state)
```
