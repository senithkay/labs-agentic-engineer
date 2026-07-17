# AGENTS.md — services/

| New | Tech |
|---|---|
| `aep-api/`| Go BFF + GitHub webhooks (git ops folded in) |
| `agents/` | TS interactive spec agents (Vercel AI SDK) |
| `collab/`|  TS Yjs collaboration server |

## Conventions

- Config/env parsing in one place per service; add every var to `.env.example`.

## Practices

- Test driven developement is preferred. Write tests first, then implement the feature. Define the contrract first, then write the test case for that contract, then implement the feature. You can tweak along the way.
- API changes are contract-first: edit `packages/contracts/api/`, run `make gen-api`, and let the strict-server compile errors drive the handler updates.


