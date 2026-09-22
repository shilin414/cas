import { api } from './api';
export interface WorkspaceArtifact {
  id: string;
  name: string;
  normalized_type: string;
  created_at: string;
  conversation_id: string;
  task_title: string;
}
export function fetchWorkspaceArtifacts(applicationId: number, q: string, cursor?: string) {
  return api.get<{ items: WorkspaceArtifact[]; next_cursor: string }>('/v2/workspace/artifacts', {
    application_id: applicationId, q: q.trim() || undefined, cursor: cursor || undefined, limit: 20,
  });
}
