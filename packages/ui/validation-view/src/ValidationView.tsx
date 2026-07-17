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

import { useMemo } from "react";
import { Alert, Box, Chip, Typography } from "@wso2/oxygen-ui";
import { Check } from "@wso2/oxygen-ui-icons-react";
import {
  parseValidationCriteria,
  type Criterion,
  type Requirement,
  type ValidationCriteria,
} from "./parse.js";

// Solid background per verification method. Text color is computed for contrast
// (getContrastText), so labels stay readable in both themes — the same approach
// as the DesignView type badges and the OpenAPI viewer's method badges. None of
// these is green, so they don't collide with the success-colored "covered" chip.
const METHOD_COLOR: Record<string, string> = {
  e2e: "#1976d2",
  scenario: "#ed6c02",
  manual: "#7b1fa2",
};
// Fixed display order for the summary tally; unknown methods sort after these.
const METHOD_ORDER = ["e2e", "scenario", "manual"];
// Requirement ids are structural, so a muted slate keeps them from competing
// with the colored method badges.
const REQ_COLOR = "#546e7a";
const FALLBACK = "#616161";

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

function SolidBadge({ label, color }: { label: string; color: string }) {
  return (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        px: 1,
        py: 0.5,
        borderRadius: 1,
        flexShrink: 0,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        bgcolor: color,
        color: theme.palette.getContrastText(color),
      })}
    >
      {label}
    </Box>
  );
}

function MethodBadge({ method }: { method: string }) {
  return <SolidBadge label={method} color={METHOD_COLOR[method] ?? FALLBACK} />;
}

// One acceptance criterion: method badge, its id, and the atomic assertion.
// e2e criteria that a validation run has covered get a success chip; scenario
// and manual criteria never carry `covered`.
function CriterionRow({ criterion }: { criterion: Criterion }) {
  return (
    <Box
      sx={{
        display: "flex",
        gap: 1.5,
        alignItems: "flex-start",
        py: 0.75,
      }}
    >
      <Box sx={{ minWidth: 92, flexShrink: 0, pt: "1px" }}>
        <MethodBadge method={criterion.method} />
      </Box>
      <Typography component="span" sx={{ ...mono, flexShrink: 0 }}>
        {criterion.id}
      </Typography>
      <Typography variant="body2" sx={{ flexGrow: 1 }}>
        {criterion.must}
      </Typography>
      {criterion.covered && (
        <Chip
          size="small"
          variant="outlined"
          color="success"
          icon={<Check size={14} />}
          label="covered"
          sx={{ flexShrink: 0 }}
        />
      )}
    </Box>
  );
}

function RequirementCard({ requirement }: { requirement: Requirement }) {
  const count = requirement.criteria.length;
  return (
    <Box
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        p: 2,
        mb: 1.5,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 0.75 }}>
        <SolidBadge label={requirement.id} color={REQ_COLOR} />
        <Typography variant="caption" color="text.secondary">
          {count} {count === 1 ? "criterion" : "criteria"}
        </Typography>
      </Box>
      <Typography variant="body1" sx={{ fontWeight: 500, mb: count > 0 ? 1 : 0 }}>
        {requirement.statement}
      </Typography>
      {count === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No criteria.
        </Typography>
      ) : (
        requirement.criteria.map((c) => (
          <CriterionRow key={c.id} criterion={c} />
        ))
      )}
    </Box>
  );
}

function ValidationBody({ criteria }: { criteria: ValidationCriteria }) {
  const { requirements } = criteria;
  // Per-method tally for the summary header (kept in a stable order).
  const { total, orderedMethods, methodCounts } = useMemo(() => {
    const counts = new Map<string, number>();
    let n = 0;
    for (const r of requirements) {
      for (const c of r.criteria) {
        n += 1;
        counts.set(c.method, (counts.get(c.method) ?? 0) + 1);
      }
    }
    const ordered = [
      ...METHOD_ORDER.filter((m) => counts.has(m)),
      ...[...counts.keys()].filter((m) => !METHOD_ORDER.includes(m)).sort(),
    ];
    return { total: n, orderedMethods: ordered, methodCounts: counts };
  }, [requirements]);

  const reqCount = requirements.length;
  return (
    <Box sx={{ height: "100%", overflow: "auto", p: 3 }}>
      <Box sx={{ maxWidth: 960, mx: "auto" }}>
        <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
          Validation Criteria
        </Typography>

        {/* Summary — totals plus a colored tally per verification method */}
        <Box
          sx={{
            mt: 1,
            mb: 3,
            display: "flex",
            alignItems: "center",
            gap: 1.5,
            flexWrap: "wrap",
          }}
        >
          <Typography variant="body2" color="text.secondary">
            {reqCount} {reqCount === 1 ? "requirement" : "requirements"} ·{" "}
            {total} {total === 1 ? "criterion" : "criteria"}
          </Typography>
          {orderedMethods.map((m) => (
            <SolidBadge
              key={m}
              label={`${m} ${methodCounts.get(m) ?? 0}`}
              color={METHOD_COLOR[m] ?? FALLBACK}
            />
          ))}
        </Box>

        {reqCount === 0 ? (
          <Typography variant="body2" color="text.secondary">
            No validation criteria.
          </Typography>
        ) : (
          requirements.map((r) => (
            <RequirementCard key={r.id} requirement={r} />
          ))
        )}
      </Box>
    </Box>
  );
}

export interface ValidationViewProps {
  /** Raw validation-criteria.json text. */
  criteria: string;
}

export function ValidationView({ criteria }: ValidationViewProps) {
  const parsed = useMemo(() => parseValidationCriteria(criteria), [criteria]);
  if ("kind" in parsed) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          Couldn't parse validation-criteria.json: {parsed.message}
        </Alert>
      </Box>
    );
  }
  return <ValidationBody criteria={parsed} />;
}
