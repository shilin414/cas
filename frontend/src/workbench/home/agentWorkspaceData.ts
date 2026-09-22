import { fetchTasks } from '@/services/taskApi';
import { fetchSchedules } from '@/services/scheduleApi';
import { fetchWorkspaceArtifacts, type WorkspaceArtifact } from '@/services/workspaceArtifactApi';
import type { TaskSummary } from '@/types/task';
import type { Schedule } from '@/types/schedule';
export type Tab = 'tasks' | 'automations' | 'resources';
export type Row = { kind: 'tasks'; value: TaskSummary } | { kind: 'automations'; value: Schedule } | { kind: 'resources'; value: WorkspaceArtifact };
export async function fetchAgentCollection(applicationId: number, tab: Tab, query: string, cursor = ''): Promise<{rows: Row[]; next: string}> {
  if (tab === 'tasks') {
    const page = await fetchTasks({applicationId, q: query, cursor, limit:20});
    return {rows:page.items.map(value => ({kind:'tasks',value})), next:page.nextCursor};
  }
  if (tab === 'resources') {
    const page = await fetchWorkspaceArtifacts(applicationId, query, cursor);
    return {rows:page.items.map(value => ({kind:'resources',value})), next:page.next_cursor};
  }
  const page = await fetchSchedules('all', cursor ? Number(cursor) : undefined, 21, query, applicationId);
  return {rows:page.slice(0,20).map(value => ({kind:'automations',value})), next:page.length > 20 ? String(page[19].id) : ''};
}
