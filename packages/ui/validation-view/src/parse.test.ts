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

import { describe, it, expect } from "vitest";
import { parseValidationCriteria } from "./parse.js";

const FULL = JSON.stringify({
  requirements: [
    {
      id: "REQ-001",
      statement: "The system supports multiple structured risk registers.",
      criteria: [
        { id: "AC-001-a", must: "A user can create a Cloud register", method: "e2e", covered: false },
        { id: "AC-001-b", must: "A user can create a Security register", method: "e2e", covered: true },
      ],
    },
    {
      id: "REQ-013",
      statement: "The system provides dashboards.",
      criteria: [
        { id: "AC-013-a", must: "A dashboard shows overall posture", method: "e2e", covered: false },
        { id: "AC-013-b", must: "A dashboard highlights recurring areas", method: "scenario" },
      ],
    },
    {
      id: "REQ-021",
      statement: "Sensitive data is protected.",
      criteria: [
        { id: "AC-021-b", must: "Data in transit is encrypted", method: "manual" },
      ],
    },
  ],
});

describe("parseValidationCriteria", () => {
  it("round-trips a full validation-criteria.json", () => {
    const d = parseValidationCriteria(FULL);
    expect("kind" in d).toBe(false); // not a ParseError
    if ("kind" in d) return;
    expect(d.requirements).toHaveLength(3);
    const req1 = d.requirements[0]!;
    expect(req1.id).toBe("REQ-001");
    expect(req1.statement).toBe(
      "The system supports multiple structured risk registers.",
    );
    expect(req1.criteria).toHaveLength(2);
    const covered = req1.criteria.find((c) => c.id === "AC-001-b")!;
    expect(covered.method).toBe("e2e");
    expect(covered.covered).toBe(true);
  });

  it("keeps `covered` only when it is a boolean (scenario/manual have none)", () => {
    const d = parseValidationCriteria(FULL);
    if ("kind" in d) throw new Error("unexpected parse error");
    const scenario = d.requirements[1]!.criteria.find(
      (c) => c.id === "AC-013-b",
    )!;
    expect(scenario.method).toBe("scenario");
    expect(scenario.covered).toBeUndefined();
    const manual = d.requirements[2]!.criteria[0]!;
    expect(manual.method).toBe("manual");
    expect(manual.covered).toBeUndefined();
  });

  it("skips requirements missing an id or statement", () => {
    const d = parseValidationCriteria(
      JSON.stringify({
        requirements: [
          { statement: "no id" },
          { id: "REQ-002" },
          { id: "REQ-003", statement: "ok", criteria: [] },
        ],
      }),
    );
    if ("kind" in d) throw new Error("unexpected parse error");
    expect(d.requirements).toEqual([
      { id: "REQ-003", statement: "ok", criteria: [] },
    ]);
  });

  it("skips criteria missing an id or must, and defaults criteria to []", () => {
    const d = parseValidationCriteria(
      JSON.stringify({
        requirements: [
          {
            id: "REQ-001",
            statement: "s",
            criteria: [
              { id: "AC-001-a", method: "e2e" },
              { must: "no id", method: "e2e" },
              { id: "AC-001-b", must: "kept", method: "e2e" },
            ],
          },
          { id: "REQ-002", statement: "no criteria field" },
        ],
      }),
    );
    if ("kind" in d) throw new Error("unexpected parse error");
    expect(d.requirements[0]!.criteria).toEqual([
      { id: "AC-001-b", must: "kept", method: "e2e" },
    ]);
    expect(d.requirements[1]!.criteria).toEqual([]);
  });

  it("labels a criterion with no method as unknown", () => {
    const d = parseValidationCriteria(
      JSON.stringify({
        requirements: [
          { id: "REQ-001", statement: "s", criteria: [{ id: "AC-001-a", must: "x" }] },
        ],
      }),
    );
    if ("kind" in d) throw new Error("unexpected parse error");
    expect(d.requirements[0]!.criteria[0]!.method).toBe("unknown");
  });

  it("preserves an unrecognized method string", () => {
    const d = parseValidationCriteria(
      JSON.stringify({
        requirements: [
          {
            id: "REQ-001",
            statement: "s",
            criteria: [{ id: "AC-001-a", must: "x", method: "smoke" }],
          },
        ],
      }),
    );
    if ("kind" in d) throw new Error("unexpected parse error");
    expect(d.requirements[0]!.criteria[0]!.method).toBe("smoke");
  });

  it("returns a ParseError on malformed JSON", () => {
    const d = parseValidationCriteria("{ not json");
    expect("kind" in d && d.kind).toBe("parse-error");
  });

  it("returns a ParseError when the top level is not an object", () => {
    expect("kind" in parseValidationCriteria("42")).toBe(true);
    expect("kind" in parseValidationCriteria("[]")).toBe(true);
  });

  it("returns a ParseError when `requirements` is missing or not an array", () => {
    expect("kind" in parseValidationCriteria(JSON.stringify({}))).toBe(true);
    expect(
      "kind" in parseValidationCriteria(JSON.stringify({ requirements: {} })),
    ).toBe(true);
  });
});
