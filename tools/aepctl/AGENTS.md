# AGENTS.md — services/aepctl

AEP Control Plane CLI and management server. Two binaries in one Go module:

| Binary | Entry point | Role |
|--------|------------|------|
| `aep` | `main.go` → `cmd/` | CLI tool — installs AEP, provisions OpenBao, manages Thunder OAuth clients |
| `aep-server` | `cmd/aep-server/` | gRPC server — runs in-cluster, handles provisioning RPCs from the CLI |

## Commands

```bash
go build -o aep .                        # build CLI
go build -o aep-server ./cmd/aep-server  # build server
```

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands for the CLI (init, sre, uninstall) |
| `cmd/aep-server/` | gRPC server binary (Init, ThunderSetup, OpenbaoUnseal handlers) |
| `internal/adminpb/` | Generated gRPC stubs from `proto/admin.proto` |
| `internal/openbao/` | HTTP client for OpenBao API |
| `internal/thunder/` | Thunder OAuth client registration (Job + CORS patch) |
| `internal/bootstrap/` | Crypto helpers (RSA key gen, password gen) |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward) |
| `internal/config/` | Viper config defaults and init |

## Proto

`proto/admin.proto` defines the `AEPAdmin` gRPC service. To regenerate stubs after editing the proto:

```bash
buf generate   # or: protoc with protoc-gen-go + protoc-gen-go-grpc
```

## Config

Resolution order (highest wins): **CLI flag > `AEP_*` env var > `--config` file >
in-cluster `aep-cli-config` ConfigMap > code default**.

- **`--config <file>`** (persistent flag): a local YAML whose keys feed viper for
  every command, including `install` (which can't read the ConfigMap — it doesn't
  exist pre-install). Lets users keep one file instead of a long flag list. See
  `config.example.yaml` for the schema. Loaded by `config.LoadFile` (MergeInConfig).
- **`aep config init [-o file]`** writes an annotated, defaults-filled
  starter config (the `config.example.yaml` template, embedded via go:embed in
  `main.go`). This is how a fresh user with no cluster/repo gets a file to edit —
  there's nothing to `export` pre-install. Refuses to overwrite an existing `-o`.
- **`aep config use <file>`** records that file as the active one (pointer
  at `~/.aep/active-config`), so later commands load it **without** `--config`.
  `config which` prints it, `config clear` unsets it. Whenever a config file is in
  effect (via `--config` or the pointer), every command prints `Using config file:
  <path>` to stderr so the source is never a surprise.
- After `install` runs, it writes non-sensitive config to the `aep-cli-config`
  ConfigMap in `wso2-aep`; subsequent commands load it via `PersistentPreRunE`
  (`install` is annotated `skipClusterConfig`).
- Sensitive values (Thunder admin secret) come from the ESO-synced
  `aep-thunder-secrets` Secret, never the ConfigMap.

Every install flag is bound to a viper key (`init.go`), so config-file / env-var
values flow through even when the flag isn't passed.

Server reads env vars injected by the bootstrap Helm chart.
