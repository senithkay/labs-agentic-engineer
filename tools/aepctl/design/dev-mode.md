# Dev mode (`aep dev`) — final shape

Dev mode is aepctl's local inner loop: after editing an AEP service's source, one
command rebuilds its image, imports it into the local k3d cluster, and rolls the
running Deployment to it. It is **opt-in** — off by default, and the
cluster-mutating subcommands refuse until it is enabled.

## Command surface

All commands follow `aep dev <verb> [service]`:

- `aep dev enable [--project-path DIR]` / `disable` / `set-path DIR` — toggle the
  mode and point it at a local AEP checkout. Persisted to `~/.aep/config.yaml`
  (`dev.enabled`, `dev.project_path`, plus `dev.k3d_cluster`, `dev.namespace`,
  `dev.image_prefix`). The path is also editable directly in the config file.
- `aep dev status` — always available; prints the mode, paths, and a per-service
  table of the running Deployment image + ready replicas.
- `aep dev reload <service|all>` — the core in-cluster verb (build → import →
  persist → redeploy). Gated by `requireDevMode()`.
- `aep dev serve <service>` — run the service's **local dev server** (HMR) instead
  of building an image. Capability-gated (see below).
- `aep dev restart <service|all>` — rollout restart only (no rebuild), for
  ConfigMap/Secret changes.
- `aep dev logs <service> [-f]` — tail the service pod logs.

## In-cluster (`reload`) vs local dev server (`serve`)

Two ways to see a code change run — the user picks by verb:

- **`reload`** builds the image and redeploys it into the cluster. Works for every
  service. It's the source of truth, but a full container build per edit.
- **`serve`** runs the service's own dev server on your laptop with hot-reload —
  seconds per edit, but not the deployed artifact.

`serve` is **capability-gated**, not a universal mode: a service opts in via a
non-empty `DevServerCmd` in the registry. Only the **console** frontend does today.
This is deliberate — console is an edge/leaf (nothing in the cluster calls into it;
it only makes outbound calls to aep-api), so it runs locally safely with just a
proxy to the API. Backends (`aep-api`, `agents`, `aep-mcp-server`, `collab`) are
mesh nodes reached by other services via cluster DNS and need many injected
dependencies, so "run locally" doesn't work cleanly (that's mirrord/telepresence
territory — out of scope). `aep dev serve aep-api` therefore returns a clear error
pointing to `reload`, rather than half-working.

`serve console` defaults to **mock data** (`VITE_API_MODE=mock`, no cluster) — ideal
for pure UI work. `--real` auto-manages a `kubectl port-forward` to the in-cluster
aep-api (torn down on Ctrl-C) for live data. It runs in the foreground until Ctrl-C.

## Service registry

`cmd/dev.go` holds the single source of truth mapping a service name to how it
builds (dockerfile + context, relative to the project root — mirroring
`.github/workflows/build-images.yml`) and how it deploys (Deployment + container
name, and the top-level Helm values key). Registered services: `aep-api`,
`agents`, `aep-mcp-server`, `console`, `collab`. `reload` checks the Deployment
exists first, so a service whose chart isn't installed (e.g. `collab` on branches
before its chart lands) fails fast with a clear message rather than after a build.

## `reload` pipeline

1. Guard — dev enabled, project path valid (contains `AGENTS.md`), `k3d`/`docker`
   present, and `dev.k3d_cluster` exists.
2. `docker build -f <root>/<dockerfile> -t <prefix>/<svc>:dev <root>/<context>`.
3. `k3d image import <prefix>/<svc>:dev -c <cluster>`.
4. Merge the image override into `~/.aep/dev-values.yaml`
   (`<valuesKey>.image = {repository, tag: dev, pullPolicy: Never}`).
5. Strategic-merge patch the Deployment's container image + `imagePullPolicy:
   Never` and stamp an `aepctl.wso2.com/reloadedAt` annotation, then wait for the
   rollout to complete.

The `:dev` tag is **stable** (keeps `dev-values.yaml` stable); each import
overwrites its content in the node, and the annotation forces a fresh ReplicaSet
even though the tag is unchanged. `pullPolicy: Never` guarantees the imported
local image is used and fails loudly (`ErrImageNeverPull`) if it is missing.

### Build speed
`reload` sets `DOCKER_BUILDKIT=1` (in `internal/dev/build.go`) so the service
Dockerfiles' `--mount=type=cache` steps apply. The pnpm-workspace Dockerfiles
(`apps/console`, `services/agents`, `services/aep-mcp-server`) copy manifests +
`packages/` before `pnpm install` and the frequently-edited **app source last**,
with the pnpm store on a cache mount — so a source-only edit skips reinstall and
the workspace-dep builds, rerunning just `gen`/`build`. `services/aep-api` (Go)
caches the module + build caches. CI's buildx honors the same mounts.

## Persistence across reinstall

`aep dev reload` records overrides in `~/.aep/dev-values.yaml`, and `aep init`
appends `-f ~/.aep/dev-values.yaml` to the platform `helm` args when dev mode is
enabled (last `-f` wins). So a re-`init`/upgrade keeps the locally-built images
instead of reverting to the registry images.

**Caveat:** the dev images live only in the k3d node. If the cluster is recreated
(`k3d cluster delete/create`), they are gone and pods with a persisted dev
override will `ErrImageNeverPull` — re-run `aep dev reload <svc>` (or `all`) to
re-import.

## Scope / assumptions

- k3d-only (import uses `k3d image import`); non-k3d clusters error clearly.
- Covers the platform runtime Deployments only — not `aep-server` (the CLI's own
  server) or data-plane Jobs (remote-worker / coding-agent-runner).
- No file-watch/auto-reload; reload is an explicit command.
