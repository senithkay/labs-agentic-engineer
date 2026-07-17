#!/usr/bin/env node
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

// generate-report.mjs — deterministic validation report generator.
//
// Maps a Playwright JSON reporter run back onto the acceptance oracle
// (specs/validation/validation-criteria.json) and emits the validation
// report. This script is copied VERBATIM into the project repo at
// tests/e2e/scripts/generate-report.mjs by the aep-validation skill and
// run there — the agent never authors the report by hand, so statuses
// cannot drift from what the test run actually produced.
//
// The oracle is READ-ONLY input: this script reads it but never writes it.
// The validation phase must not touch specs/ — coverage is expressed by the
// report's per-criterion pass/fail, not by a flag written back into the oracle.
//
// Inputs (paths relative to the project repo root, override via flags):
//   --issue <n>       validation issue number (required)
//   --commit <sha>    commit under validation (default: "unknown")
//   --criteria <p>    default specs/validation/validation-criteria.json (read-only)
//   --results <p>     default tests/e2e/test-results/results.json
//   --heal-log <p>    default tests/e2e/heal-log.json (optional file)
//   --out <dir>       default tests/validation
//
// Outputs:
//   <out>/report.json          machine-readable report (schemaVersion 1)
//   <out>/report.md            human report incl. manual checklist
//
// Join key: every automated spec's title MUST start with "<AC-ID>: "
// (e.g. "AC-001-a: shows a name text box"). Duplicate or unknown AC ids
// are hard errors (exit 2) — fix the spec titles, then re-run.
//
// Each mapped spec file must also carry a "// spec:" header comment
// linking it to its test-plan section (hard error when absent — add the
// header and regenerate; no test re-run needed). Raw page.locator()
// usage is reported as a warning: semantic locators (getByRole/
// getByLabel/...) survive UI change, raw CSS is what the healer ends
// up fixing later.

import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";

const AC_TITLE_RE = /^(AC-\d{3}-[a-z]):/;

function parseArgs(argv) {
  const args = {
    criteria: "specs/validation/validation-criteria.json",
    results: "tests/e2e/test-results/results.json",
    healLog: "tests/e2e/heal-log.json",
    out: "tests/validation",
    commit: "unknown",
    issue: null,
  };
  for (let i = 2; i < argv.length; i += 2) {
    const key = argv[i];
    const val = argv[i + 1];
    if (val === undefined) fail(`missing value for ${key}`);
    switch (key) {
      case "--issue": args.issue = Number(val); break;
      case "--commit": args.commit = val; break;
      case "--criteria": args.criteria = val; break;
      case "--results": args.results = val; break;
      case "--heal-log": args.healLog = val; break;
      case "--out": args.out = val; break;
      default: fail(`unknown flag: ${key}`);
    }
  }
  if (!Number.isInteger(args.issue) || args.issue <= 0) {
    fail("--issue <positive integer> is required");
  }
  return args;
}

function fail(msg) {
  console.error(`generate-report: ${msg}`);
  process.exit(2);
}

function readJson(p, what) {
  if (!existsSync(p)) fail(`${what} not found: ${p}`);
  try {
    return JSON.parse(readFileSync(p, "utf8"));
  } catch (err) {
    fail(`${what} is not valid JSON (${p}): ${err.message}`);
  }
}

function stripAnsi(s) {
  // eslint-disable-next-line no-control-regex
  return String(s).replace(/\[[0-9;]*m/g, "");
}

// Recursively collect every spec from the Playwright JSON reporter tree.
function collectSpecs(suite, acc, fileFromParent) {
  const file = suite.file ?? fileFromParent;
  for (const spec of suite.specs ?? []) {
    acc.push({ ...spec, file: spec.file ?? file });
  }
  for (const child of suite.suites ?? []) {
    collectSpecs(child, acc, file);
  }
  return acc;
}

// Aggregate one reporter spec (possibly multiple projects/tests) into a
// single outcome: fail > pass(flaky) > pass > not_run.
function specOutcome(spec) {
  const tests = spec.tests ?? [];
  const statuses = tests.map((t) => t.status);
  let status;
  if (statuses.includes("unexpected")) status = "fail";
  else if (statuses.includes("expected") || statuses.includes("flaky")) status = "pass";
  else status = "not_run"; // skipped only

  const flaky = status === "pass" && statuses.includes("flaky");

  let failure = null;
  let durationMs = 0;
  for (const t of tests) {
    for (const r of t.results ?? []) {
      durationMs += r.duration ?? 0;
      if (!failure && (r.error || (r.errors ?? []).length > 0)) {
        const err = r.error ?? r.errors[0];
        failure = {
          message: stripAnsi(err.message ?? "unknown error").slice(0, 2000),
          location: spec.file ? `${spec.file}:${spec.line ?? 0}` : null,
        };
      }
    }
  }
  if (status !== "fail") failure = null;
  return { status, flaky, failure, durationMs };
}

function escapeCell(s) {
  return String(s).replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

function main() {
  const args = parseArgs(process.argv);

  const criteriaDoc = readJson(args.criteria, "criteria file");
  const results = readJson(args.results, "Playwright results");
  const healEntries = existsSync(args.healLog)
    ? readJson(args.healLog, "heal log")
    : [];
  if (!Array.isArray(healEntries)) fail("heal log must be a JSON array");

  // ---- index criteria ----------------------------------------------------
  const criteriaById = new Map();
  for (const req of criteriaDoc.requirements ?? []) {
    for (const c of req.criteria ?? []) {
      if (criteriaById.has(c.id)) fail(`duplicate criterion id in criteria file: ${c.id}`);
      criteriaById.set(c.id, { req, criterion: c });
    }
  }
  if (criteriaById.size === 0) fail("criteria file has no criteria");

  // ---- index test results by AC id ---------------------------------------
  const specs = [];
  for (const suite of results.suites ?? []) collectSpecs(suite, specs);

  const resultByAc = new Map();
  const unmappedTests = [];
  const errors = [];
  for (const spec of specs) {
    const m = AC_TITLE_RE.exec(spec.title ?? "");
    if (!m) {
      unmappedTests.push({ title: spec.title ?? "", file: spec.file ?? null });
      continue;
    }
    const acId = m[1];
    if (resultByAc.has(acId)) {
      errors.push(`duplicate spec title prefix ${acId} (files: ${resultByAc.get(acId).file}, ${spec.file})`);
      continue;
    }
    if (!criteriaById.has(acId)) {
      errors.push(`spec title references unknown criterion ${acId} (${spec.file})`);
      continue;
    }
    resultByAc.set(acId, { ...specOutcome(spec), file: spec.file ?? null });
  }
  if (errors.length > 0) {
    for (const e of errors) console.error(`generate-report: ${e}`);
    process.exit(2);
  }

  // Heal-log entries must join to criteria AND carry full provenance — reject
  // free-form shapes (an entry the report can't attribute, or a heal without
  // spec/classification/change/commit, is an invisible heal).
  const HEAL_FIELDS = ["criterionId", "spec", "classification", "change", "commit"];
  const healByAc = new Map();
  for (const [i, h] of healEntries.entries()) {
    if (h === null || typeof h !== "object" || Array.isArray(h)) {
      fail(
        `heal-log entry ${i} is not an object — ` +
          `each entry must be {criterionId, spec, classification, change, commit}`,
      );
    }
    if (!h.criterionId || !criteriaById.has(h.criterionId)) {
      fail(
        `heal-log entry ${i} has ${h.criterionId ? `unknown criterionId "${h.criterionId}"` : "no criterionId"} — ` +
          `each entry must be {criterionId, spec, classification, change, commit} with a criterionId from the criteria file`,
      );
    }
    const missing = HEAL_FIELDS.filter(
      (f) => typeof h[f] !== "string" || h[f].trim() === "",
    );
    if (missing.length > 0) {
      fail(
        `heal-log entry ${i} (${h.criterionId}) is missing required field(s): ${missing.join(", ")} — ` +
          `each entry must be {criterionId, spec, classification, change, commit}, all non-empty strings`,
      );
    }
    const list = healByAc.get(h.criterionId) ?? [];
    list.push(h);
    healByAc.set(h.criterionId, list);
  }

  // ---- heal visibility ----------------------------------------------------
  // A spec that EXISTED at the base ref and was modified this run is a heal
  // by definition — it MUST have a heal-log entry, or a silent change (e.g.
  // a weakened assertion) would ship inside a clean report. The git diff
  // filter (--diff-filter=M = modified, not added) is exactly the
  // "pre-existing spec was changed" signal; a brand-new spec is an addition
  // (A) and is not gated. Diffed against the origin default branch (committed
  // and uncommitted changes both count).
  let healCheckSkipped = false;
  {
    let modified = null;
    for (const base of ["origin/HEAD", "origin/main", "origin/master"]) {
      try {
        modified = execFileSync(
          "git",
          ["diff", "--name-only", "--diff-filter=M", base, "--"],
          { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
        ).split("\n");
        break;
      } catch {
        // ref missing — try the next candidate
      }
    }
    if (modified === null) {
      // not a git repo / no origin ref — degrade visibly, never silently
      healCheckSkipped = true;
    } else {
      for (const p of modified) {
        const m = /(AC-\d{3}-[a-z])\.spec\.[cm]?[tj]sx?$/.exec(p);
        if (!m) continue;
        if (!healByAc.has(m[1])) {
          errors.push(
            `${p} (criterion ${m[1]}) is a pre-existing spec modified this run but has no heal-log entry — ` +
              `record the heal in tests/e2e/heal-log.json {criterionId, spec, classification, change, commit} and regenerate`,
          );
        }
      }
    }
  }

  // ---- spec-file conventions (header hard-check, locator lint) -----------
  // Reporter file paths are relative to the run's rootDir (usually the
  // testDir, e.g. tests/e2e/specs), NOT the repo root — resolve against
  // rootDir first, then the conventional layouts.
  const rootDir = results.config?.rootDir;
  function resolveSpecPath(file) {
    const candidates = [];
    if (rootDir) candidates.push(path.resolve(rootDir, file));
    candidates.push(
      path.resolve("tests/e2e/specs", file),
      path.resolve("tests/e2e", file),
    );
    for (const c of candidates) {
      if (existsSync(c)) return c;
    }
    return null;
  }
  const warnings = [];
  if (healCheckSkipped) {
    warnings.push("heal-visibility check skipped (no git origin ref available)");
  }
  for (const [acId, r] of resultByAc) {
    if (!r.file) continue;
    const abs = resolveSpecPath(r.file);
    if (!abs) {
      warnings.push(`${acId}: spec file not found on disk (${r.file}) — header/locator checks skipped`);
      continue;
    }
    r.repoPath = path.relative(process.cwd(), abs).split(path.sep).join("/");
    const src = readFileSync(abs, "utf8");
    const head = src.split("\n").slice(0, 10);
    if (!head.some((l) => l.startsWith("// spec:"))) {
      errors.push(
        `${r.repoPath} is missing its "// spec:" header comment (link to the test-plan section for ${acId})`,
      );
    }
    if (/\.locator\(/.test(src)) {
      warnings.push(
        `${acId}: raw locator() usage in ${r.repoPath} — prefer getByRole/getByLabel/getByPlaceholder`,
      );
    }
  }
  if (errors.length > 0) {
    for (const e of errors) console.error(`generate-report: ${e}`);
    process.exit(2);
  }

  // ---- build per-criterion rows ------------------------------------------
  const rows = [];
  const totals = { e2e: { total: 0, pass: 0, fail: 0, notRun: 0 }, manual: 0, scenario: 0 };
  for (const req of criteriaDoc.requirements ?? []) {
    for (const c of req.criteria ?? []) {
      const heals = healByAc.get(c.id) ?? [];
      const row = {
        id: c.id,
        requirementId: req.id,
        must: c.must,
        method: c.method,
        status: null,
        spec: null,
        healed: heals.length > 0,
        healAttempts: heals.length,
        flaky: false,
        durationMs: 0,
        failure: null,
      };
      if (c.method === "e2e") {
        totals.e2e.total += 1;
        const r = resultByAc.get(c.id);
        if (!r) {
          row.status = "not_run";
          totals.e2e.notRun += 1;
        } else {
          row.status = r.status;
          row.spec = r.repoPath ?? (r.file ? path.posix.join("tests/e2e", r.file) : null);
          row.flaky = r.flaky;
          row.durationMs = r.durationMs;
          row.failure = r.failure;
          if (r.status === "pass") totals.e2e.pass += 1;
          else if (r.status === "fail") totals.e2e.fail += 1;
          else totals.e2e.notRun += 1;
        }
      } else if (c.method === "manual") {
        row.status = "manual";
        totals.manual += 1;
      } else {
        row.status = "not_validated";
        totals.scenario += 1;
      }
      rows.push(row);
    }
  }

  // ---- report.json ---------------------------------------------------------
  const report = {
    schemaVersion: 1,
    issue: args.issue,
    commit: args.commit,
    generatedAt: new Date().toISOString(),
    playwrightVersion: results.config?.version ?? "unknown",
    totals,
    criteria: rows,
    ...(warnings.length > 0 ? { warnings } : {}),
    ...(unmappedTests.length > 0 ? { unmappedTests } : {}),
  };
  mkdirSync(args.out, { recursive: true });
  const reportJsonPath = path.join(args.out, "report.json");
  writeFileSync(reportJsonPath, JSON.stringify(report, null, 2) + "\n");

  // ---- report.md -------------------------------------------------------------
  const md = [];
  md.push(`# Validation report`);
  md.push("");
  md.push(`- **Issue:** #${args.issue}`);
  md.push(`- **Commit:** ${args.commit}`);
  md.push(`- **Generated:** ${report.generatedAt}`);
  md.push(`- **Playwright:** ${report.playwrightVersion}`);
  md.push("");
  md.push(`## Summary`);
  md.push("");
  md.push(`| Method | Total | Pass | Fail | Not run |`);
  md.push(`|---|---|---|---|---|`);
  md.push(`| e2e | ${totals.e2e.total} | ${totals.e2e.pass} | ${totals.e2e.fail} | ${totals.e2e.notRun} |`);
  md.push(`| manual (human checklist) | ${totals.manual} | — | — | — |`);
  md.push(`| scenario (not validated) | ${totals.scenario} | — | — | — |`);
  md.push("");

  const e2eRows = rows.filter((r) => r.method === "e2e");
  if (e2eRows.length > 0) {
    md.push(`## E2E results`);
    md.push("");
    md.push(`| Criterion | Must | Status | Spec | Notes |`);
    md.push(`|---|---|---|---|---|`);
    for (const r of e2eRows) {
      const notes = [];
      if (r.healed) notes.push(`healed ×${r.healAttempts}`);
      if (r.flaky) notes.push("flaky");
      const statusIcon = r.status === "pass" ? "✅ pass" : r.status === "fail" ? "❌ fail" : "⏭️ not_run";
      md.push(
        `| ${r.id} | ${escapeCell(r.must)} | ${statusIcon} | ${r.spec ? `\`${r.spec}\`` : "—"} | ${notes.join(", ") || "—"} |`,
      );
    }
    md.push("");
  }

  const failures = e2eRows.filter((r) => r.status === "fail");
  if (failures.length > 0) {
    md.push(`## Failures`);
    md.push("");
    for (const r of failures) {
      md.push(`### ${r.id} — ${r.must}`);
      md.push("");
      if (r.spec) md.push(`Spec: \`${r.spec}\``);
      if (r.failure?.location) md.push(`Location: \`${r.failure.location}\``);
      md.push("");
      md.push("```");
      md.push((r.failure?.message ?? "no error captured").split("\n").slice(0, 20).join("\n"));
      md.push("```");
      md.push("");
    }
  }

  const manualRows = rows.filter((r) => r.method === "manual");
  if (manualRows.length > 0) {
    md.push(`## Manual checklist`);
    md.push("");
    for (const r of manualRows) {
      md.push(`- [ ] **${r.id}** — ${r.must}`);
    }
    md.push("");
  }

  const scenarioRows = rows.filter((r) => r.method === "scenario");
  if (scenarioRows.length > 0) {
    md.push(`## Not validated (scenario)`);
    md.push("");
    md.push(`These criteria need agentic/exploratory judgment and are out of scope for this automated run:`);
    md.push("");
    for (const r of scenarioRows) {
      md.push(`- **${r.id}** — ${r.must}`);
    }
    md.push("");
  }

  if (healEntries.length > 0) {
    md.push(`## Healing log`);
    md.push("");
    md.push(`| Criterion | Classification | Change | Commit |`);
    md.push(`|---|---|---|---|`);
    for (const h of healEntries) {
      md.push(
        `| ${h.criterionId ?? "—"} | ${escapeCell(h.classification ?? "—")} | ${escapeCell(h.change ?? "—")} | ${h.commit ? `\`${String(h.commit).slice(0, 8)}\`` : "—"} |`,
      );
    }
    md.push("");
  }

  if (warnings.length > 0) {
    md.push(`## Warnings`);
    md.push("");
    for (const w of warnings) {
      md.push(`- ${escapeCell(w)}`);
    }
    md.push("");
  }

  if (unmappedTests.length > 0) {
    md.push(`## Unmapped tests (warning)`);
    md.push("");
    md.push(`Tests whose titles carry no \`AC-NNN-x:\` prefix (setup projects are expected here):`);
    md.push("");
    for (const t of unmappedTests) {
      md.push(`- ${escapeCell(t.title)}${t.file ? ` (\`${t.file}\`)` : ""}`);
    }
    md.push("");
  }

  const reportMdPath = path.join(args.out, "report.md");
  writeFileSync(reportMdPath, md.join("\n") + "\n");

  console.log(
    `generate-report: e2e ${totals.e2e.pass}/${totals.e2e.total} passing ` +
      `(${totals.e2e.fail} fail, ${totals.e2e.notRun} not run); ` +
      `wrote ${reportJsonPath}, ${reportMdPath}`,
  );
}

main();
