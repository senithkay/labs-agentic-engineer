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

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Box, Button, CircularProgress, PageContent, Typography } from '@wso2/oxygen-ui';
import { ProjectStatusPolyline, type Stage } from '@asdlc/project-status';
import { api, ApiError } from '../services/api';
import type { ComponentTask, ProjectSdlcPhase, ProjectStatus } from '../services/api';
import {
  projectArchitecturePath,
  projectRequirementsPath,
  projectTasksPath,
} from '../lib/paths';
import { buildProjectStages } from '../lib/buildProjectStages';
import { useAuth } from '../auth';
import ProjectPromptPage from './ProjectPromptPage';
import ProjectComponentsPage from './ProjectComponentsPage';

// Local-only phase added on top of the BFF's ProjectSdlcPhase to distinguish
// "BFF rejected our JWT" from "BFF says no repo" — the two look identical
// otherwise (status fetch returned nothing) and shouldn't share UI.
type Phase = ProjectSdlcPhase | 'auth-expired' | null;

const ACTIVE_TASK_STATUSES: ReadonlySet<string> = new Set([
  'pending',
  'on_hold',
  'in_progress',
  'ready_for_review',
  'merged',
  'building',
]);

export default function ProjectOverviewPage() {
  const { orgId, projectId } = useParams();
  const navigate = useNavigate();
  const routeOrgId = orgId ?? 'default';
  const [phase, setPhase] = useState<Phase>(null);
  const [status, setStatus] = useState<ProjectStatus | undefined>();
  const [tasks, setTasks] = useState<ComponentTask[]>([]);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchAll = useCallback(async () => {
    if (!projectId) return;
    let s: ProjectStatus | undefined;
    try {
      s = await api.getProjectStatus(routeOrgId, projectId);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        setPhase('auth-expired');
        return;
      }
      throw e;
    }
    const t = await api.listTasks(routeOrgId, projectId).catch(() => [] as ComponentTask[]);
    setStatus(s);
    setPhase(s ? s.phase : 'no-repo');
    setTasks(t);
  }, [projectId, routeOrgId]);

  useEffect(() => {
    fetchAll();
  }, [fetchAll]);

  // Poll while repo is cloning or any task is active in the pipeline.
  useEffect(() => {
    const cloning = phase === 'repo-cloning';
    const tasksActive = tasks.some((t) => ACTIVE_TASK_STATUSES.has(t.status));
    if (cloning || tasksActive) {
      const interval = cloning ? 3000 : 5000;
      pollRef.current = setInterval(fetchAll, interval);
    }
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [phase, tasks, fetchAll]);

  const stages = useMemo(() => buildProjectStages(status, tasks), [status, tasks]);

  const handleStageClick = useCallback(
    (stage: Stage) => {
      if (!projectId) return;
      switch (stage.id) {
        case 'requirements':
          navigate(projectRequirementsPath(routeOrgId, projectId));
          break;
        case 'architecture':
          navigate(projectArchitecturePath(routeOrgId, projectId));
          break;
        case 'tasks':
          navigate(projectTasksPath(routeOrgId, projectId));
          break;
        // 'deployment' has no dedicated page yet — no-op.
      }
    },
    [navigate, projectId, routeOrgId],
  );

  if (phase === null) {
    return (
      <PageContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', py: 12 }}>
          <CircularProgress size={36} sx={{ mb: 2 }} />
          <Typography variant="body2" color="text.secondary">
            Loading project...
          </Typography>
        </Box>
      </PageContent>
    );
  }

  if (phase === 'repo-cloning') {
    return (
      <PageContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', py: 12 }}>
          <CircularProgress size={48} sx={{ mb: 3 }} />
          <Typography variant="h6" color="text.secondary">
            Setting up repository...
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
            Cloning the Git repository. This may take a moment.
          </Typography>
        </Box>
      </PageContent>
    );
  }

  if (phase === 'auth-expired') {
    return <AuthExpiredState />;
  }

  if (phase === 'repo-error') {
    return <RepoErrorState repoUrl={status?.repoUrl} errorMessage={status?.repoErrorMessage} />;
  }

  if (phase === 'no-repo') {
    return (
      <PageContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', py: 12 }}>
          <Typography variant="h6" color="text.secondary" sx={{ mb: 1 }}>
            No Git repository associated
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Create a new project to use this platform.
          </Typography>
        </Box>
      </PageContent>
    );
  }

  if (phase === 'prompt') {
    return <ProjectPromptPage />;
  }

  return (
    <ProjectComponentsPage
      statusBanner={<ProjectStatusPolyline stages={stages} onStageClick={handleStageClick} />}
    />
  );
}

// Rendered when the BFF rejected our JWT with 401 — almost always means the
// access token expired while the SPA was open. Re-signing in is the only
// path back; the in-memory React state otherwise still looks "signed in"
// because asgardeo hasn't observed the failure.
function RepoErrorState({ repoUrl, errorMessage }: { repoUrl?: string; errorMessage?: string }) {
  const { orgId } = useParams();
  const navigate = useNavigate();
  const routeOrgId = orgId ?? 'default';
  const { summary, action, showGitHubSettings } = describeRepoError(errorMessage);

  return (
    <PageContent>
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', py: 12, gap: 1.5, maxWidth: 520 }}>
        <Typography variant="h6" color="text.secondary">
          We couldn&apos;t set up your repository
        </Typography>
        {repoUrl ? (
          <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center' }}>
            {repoUrl}
          </Typography>
        ) : null}
        <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center' }}>
          {summary}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ textAlign: 'center' }}>
          {action}
        </Typography>
        {showGitHubSettings ? (
          <Button
            variant="outlined"
            size="small"
            sx={{ mt: 1 }}
            onClick={() => navigate(`/organizations/${routeOrgId}/settings/github`)}
          >
            Open GitHub settings
          </Button>
        ) : null}
      </Box>
    </PageContent>
  );
}

function describeRepoError(errorMessage?: string): {
  summary: string;
  action: string;
  showGitHubSettings: boolean;
} {
  const raw = errorMessage?.trim() ?? '';

  if (/authentication failed|could not read Username|resolve credential|^token:/i.test(raw)) {
    return {
      summary: 'GitHub access for your organization could not be verified.',
      action: 'Check your GitHub connection, then delete this project and create it again.',
      showGitHubSettings: true,
    };
  }

  if (/clone failed/i.test(raw)) {
    return {
      summary: 'The repository could not be cloned from GitHub.',
      action: 'Confirm the repository exists and your GitHub connection has the required access, then try again.',
      showGitHubSettings: true,
    };
  }

  if (/permission denied/i.test(raw)) {
    return {
      summary: 'The platform could not prepare storage for this project.',
      action: 'Please try again shortly. If this keeps happening, contact your administrator.',
      showGitHubSettings: false,
    };
  }

  return {
    summary: 'An unexpected error occurred while provisioning the repository.',
    action: 'Delete this project and try creating it again. Contact your administrator if the problem continues.',
    showGitHubSettings: false,
  };
}

function AuthExpiredState() {
  const { signIn } = useAuth();
  return (
    <PageContent>
      <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', py: 12, gap: 1.5 }}>
        <Typography variant="h6" color="text.secondary">
          Your session has expired
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 360, textAlign: 'center' }}>
          Sign in again to continue. Your in-progress work was not lost.
        </Typography>
        <Button variant="contained" size="small" sx={{ mt: 1 }} onClick={() => signIn()}>
          Sign in again
        </Button>
      </Box>
    </PageContent>
  );
}
