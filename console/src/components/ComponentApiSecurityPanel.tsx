import { useMemo } from 'react';
import { Box, Button, Chip, Stack, Typography } from '@wso2/oxygen-ui';
import yaml from 'js-yaml';
import { componentDesignPath } from '../lib/designDocumentTypes';

// ComponentApiSecurityPanel — surface api.security per component on the
// architecture page. Reads each per-component design.md from `files`,
// parses the YAML frontmatter, and renders a status chip + a toggle button
// that rewrites the frontmatter via `onComponentSecurityChange`.
//
// Display rules:
//   - api.security: required → 🔒 Protected (cluster gateway enforces JWT)
//   - absent / anything else → 🌐 Public (no AP hop)
//
// Wiring: see ProjectArchitecturePage; this panel renders inside the Cell
// Diagram view, so the toggle and the diagram stay glanceable together.
// The toggle calls `onComponentSecurityChange(componentName, newContent)`
// — same shape as `handleFileChange`, so the existing auto-save +
// trait_sync pipeline kicks in unchanged.

interface Props {
  componentNames: string[];
  files: Record<string, string>;
  onComponentSecurityChange: (componentName: string, newContent: string) => void;
  disabled?: boolean;
}

interface ParsedFrontmatter {
  raw: string;
  parsed: Record<string, unknown>;
  body: string;
}

function splitFrontmatter(content: string): ParsedFrontmatter {
  const trimmed = content.replace(/^﻿/, '');
  if (!trimmed.startsWith('---')) {
    return { raw: '', parsed: {}, body: content };
  }
  const rest = trimmed.slice(3).replace(/^[ \t]*\r?\n/, '');
  const endIdx = rest.indexOf('\n---');
  if (endIdx < 0) {
    return { raw: '', parsed: {}, body: content };
  }
  const raw = rest.slice(0, endIdx);
  let body = rest.slice(endIdx + '\n---'.length);
  body = body.replace(/^\r?\n/, '');
  let parsed: Record<string, unknown> = {};
  try {
    const loaded = yaml.load(raw);
    if (loaded && typeof loaded === 'object') {
      parsed = loaded as Record<string, unknown>;
    }
  } catch {
    parsed = {};
  }
  return { raw, parsed, body };
}

function joinFrontmatter(parsed: Record<string, unknown>, body: string): string {
  const fm = yaml.dump(parsed, { lineWidth: 1000 }).trimEnd();
  const trimmedBody = body.replace(/^[\r\n]+/, '');
  if (!fm || fm === '{}') {
    return trimmedBody;
  }
  return `---\n${fm}\n---\n\n${trimmedBody}`;
}

function isProtected(parsed: Record<string, unknown>): boolean {
  const api = parsed.api as Record<string, unknown> | undefined;
  if (!api) return false;
  const sec = (api.security ?? '').toString().trim().toLowerCase();
  return sec === 'required';
}

export default function ComponentApiSecurityPanel({
  componentNames,
  files,
  onComponentSecurityChange,
  disabled,
}: Props) {
  const rows = useMemo(() => {
    return componentNames
      .map((name) => {
        const path = componentDesignPath(name);
        const content = files[path];
        if (content == null) return null;
        const parts = splitFrontmatter(content);
        return {
          name,
          path,
          protected: isProtected(parts.parsed),
          parts,
          content,
        };
      })
      .filter((r): r is Exclude<typeof r, null> => r != null);
  }, [componentNames, files]);

  if (rows.length === 0) return null;

  return (
    <Box
      data-testid="api-security-panel"
      sx={{ width: '100%', maxWidth: 816, py: 1.5, px: 0 }}
    >
      <Typography
        variant="overline"
        component="h3"
        sx={{
          m: 0,
          mb: 1,
          color: 'text.secondary',
          letterSpacing: '0.08em',
          fontSize: 11,
          fontWeight: 600,
        }}
      >
        API Security
      </Typography>
      <Stack spacing={0.75}>
        {rows.map((row) => {
          const next = !row.protected;
          const nextParsed = { ...row.parts.parsed };
          if (next) {
            nextParsed.api = { security: 'required' };
          } else {
            delete nextParsed.api;
          }
          const nextContent = joinFrontmatter(nextParsed, row.parts.body);

          return (
            <Stack
              key={row.name}
              direction="row"
              alignItems="center"
              gap={1}
              data-testid={`api-security-row-${row.name}`}
              sx={{ minHeight: 32 }}
            >
              <Typography variant="body2" sx={{ flex: '0 0 auto', minWidth: 140 }}>
                {row.name}
              </Typography>
              <Chip
                size="small"
                data-testid={`api-security-chip-${row.name}`}
                label={row.protected ? 'Protected' : 'Public'}
                color={row.protected ? 'success' : 'default'}
                variant={row.protected ? 'filled' : 'outlined'}
                sx={{ fontWeight: 500 }}
              />
              <Box sx={{ flexGrow: 1 }} />
              <Button
                size="small"
                variant="outlined"
                disabled={disabled}
                data-testid={`api-security-toggle-${row.name}`}
                onClick={() => onComponentSecurityChange(row.name, nextContent)}
              >
                {row.protected ? 'Make public' : 'Make protected'}
              </Button>
            </Stack>
          );
        })}
      </Stack>
    </Box>
  );
}
