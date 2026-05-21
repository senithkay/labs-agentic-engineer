import { useEffect, useState } from 'react';
import { Box, Chip, CircularProgress, IconButton, Tooltip, Typography, useTheme } from '@wso2/oxygen-ui';
import type { DatabaseArtifact, DatabaseArtifactStatus } from '../../services/api/types';
import { restApi } from '../../services/api/rest';

const POLL_INTERVAL = 5000;

const STATUS_CONFIG: Record<DatabaseArtifactStatus, { label: string; color: string }> = {
  pending:      { label: 'PENDING',      color: 'text.disabled' },
  provisioning: { label: 'PROVISIONING', color: 'warning.main'  },
  healthy:      { label: 'HEALTHY',      color: 'success.main'  },
  faulty:       { label: 'FAULTY',       color: 'error.main'    },
};

const DB_TYPE_COLORS: Record<string, string> = {
  mysql:   '#00758f',
  mongodb: '#00ed64',
};

function DbIcon({ dbType }: { dbType?: string }) {
  const color = dbType ? (DB_TYPE_COLORS[dbType.toLowerCase()] ?? '#888') : '#888';
  const gradId = `db-grad-${dbType ?? 'default'}`;
  const label = dbType ? dbType.slice(0, 2).toUpperCase() : 'DB';
  return (
    <svg width="36" height="36" viewBox="0 0 40 40" xmlns="http://www.w3.org/2000/svg" style={{ flexShrink: 0 }}>
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.85" />
          <stop offset="100%" stopColor={color} stopOpacity="1" />
        </linearGradient>
      </defs>
      {/* Body */}
      <path d="M 7 9 A 13 3.5 0 0 0 33 9 L 33 33 A 13 3.5 0 0 1 7 33 Z" fill={`url(#${gradId})`} />
      {/* Top ellipse + highlight */}
      <ellipse cx="20" cy="9" rx="13" ry="3.5" fill={color} />
      <ellipse cx="20" cy="8.5" rx="11.5" ry="2.5" fill="#fff" opacity="0.18" />
      {/* Data disc rings */}
      <path d="M 7 17 A 13 3.5 0 0 0 33 17" fill="none" stroke="#fff" strokeWidth="0.7" opacity="0.5" />
      <path d="M 7 25 A 13 3.5 0 0 0 33 25" fill="none" stroke="#fff" strokeWidth="0.7" opacity="0.5" />
      {/* Ground shadow */}
      <ellipse cx="20" cy="36" rx="11" ry="1.2" fill="#000" opacity="0.08" />
      {/* Label */}
      <text x="20" y="23" textAnchor="middle" fontSize="6.5" fontWeight="700"
            fill="#fff" opacity="0.95" fontFamily="Inter, sans-serif" letterSpacing="0.5">
        {label}
      </text>
    </svg>
  );
}

function StatusBadge({ status }: { status: DatabaseArtifactStatus }) {
  const cfg = STATUS_CONFIG[status];
  return (
    <Typography
      variant="caption"
      sx={{
        fontWeight: 700,
        color: cfg.color,
        fontSize: '0.6rem',
        letterSpacing: '0.06em',
        flexShrink: 0,
      }}
    >
      {cfg.label}
    </Typography>
  );
}


function CopyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function EyeIcon({ open }: { open: boolean }) {
  if (open) {
    return (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
        <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
        <line x1="1" y1="1" x2="23" y2="23" />
      </svg>
    );
  }
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function CredentialRow({ label, value, secret }: { label: string; value: string; secret?: boolean }) {
  const [visible, setVisible] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  const display = secret && !visible ? '••••••••••••••' : value;

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, py: 0.5 }}>
      <Typography
        variant="caption"
        sx={{ color: 'text.disabled', fontSize: '0.6rem', fontWeight: 700, letterSpacing: '0.05em', width: 36, flexShrink: 0 }}
      >
        {label}
      </Typography>
      <Typography
        variant="caption"
        sx={{
          flex: 1,
          fontSize: '0.7rem',
          fontFamily: 'monospace',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          color: 'text.primary',
        }}
      >
        {display}
      </Typography>
      {secret && (
        <Tooltip title={visible ? 'Hide' : 'Show'}>
          <IconButton size="small" onClick={() => setVisible(v => !v)} sx={{ p: 0.25, color: 'text.secondary' }}>
            <EyeIcon open={visible} />
          </IconButton>
        </Tooltip>
      )}
      <Tooltip title={copied ? 'Copied!' : 'Copy'}>
        <IconButton size="small" onClick={handleCopy} sx={{ p: 0.25, color: 'text.secondary' }}>
          <CopyIcon />
        </IconButton>
      </Tooltip>
    </Box>
  );
}

function SpinningBorder() {
  const theme = useTheme();
  const color = theme.palette.primary.main;
  return (
    <svg
      aria-hidden="true"
      style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', overflow: 'visible' }}
    >
      <style>{`@keyframes db-border-travel { to { stroke-dashoffset: -1; } }`}</style>
      <rect
        x="1" y="1" width="calc(100% - 2px)" height="calc(100% - 2px)"
        rx="9" ry="9"
        fill="none"
        stroke={color}
        strokeWidth="2"
        pathLength="1"
        strokeDasharray="0.55 0.45"
        style={{ animation: 'db-border-travel 1.5s linear infinite' }}
      />
    </svg>
  );
}

function DatabaseCard({ db }: { db: DatabaseArtifact }) {
  const [expanded, setExpanded] = useState(false);
  const hasCredentials = db.host || db.port || db.username || db.password;
  const isProvisioning = db.status === 'provisioning';

  return (
    <Box
      sx={{
        position: 'relative',
        borderRadius: 1.25,
        border: '1px solid',
        borderColor: isProvisioning ? 'transparent' : (expanded ? 'primary.light' : 'divider'),
        bgcolor: 'background.paper',
        mb: 1,
        overflow: 'hidden',
        cursor: hasCredentials ? 'pointer' : 'default',
        transition: 'border-color 0.15s',
        '&:hover': hasCredentials && !isProvisioning ? { borderColor: 'primary.light' } : {},
      }}
      onClick={() => { if (hasCredentials) setExpanded(e => !e); }}
    >
      {isProvisioning && <SpinningBorder />}

      {/* Card header */}
      <Box sx={{ display: 'flex', gap: 1.25, p: 1.25 }}>
        <DbIcon dbType={db.dbType} />
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 0.5 }}>
            <Typography
              variant="body2"
              sx={{ fontWeight: 600, fontSize: '0.8rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            >
              {db.dbName || db.requestedName || (db.components?.[0] ?? 'database')}
            </Typography>
            <StatusBadge status={db.status} />
          </Box>
          <Typography variant="caption" sx={{ color: 'text.secondary', fontSize: '0.7rem', display: 'block' }}>
            {db.dbType ? db.dbType.charAt(0).toUpperCase() + db.dbType.slice(1) : 'Database'}
            {db.components?.length ? ` · ${db.components.join(', ')}` : ''}
          </Typography>
        </Box>
      </Box>

      {/* Credentials panel */}
      {expanded && hasCredentials && (
        <Box
          sx={{ px: 1.25, pb: 1.25, pt: 0.25 }}
          onClick={e => e.stopPropagation()}
        >
          <Typography
            variant="caption"
            sx={{ color: 'text.disabled', fontSize: '0.6rem', fontWeight: 700, letterSpacing: '0.08em', display: 'block', mb: 0.5 }}
          >
            CREDENTIALS
          </Typography>
          <Box sx={{ borderTop: '1px solid', borderColor: 'divider', pt: 0.5 }}>
            {db.host     && <CredentialRow label="HOST" value={db.host} />}
            {db.port     && <CredentialRow label="PORT" value={String(db.port)} />}
            {db.username && <CredentialRow label="USER" value={db.username} />}
            {db.password && <CredentialRow label="PASS" value={db.password} secret />}
          </Box>
        </Box>
      )}
    </Box>
  );
}

interface DatabaseArtifactsPanelProps {
  orgId: string;
  projectId: string;
  onHasArtifacts?: (has: boolean) => void;
}

export function DatabaseArtifactsPanel({ orgId, projectId, onHasArtifacts }: DatabaseArtifactsPanelProps) {
  const [databases, setDatabases] = useState<DatabaseArtifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [apiError, setApiError] = useState<string | null>(null);

  useEffect(() => {
    if (!orgId || !projectId) return;

    let cancelled = false;

    const load = async () => {
      try {
        const data = await restApi.listDatabaseArtifacts(orgId, projectId);
        if (!cancelled) {
          setDatabases(data);
          setApiError(null);
        }
      } catch (err: any) {
        // eslint-disable-next-line no-console
        console.error('[DatabaseArtifactsPanel] failed to load:', err?.message ?? err);
        if (!cancelled) setApiError(err?.message ?? 'Failed to load');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };

    load();
    const interval = setInterval(load, POLL_INTERVAL);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [orgId, projectId]);

  const healthy      = databases.filter(d => d.status === 'healthy').length;
  const provisioning = databases.filter(d => d.status === 'provisioning').length;
  const faulty       = databases.filter(d => d.status === 'faulty').length;
  const pending      = databases.filter(d => d.status === 'pending').length;

  const total = databases.length;
  const visible = !loading && !apiError && total > 0;

  useEffect(() => {
    onHasArtifacts?.(visible);
  }, [visible, onHasArtifacts]);

  return (
    <Box
      sx={{
        position: 'sticky',
        top: 64,
        height: 'calc(100vh - 64px)',
        display: 'flex',
        flexDirection: 'column',
        borderLeft: '1px solid',
        borderColor: 'divider',
        bgcolor: 'transparent',
        pl: 2,
        pr: 1.5,
        pt: 2,
        overflowX: 'hidden',
        overflowY: visible ? 'auto' : 'hidden',
        opacity: visible ? 1 : 0,
        transform: visible ? 'translateX(0)' : 'translateX(16px)',
        transition: 'opacity 0.3s ease, transform 0.35s ease',
        pointerEvents: visible ? 'auto' : 'none',
      }}
    >
      {/* Header */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.5 }}>
        <Typography variant="body2" sx={{ fontWeight: 700 }}>
          Artifacts
        </Typography>
        {total > 0 && (
          <Chip
            label={total}
            size="small"
            sx={{ height: 18, fontSize: '0.65rem', fontWeight: 700, px: 0.25 }}
          />
        )}
        {loading && <CircularProgress size={12} sx={{ ml: 'auto' }} />}
      </Box>

      {/* Status summary */}
      {total > 0 && (
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, mb: 1.5 }}>
          {healthy > 0 && (
            <Typography variant="caption" sx={{ color: 'success.main', fontWeight: 600, fontSize: '0.7rem' }}>
              {healthy} healthy
            </Typography>
          )}
          {provisioning > 0 && (
            <Typography variant="caption" sx={{ color: 'warning.main', fontWeight: 600, fontSize: '0.7rem' }}>
              {provisioning} provisioning
            </Typography>
          )}
          {faulty > 0 && (
            <Typography variant="caption" sx={{ color: 'error.main', fontWeight: 600, fontSize: '0.7rem' }}>
              {faulty} faulty
            </Typography>
          )}
          {pending > 0 && (
            <Typography variant="caption" sx={{ color: 'text.disabled', fontWeight: 600, fontSize: '0.7rem' }}>
              {pending} pending
            </Typography>
          )}
        </Box>
      )}

      {/* Database cards */}
      <Box sx={{ flex: 1 }}>
        {loading && total === 0 ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', pt: 4 }}>
            <CircularProgress size={20} />
          </Box>
        ) : apiError ? (
          <Typography variant="caption" sx={{ color: 'error.main', display: 'block', mt: 2, wordBreak: 'break-word' }}>
            {apiError}
          </Typography>
        ) : (
          databases.map(db => <DatabaseCard key={db.id} db={db} />)
        )}
      </Box>
    </Box>
  );
}
