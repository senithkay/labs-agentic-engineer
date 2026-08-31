# aep-mcp-server skills — canonical source of truth

These `SKILL.md` files are **owned by AEP** and consumed by the OpenChoreo
SRE/RCA agent, which drives the handoff MCP tools this server exposes
(`ae_search_related_issues`, `ae_create_issue`, `ae_dispatch_coding_agent`).

They live here — next to the MCP surface whose contract they describe — and
**not** in the repo-root `skills/` agent library, so AEP's own skill reconcile
does not inject them into AEP's coding/design agents.

## issue-fix

`issue-fix/SKILL.md` is the handoff skill: it tells the SRE agent's handoff
sub-agent how to classify config-vs-code root causes, dedupe against related
GitHub issues, file one issue with RCA context and cross-links, and dispatch
the coding agent. Its content is AEP's contract (the `aep:*`/`sre-agent`
labels, the taskmeta block, dedupe keys, unprefixed component names, dispatch
rules), so it belongs with AEP.

## How it reaches the SRE agent (deploy-time mount)

The SRE agent does not bake this skill into its image and does not fetch it at
runtime. Instead the skill is **materialized at deploy time**:

1. This file is rendered into a ConfigMap (`rca-agent-skill-issue-fix`, key
   `SKILL.md`) in the agent's namespace.
2. The ConfigMap is mounted into the agent pod at
   `/etc/rca-agent/skills/issue-fix/`.
3. The agent's `EXTERNAL_SKILLS_DIR=/etc/rca-agent/skills` makes its loader read
   the mounted copy (searched before its built-in `src/skills` library).

This wiring is done by **`aectl sre install`** (gated on `--ae-handoff=true`),
which reads this file, renders the ConfigMap, and strategic-merge-patches the
`ai-rca-agent` Deployment with the volume, mount, and `EXTERNAL_SKILLS_DIR`.
Edit the skill here, re-run `aectl sre install` (or just re-apply the ConfigMap)
and restart the agent — no SRE image rebuild.

> Edit this file, not any copy on the agent side. There is no committed copy in
> the SRE repo; the only other instance is the transient ConfigMap.
