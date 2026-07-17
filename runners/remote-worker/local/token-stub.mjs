/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Local-dev credential stub. Stands in for the platform's
// credentials/refresh endpoint so the runner can be exercised without
// the BFF/git-service:
//
//   POST /internal/v1/tasks/{taskId}/credentials/refresh
//     -> { "token": $GITHUB_PAT, "taskId": <echoed from path> }
//
// For a validation run it also plays the validation-context endpoint so the
// aep-validation skill can fetch its deployed endpoints (never in the issue):
//
//   GET /internal/v1/executions/{id}/validation-context
//     -> { endpoints:[{component,url}], credentials:null, criteriaPath }
//
// The endpoints point at localhost dev servers the agent starts in-container
// (the local-dev-servers path in the aep-validation skill); credentials are
// null (auth-gated criteria then land not_run). Override the endpoints with
// VALIDATION_CONTEXT_JSON to validate a different sample.
//
// The taskId echo satisfies credhelper.sh's anti-misroute tripwire.
// Identity fields are deliberately omitted so the runner keeps the
// AEP_IDENTITY_* values it was launched with (no drift rewrite).
//
// SECURITY: every response carries a real GitHub PAT. Keep the bind
// address on loopback (the default) and never expose this beyond your
// machine. When STUB_BEARER is set (run-local.sh always sets it to the
// per-run AEP_BEARER), callers must present that exact
// `Authorization: Bearer` value — the runner already sends it on every
// refresh call, so this costs nothing and de-fangs a non-loopback bind.

import http from "node:http";

const pat = process.env.GITHUB_PAT ?? "";
if (pat === "") {
  console.error("[token-stub] GITHUB_PAT is not set");
  process.exit(1);
}
const port = Number(process.env.STUB_PORT || 8377);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  console.error(`[token-stub] STUB_PORT is not a valid port: ${process.env.STUB_PORT}`);
  process.exit(1);
}
const bind = process.env.STUB_BIND || "127.0.0.1";
const expectedBearer = process.env.STUB_BEARER ?? "";

// Both scopings: the runner uses tasks/{id} when AEP_PLATFORM_URL is unset
// (git-service fallback) and executions/{id} when it is set (the current
// execution-keyed model). Match either so the stub serves both run shapes.
const REFRESH_RE = /^\/internal\/v1\/(?:tasks|executions)\/([^/]+)\/credentials\/refresh$/;
const VALIDATION_CONTEXT_RE = /^\/internal\/v1\/executions\/([^/]+)\/validation-context$/;
const VALIDATION_REPORT_RE = /^\/internal\/v1\/executions\/([^/]+)\/validation-report$/;
const JSON_HEADERS = { "Content-Type": "application/json", "Cache-Control": "no-store" };

// The validation-context payload the stub returns. Localhost dev servers the
// agent starts in its own container; override via VALIDATION_CONTEXT_JSON.
const validationContext = process.env.VALIDATION_CONTEXT_JSON
  ? JSON.parse(process.env.VALIDATION_CONTEXT_JSON)
  : {
      endpoints: [
        { component: "hello-web", url: "http://localhost:5173" },
        { component: "hello-api", url: "http://localhost:9090" },
      ],
      credentials: null,
      criteriaPath: "specs/validation/validation-criteria.json",
    };

const server = http.createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://localhost");
  if (req.method === "GET" && url.pathname === "/healthz") {
    res.writeHead(200).end("ok");
    return;
  }
  // Runner callbacks that require the per-run bearer: credentials/refresh
  // (POST), validation-context (GET), validation-report (POST). Everything
  // else 404s.
  const refreshM = req.method === "POST" ? REFRESH_RE.exec(url.pathname) : null;
  const contextM = req.method === "GET" ? VALIDATION_CONTEXT_RE.exec(url.pathname) : null;
  const reportM = req.method === "POST" ? VALIDATION_REPORT_RE.exec(url.pathname) : null;
  if (!refreshM && !contextM && !reportM) {
    console.error(`[token-stub] 404 ${req.method} ${url.pathname}`);
    res.writeHead(404, JSON_HEADERS).end('{"error":"not found"}');
    return;
  }
  if (expectedBearer !== "" && req.headers.authorization !== `Bearer ${expectedBearer}`) {
    console.error(`[token-stub] 401 bad or missing bearer for ${url.pathname}`);
    res.writeHead(401, JSON_HEADERS).end('{"error":"unauthorized"}');
    return;
  }
  if (contextM) {
    console.error(`[token-stub] 200 validation-context for execution ${contextM[1]}`);
    res.writeHead(200, JSON_HEADERS).end(JSON.stringify(validationContext));
    return;
  }
  if (reportM) {
    // Drain the body so the runner's POST completes, then ack.
    req.resume();
    req.on("end", () => {
      console.error(`[token-stub] 200 validation-report for execution ${reportM[1]}`);
      res.writeHead(200, JSON_HEADERS).end('{"ok":true}');
    });
    return;
  }
  const m = refreshM;
  let taskId;
  try {
    taskId = decodeURIComponent(m[1]);
  } catch {
    console.error(`[token-stub] 400 malformed task id segment`);
    res.writeHead(400, JSON_HEADERS).end('{"error":"malformed task id"}');
    return;
  }
  console.error(`[token-stub] 200 refresh for task ${taskId}`);
  res.writeHead(200, JSON_HEADERS);
  res.end(JSON.stringify({ token: pat, taskId }));
});

server.on("error", (err) => {
  console.error(`[token-stub] server error: ${err.message}`);
  process.exit(1);
});

server.listen(port, bind, () => {
  console.error(`[token-stub] listening on http://${bind}:${port}`);
});
