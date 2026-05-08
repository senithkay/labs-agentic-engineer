import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../services/api';
import type { TaskProgressEvent, TaskProgressResponse, TaskStatus } from '../services/api';

const POLL_MS = 3_000;

// useTaskAgentProgress streams the coding-agent's NDJSON output from
// the BFF's /progress/agent endpoint, accumulating lines locally and
// echoing the cursor on each poll. Polling stops once the response
// returns final:true OR the task moves past `in_progress`.
export function useTaskAgentProgress(
  orgId: string | undefined,
  projectId: string | undefined,
  taskId: string | undefined,
  taskStatus: TaskStatus | undefined,
) {
  const [cursor, setCursor] = useState(0);
  const [accumulated, setAccumulated] = useState<TaskProgressEvent[]>([]);
  const [phase, setPhase] = useState<string | undefined>(undefined);
  const [final, setFinal] = useState(false);
  // Reset accumulator when the task identity changes.
  const seenTask = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (seenTask.current !== taskId) {
      seenTask.current = taskId;
      setCursor(0);
      setAccumulated([]);
      setPhase(undefined);
      setFinal(false);
    }
  }, [taskId]);

  // Fetch the agent feed for any task that ever ran (past `pending`),
  // not just in-flight ones — the design intends `final:true` to freeze
  // a populated historical feed. We always do at least one fetch; only
  // polling is gated on the active phase.
  const enabled = !!orgId && !!projectId && !!taskId && taskStatus !== undefined && taskStatus !== 'pending' && taskStatus !== 'pending_deps';
  const isLivePhase = taskStatus === 'in_progress';

  const query = useQuery<TaskProgressResponse>({
    queryKey: ['taskAgentProgress', orgId, projectId, taskId, cursor],
    queryFn: () => api.getTaskAgentProgress(orgId!, projectId!, taskId!, cursor),
    enabled,
    refetchInterval: enabled && isLivePhase && !final ? POLL_MS : false,
  });

  useEffect(() => {
    const data = query.data;
    if (!data) return;
    if (data.lines && data.lines.length > 0) {
      setAccumulated((prev) => [...prev, ...data.lines]);
    }
    if (data.cursorMillis > cursor) {
      setCursor(data.cursorMillis);
    }
    if (data.phase) setPhase(data.phase);
    if (data.final) setFinal(true);
  }, [query.data]); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    lines: accumulated,
    phase,
    final,
    isLoading: query.isLoading,
    error: query.error as Error | null,
  };
}
