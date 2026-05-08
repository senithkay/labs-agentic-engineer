import { useEffect, useMemo, useRef, useState } from 'react';
import { Box, Stack, Typography, useTheme } from '@wso2/oxygen-ui';
import type { TaskProgressEvent } from '../../services/api';

interface Props {
  agentLines: TaskProgressEvent[];
  buildLines: TaskProgressEvent[];
  agentFinal?: boolean;
  buildFinal?: boolean;
  emptyMessage?: string;
}

function iconFor(kind: TaskProgressEvent['kind']): string {
  switch (kind) {
    case 'phase':       return '⚙️';
    case 'tool_use':    return '🛠️';
    case 'git_commit':  return '📦';
    case 'git_push':    return '🚀';
    case 'gh_action':   return '🐙';
    case 'result':      return '✅';
    case 'build_step':  return '🏗️';
    case 'log':         return '📝';
    default:            return '·';
  }
}

function summaryFor(ev: TaskProgressEvent): string {
  switch (ev.kind) {
    case 'phase':      return ev.phase ?? '';
    case 'tool_use':   return `${ev.tool ?? 'tool'}${ev.summary ? ' · ' + ev.summary : ''}`;
    case 'git_commit': return `Committed${ev.summary ? ': ' + ev.summary : ''}${ev.sha ? ' (' + ev.sha.slice(0, 7) + ')' : ''}`;
    case 'git_push':   return `Pushed${ev.branch ? ' to ' + ev.branch : ''}${ev.sha ? ' (' + ev.sha.slice(0, 7) + ')' : ''}`;
    case 'gh_action':  return ev.command ?? 'gh';
    case 'result':     return ev.summary ?? (ev.status === 'success' ? 'Done' : `Failed${ev.error ? ': ' + ev.error : ''}`);
    case 'build_step': return `${ev.step ?? 'step'}${ev.phase ? ' · ' + ev.phase : ''}${ev.message ? ' · ' + ev.message : ''}`;
    case 'log':        return ev.summary ?? '';
    default:           return ev.summary ?? '';
  }
}

function formatTs(ts: string | undefined): string {
  if (!ts) return '—:—:—';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '—:—:—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function compareEvents(a: TaskProgressEvent, b: TaskProgressEvent): number {
  if (a.ts === b.ts) return a.seq - b.seq;
  return a.ts < b.ts ? -1 : 1;
}

export function TaskActivityFeed({ agentLines, buildLines, agentFinal, buildFinal, emptyMessage }: Props) {
  const theme = useTheme();
  const scrollRef = useRef<HTMLDivElement>(null);
  // Stick-to-bottom: when the user is near the bottom, auto-scroll on new
  // events; if they scroll up to read history, we stop auto-scrolling and
  // surface a "jump to latest" affordance.
  const [stickToBottom, setStickToBottom] = useState(true);

  const merged = useMemo(() => {
    const seen = new Set<string>();
    const out: TaskProgressEvent[] = [];
    for (const ev of [...agentLines, ...buildLines]) {
      const key = `${ev.ts}|${ev.seq}|${ev.kind}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push(ev);
    }
    out.sort(compareEvents);
    return out;
  }, [agentLines, buildLines]);

  useEffect(() => {
    if (!stickToBottom) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [merged.length, stickToBottom]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setStickToBottom(nearBottom);
  };

  if (merged.length === 0) {
    return (
      <Box sx={{ py: 4, textAlign: 'center' }}>
        <Typography variant="body2" color="text.disabled">
          {emptyMessage ?? 'Waiting for activity…'}
        </Typography>
      </Box>
    );
  }

  return (
    <Box
      ref={scrollRef}
      onScroll={handleScroll}
      sx={{ position: 'relative', py: 1, maxHeight: 480, overflowY: 'auto' }}
    >
      <Stack spacing={0.5}>
      {merged.map((ev, i) => (
        <Stack
          key={`${ev.ts}-${ev.seq}-${i}`}
          direction="row"
          spacing={1.5}
          alignItems="flex-start"
          sx={{
            px: 1,
            py: 0.5,
            borderRadius: 1,
            '&:hover': { bgcolor: theme.palette.action.hover },
          }}
        >
          <Typography
            component="span"
            sx={{
              fontFamily: 'monospace',
              fontSize: '0.72rem',
              color: 'text.disabled',
              minWidth: 64,
              flexShrink: 0,
            }}
          >
            {formatTs(ev.ts)}
          </Typography>
          <Typography component="span" sx={{ fontSize: '0.85rem', flexShrink: 0 }}>
            {iconFor(ev.kind)}
          </Typography>
          <Typography
            component="span"
            sx={{
              fontSize: '0.8rem',
              color: ev.kind === 'result' && ev.status === 'failure' ? 'error.main' : 'text.primary',
              wordBreak: 'break-word',
            }}
          >
            {summaryFor(ev)}
          </Typography>
        </Stack>
      ))}
      {(agentFinal || buildFinal) && (
        <Box sx={{ px: 1, py: 1, textAlign: 'center' }}>
          <Typography variant="caption" color="text.disabled">— end of feed —</Typography>
        </Box>
      )}
      </Stack>
      {!stickToBottom && (
        <Box
          onClick={() => {
            setStickToBottom(true);
            const el = scrollRef.current;
            if (el) el.scrollTop = el.scrollHeight;
          }}
          sx={{
            position: 'sticky',
            bottom: 4,
            mx: 'auto',
            width: 'fit-content',
            px: 1.5, py: 0.5,
            borderRadius: 4,
            bgcolor: 'primary.main',
            color: 'primary.contrastText',
            cursor: 'pointer',
            boxShadow: 2,
            fontSize: '0.72rem',
            fontWeight: 600,
            '&:hover': { bgcolor: 'primary.dark' },
          }}
        >
          ↓ Jump to latest
        </Box>
      )}
    </Box>
  );
}
