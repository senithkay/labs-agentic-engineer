import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../services/api';
import type { TaskProgressEvent, TaskProgressResponse, TaskStatus } from '../services/api';

const POLL_MS = 3_000;

// useTaskBuildProgress mirrors useTaskAgentProgress but reads from
// /progress/build, which surfaces synthetic build_step lines derived
// from WorkflowRun.Status.Tasks[]. Active during `building`; stops on
// final:true.
export function useTaskBuildProgress(
  orgId: string | undefined,
  projectId: string | undefined,
  taskId: string | undefined,
  taskStatus: TaskStatus | undefined,
) {
  const [cursor, setCursor] = useState(0);
  const [accumulated, setAccumulated] = useState<TaskProgressEvent[]>([]);
  const [final, setFinal] = useState(false);
  const seenTask = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (seenTask.current !== taskId) {
      seenTask.current = taskId;
      setCursor(0);
      setAccumulated([]);
      setFinal(false);
    }
  }, [taskId]);

  // Fetch once for any task past merged so the build_step feed shows
  // historical builds; only poll while the task is actively building.
  const enabled = !!orgId && !!projectId && !!taskId
    && (taskStatus === 'merged' || taskStatus === 'building' || taskStatus === 'deployed' || taskStatus === 'failed');
  const isLivePhase = taskStatus === 'building' || taskStatus === 'merged';

  const query = useQuery<TaskProgressResponse>({
    queryKey: ['taskBuildProgress', orgId, projectId, taskId, cursor],
    queryFn: () => api.getTaskBuildProgress(orgId!, projectId!, taskId!, cursor),
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
    if (data.final) setFinal(true);
  }, [query.data]); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    lines: accumulated,
    final,
    isLoading: query.isLoading,
    error: query.error as Error | null,
  };
}
