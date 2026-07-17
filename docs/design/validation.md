# VALIDATION phase

## Why

The SDLC (requirements → design → implement → merge → deploy) asserted "done"
but never verified that the deployed system satisfies the requirements. The
VALIDATION phase closes that gap: it authors and runs end-to-end tests against
the *already-deployed* system and produces a report, without a human writing a
single test.

## The acceptance oracle

`specs/validation/validation-criteria.json` is the machine-readable oracle:
`requirements[] → criteria[] { id: AC-NNN-x, must, method: e2e | scenario |
manual }`. It is authored in the **design phase** by the `validation-criteria`
skill from the requirement prose alone (never from the design or source), as the
final step of "Generate design" (the console's `buildDesignGenerationInstruction`;
the server `design-generate` steering item (6) for the legacy path). The prompt
only states the intent — the model selects the skill by its `description`.

From validation's perspective the oracle is **read-only input**. Coverage is not
a persisted flag: it is derived from committed-spec presence + the report's
pass/fail. Nothing in the validation run writes under `specs/`.

## End-to-end flow (as-built)

1. **Mint (planning).** After the plan tap writes the implementation issues, the
   plan session mints ONE project-scoped `aep:validation` issue, best-effort
   (`validation.EnsureValidationIssue`). It `dependsOn` every design component
   and carries **no `aep:execute` stamp**, so the reactive sweep never dispatches
   it — the devflow does. Idempotent (one open validation issue per project);
   a missing/malformed oracle is a clean no-op.
2. **Gate.** The Temporal devflow's *validating* phase runs only after every
   planned coding task has succeeded, then runs an OpenChoreo consistency check
   (`Validate`) asserting each component has a Ready deployment. The execution
   funnel independently holds the task until all its component deps derive
   `deployed`.
3. **Orchestrate.** The validating phase spawns ONE `ValidationFlowWorkflow`
   child (`validationflow-<org>-<project>-<tag>`). It resolves the validation
   issue via `ResolveValidationTask` (idempotent re-ensure + find; zero criteria
   ⇒ skipped, "no acceptance criteria"), records the phase's run row
   (`kind=validation`, parented to the dev run — the issue's webhook-signal
   owner), then fans out one `ValidationTaskWorkflow` lane child per automated
   lane, in parallel (e2e only today). One issue, one PR, N lanes: the
   orchestrator pumps the issue's signals to the lanes (`lane-status`,
   correlated by execution id) and merges the single PR only after every lane
   finished.
4. **Dispatch.** The orchestrator dispatches each lane through the shared coding
   executor, which swaps to the Playwright runner image
   (`VALIDATION_RUNNER_IMAGE`), a project-scoped sentinel component,
   `AEP_TASK_KIND=validation`, and a longer deadline; the coding-only component
   pre-flight is skipped.
5. **Runner.** The `aep-validation` skill reads the issue, fetches the deployed
   endpoints (and, on demand, test credentials) from the platform, authors and
   runs Playwright e2e specs against the deployed app, heals brittleness
   (bounded), generates a report, and opens ONE PR (`Closes #N`) — ready for
   review even when criteria fail (a failing criterion is report content, not a
   task failure). The PR opening doubles as the e2e lane's completion signal.
6. **Merge = done.** Once every lane succeeded, the orchestrator merges the
   single validation PR: no build, no deploy. The issue's resting state is
   `merged`, surfaced as "Done".

## Platform side (`services/aep-api`)

**Issue minting — `internal/feature/validation`.** Owns the *issue* side only;
feature-edge allowlist is `{gitrepo}`, with design and criteria reads as consumer
ports adapted at the composition root. Minted in planning
(`task/plan.go` `SetValidationIssueMinter`); re-ensured in the devflow
(`ResolveValidationTask`). The issue body carries the acceptance oracle, the test
layout, and the report/PR conventions — but never endpoint URLs or credentials.

**Runtime inputs — two runner-scoped S2S endpoints** (`validation_internal_huma.go`),
both execution-fenced (`auth.ExecutionScopedInput` / `SecurityRunner`) and
org-tenant-fenced (`GetByIDScoped`):
- `GET /internal/v1/executions/{id}/validation-context` → `{ endpoints[], criteriaPath }`.
  Endpoints resolve from OpenChoreo ReleaseBindings (first HTTP external URL per
  component).
- `POST /internal/v1/executions/{id}/test-credentials` → `{ username, password,
  mock, note }`. Requested only when a criterion needs a login. v1 returns a mock
  `admin/admin`; the request hints (`role`/`purpose`/`username`) are the forward
  contract for real per-project user provisioning.

**Service identity for OpenChoreo reads.** Endpoint resolution and the `Validate`
consistency check run inside the runner's inbound request, whose ctx carries the
runner task JWT (aud `git-service`). They must set `auth.WithServiceIdentity` or
OpenChoreo rejects the forwarded token (401) and zero endpoints resolve. (This was
the root cause of "the deployed endpoint didn't resolve".)

**Lifecycle.** No new task states. Validation runs coding → PR → merge:
- The phase's tracking row is the ORCHESTRATOR's `workflow_runs` row
  (`kind=validation`, the issue number, `parent_workflow_id` = the dev run):
  the signaler's `RunningTaskByIssue` (kind IN task, validation) routes the
  issue's webhooks to it, and the status builder's `ValidationRunByParent`
  reads it as the deploy board's validation state. Lane children record no
  rows. There is no `class` column — `Kind` discriminates.
- A merged validation PR spawns **no build** (`execution/events.go`) — the task is
  project-scoped, with no component to build.
- The derived-status view maps a completed validation (issue closed with a
  succeeded coding run) to `deployed` → "Done" (`task/reads.go`); a genuinely
  failed coding run stays "Failed".
- The planner excludes the validation class from the implementation graph; the
  funnel deps-gate holds it until every component deploys.

## Runner side (`runners/remote-worker`)

**Skills (plugin `aep`, v0.6.0).**
- `aep-validation` — orchestrator; replaces the `aep` implementation workflow for
  `aep`+`validation` issues (the `aep` skill points to it; its auth/git/deny rules
  still apply).
- `playwright-authoring` — authoring discipline: explore the live app via
  `playwright-cli`, a spec counts only after passing twice, `// spec:` header,
  semantic-locator/web-first-assertion rules, `request` fixture for API criteria,
  env-only credentials.
- `playwright-healing` — bounded brittleness repair; a heal changes *how* a test
  drives the app, never *what* it asserts; every heal is logged.
- `playwright-cli` — vendored (Apache-2.0) browser-automation CLI mechanics.

**File layout (the read-only-`specs/` contract).**
- `specs/validation/validation-criteria.json` — read-only oracle.
- `tests/e2e/` — runnable Playwright package (specs, config, `lib/targets`,
  `targets.json`, committed copy of the report script). Outside every component
  App Path, so committing tests never triggers a rebuild.
- `tests/validation/` — the validation record: `test-plan.md`, `report.md`,
  `report.json`.

**Report generation — `generate-report.mjs` (deterministic).** Maps the Playwright
JSON reporter onto the oracle by the `AC-NNN-x:` title join key and writes
`tests/validation/report.{md,json}`. It reads the oracle but never writes it.
Heal-visibility gate: a pre-existing spec (git `--diff-filter=M`) modified without
a matching heal-log entry is a hard error (exit 2); newly added specs are not
gated. The agent never hand-writes the report.

**Image — `Dockerfile.validation`.** `node:22-bookworm-slim` (glibc, for chromium)
with baked chromium + `playwright-cli` + `gh` + Go, plus the plugin skills baked
in at build time (`COPY . .`, same convention as the coding runner). `AEP_PLAYWRIGHT_VERSION`
pins `tests/e2e/package.json` to the baked browser version. The alpine coding image
cannot run chromium, so there is no fallback — an empty `VALIDATION_RUNNER_IMAGE`
disables validation dispatch (fails loudly). Released to GHCR as
`ghcr.io/wso2/aep/remote-worker-validation:<version>` by `.github/workflows/release.yml`
and wired into aep-api via the platform Helm chart's `validationRunner.image`; the
local `aep-validation-runner:dev` tag (`build-validation-runner.sh`) is the dev path.

**Runner plumbing (`src/`).** The only validation-specific change is the
`AEP_TASK_KIND` switch (`implementation` | `validation`) that preloads
`aep:aep-validation`. Everything else is generic: bearer via `AEP_BEARER_FILE`
(never env), `AEP_TASK_ID` = execution id, `AEP_PLATFORM_URL`. All validation HTTP
calls live in the skill markdown, not runner code.

## Scope & follow-ups (not built)

- **Automated method:** `e2e` only. `manual` → human checklist in the report;
  `scenario` → listed as not-yet-validated.
- **Test credentials:** mock `admin/admin`; real per-project user provisioning is
  pending.
- **Results are surfaced via the PR only** (committed report + a summary issue
  comment). There is deliberately **no platform report-ingest endpoint / read
  model, no public `GET /projects/{p}/validation` status API, and no console
  validation UI** yet.
- No scenario-lane (agentic-judgment) automation and no automated fix-task loop on
  failures. The workflow side is lane-ready (`ValidationFlowWorkflow` fans out
  `ValidationTaskWorkflow` children; a lane = an entry in its lane slice), but a
  second lane still needs: a job-succeeded watcher signal (today success == the
  PR opening, and only the e2e lane opens the PR), lane-qualified funnel
  admission (`TryAdmit` allows one active execution per issue), and a finalize
  step that combines lane branches + per-lane reports into the single PR.
- `scripts/create-validation-issue.mjs` is an interim, repo-root issue generator
  kept only for the local harness; the platform mints the issue itself in
  production.

## Key files

| Concern | Location |
|---|---|
| Issue minting + endpoints/credentials | `services/aep-api/internal/feature/validation/` |
| Composition-root adapters (endpoints, `Validate`, creds, service identity) | `services/aep-api/internal/app/validation_adapters.go` |
| Mint call site (planning) | `services/aep-api/internal/feature/task/plan.go` |
| Devflow validating phase (orchestrator + lane children) | `services/aep-api/internal/feature/devflow/workflow_dev.go`, `workflow_validation.go` |
| Dispatch (Playwright image swap) | `services/aep-api/internal/feature/codingagent/coding_executor.go` |
| Merge-skips-build + derived status | `services/aep-api/internal/feature/execution/events.go`, `internal/feature/task/reads.go` |
| Oracle authoring skill | `skills/validation-criteria/` (vendored into `services/aep-api/skills/embedded/`) |
| Runner skills + report generator | `runners/remote-worker/plugin/skills/{aep-validation,playwright-authoring,playwright-healing,playwright-cli}/` |
| Validation runner image | `runners/remote-worker/Dockerfile.validation` |
