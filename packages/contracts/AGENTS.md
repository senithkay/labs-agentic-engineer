# AGENTS.md — packages/contracts (`@aep/contracts`)

## Instructions

- The OpenAPI contract is hand-maintained (contract-first), never generated.
  Edit deliberately — server code and console types are generated FROM it.
- `api/v1/openapi.yaml` is the single contract document (OpenAPI 3.0.3). After
  any edit, run `make gen-api` in `services/aep-api` (CI's `gen-api-check`
  enforces it).
- A few schemas are hand-written in `services/aep-api/models/` and marked
  `x-go-type: models.X` in the contract — don't duplicate them.
- Every error response points at the shared `Error` schema
  (`{code, message, details?[{field, message}]}`, always `application/json`);
  validation failures are `400`.
- The `path` parameter of `read-file` is a trailing wildcard (may contain
  slashes); the server registers the extra catch-all route for it.
