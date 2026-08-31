# Developer guide — setup & dev flow

## Prerequisites

- Go 1.26 (auto-downloaded by the toolchain if your local `go` is older;
  `GOTOOLCHAIN=auto`).
- Node 22 LTS + pnpm 10 (`corepack enable` to get the pinned pnpm).
- `make tools` to install pinned Go tools (golangci-lint).

## First run

```bash
make install     # pnpm install + go work sync
make gen         # regenerate contracts (TS clients + Go server interfaces)
make build       # build everything
```

## Uniform verbs

All driven from the root `Makefile` (the single entry point):

| Verb | What it does |
|---|---|
| `make gen` | `openapi-typescript` (TS) + `go generate` (Go) codegen |
| `make build` | turbo build (TS) + `go build` (go.work) — runs `gen` first |
| `make dev` | start dev servers |
| `make test` | turbo test + `go test` |
| `make lint` | eslint + golangci-lint |
| `make typecheck` | `tsc` + `go vet` |
| `make license-check` | fail if any source lacks the Apache header |

## Adding a package

- **TypeScript:** create `packages/<name>` (or `apps/`, `services/`, `runners/`)
  with a `package.json` exposing the uniform scripts. A workspace glob picks it up
  — no tooling edits.
- **Go:** create the module and add one `use` line to `go.work`. The Makefile
  discovers it dynamically.

## Running the platform

**Local dev (Skaffold):** Three steps, all in `deployments/`:
`scripts/setup-dev.sh` creates the k3d cluster with OpenChoreo, Thunder, and
Temporal; `make setup-local` registers secrets and Thunder OAuth clients; and
`make dev-cluster` (Skaffold) builds and deploys the AEP services in-cluster.
The root README has the full walkthrough; the `deployments/` README documents
each step.

**Existing OC cluster (aectl):** Build `tools/aectl`, run
`aectl platform config import --config <file>` to write cluster config, then
`./aectl platform install` — installs the Helm chart, provisions OpenBao, and
wires Thunder clients onto a cluster that already has OpenChoreo.
