import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TaskPage, TaskSummary } from '@/types/task';

const mocks = vi.hoisted(() => ({
  fetchTasks: vi.fn(),
  renameTask: vi.fn(),
  deleteTask: vi.fn(),
  fetchWorkspaceBootstrap: vi.fn(),
}));

vi.mock('@/services/taskApi', () => ({
  fetchTasks: mocks.fetchTasks,
  renameTask: mocks.renameTask,
  deleteTask: mocks.deleteTask,
}));

vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/runApi')>()),
  fetchWorkspaceBootstrap: mocks.fetchWorkspaceBootstrap,
}));

import { useTaskStore } from '../useTaskStore';
import { useWorkspaceBootstrapStore } from '../useWorkspaceBootstrapStore';
import { useWorkspaceStore, workspaceStateOf } from '../useWorkspaceStore';
import { useRunChatStore } from '../useRunChatStore';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function task(id: string): TaskSummary {
  return {
    id,
    title: `Task ${id}`,
    applicationId: null,
    createdAt: '2026-09-19T00:00:00Z',
    updatedAt: '2026-09-19T00:00:00Z',
    executionState: 'idle',
  };
}

describe('useTaskStore request sequencing', () => {
  beforeEach(() => {
    mocks.fetchTasks.mockReset();
    mocks.renameTask.mockReset();
    mocks.deleteTask.mockReset();
    mocks.fetchWorkspaceBootstrap.mockReset().mockResolvedValue({
      default_application: null, favorites: [], frequent: [], recent: [], recommended: [],
      recent_fixed_apps: [], recent_capabilities: [], recent_tasks: [],
      agent_categories: [], app_categories: [],
    });
    useTaskStore.getState().clear();
    useWorkspaceBootstrapStore.getState().clear();
    useWorkspaceStore.getState().clearAll();
    useRunChatStore.getState().clearAll();
  });

  it('keeps the newer search when load responses finish out of order', async () => {
    const loadAResponse = deferred<TaskPage>();
    const loadBResponse = deferred<TaskPage>();
    mocks.fetchTasks
      .mockReturnValueOnce(loadAResponse.promise)
      .mockReturnValueOnce(loadBResponse.promise);

    const loadA = useTaskStore.getState().load({ limit: 20, q: 'A' });
    const loadB = useTaskStore.getState().load({ limit: 20, q: 'B' });

    loadBResponse.resolve({ items: [task('B')], nextCursor: 'cursor-b' });
    await loadB;
    loadAResponse.resolve({ items: [task('A')], nextCursor: 'cursor-a' });
    await loadA;

    expect(useTaskStore.getState()).toMatchObject({
      items: [task('B')],
      nextCursor: 'cursor-b',
      loading: false,
      error: null,
      query: { limit: 20, q: 'B' },
    });
  });

  it('does not append an old loadMore page after a newer search replaces the result set', async () => {
    useTaskStore.setState({
      items: [task('A-1')],
      nextCursor: 'cursor-a',
      loading: false,
      error: null,
      query: { limit: 20, q: 'A' },
    });
    const loadMoreAResponse = deferred<TaskPage>();
    const loadBResponse = deferred<TaskPage>();
    mocks.fetchTasks
      .mockReturnValueOnce(loadMoreAResponse.promise)
      .mockReturnValueOnce(loadBResponse.promise);

    const loadMoreA = useTaskStore.getState().loadMore();
    const loadB = useTaskStore.getState().load({ limit: 20, q: 'B' });

    expect(mocks.fetchTasks).toHaveBeenNthCalledWith(1, {
      limit: 20,
      q: 'A',
      cursor: 'cursor-a',
    });
    loadBResponse.resolve({ items: [task('B-1')], nextCursor: 'cursor-b' });
    await loadB;
    loadMoreAResponse.resolve({ items: [task('A-2')], nextCursor: 'cursor-a-2' });
    await loadMoreA;

    expect(useTaskStore.getState()).toMatchObject({
      items: [task('B-1')],
      nextCursor: 'cursor-b',
      loading: false,
      error: null,
      query: { limit: 20, q: 'B' },
    });
  });

  it('reconciles a successful rename into Task Center and recent tasks', async () => {
    const original = task('7');
    const updated = { ...original, title: 'Renamed task', updatedAt: '2026-09-19T01:00:00Z' };
    useTaskStore.setState({ items: [original] });
    useWorkspaceBootstrapStore.setState({ recentTasks: [original] });
    mocks.renameTask.mockResolvedValue(updated);
    mocks.fetchTasks.mockResolvedValue({ items: [updated], nextCursor: '' });

    await useTaskStore.getState().rename('7', 'Renamed task');

    expect(useTaskStore.getState().items).toEqual([updated]);
    expect(useWorkspaceBootstrapStore.getState().recentTasks).toEqual([updated]);
  });

  it('reconciles a successful delete across every task-backed client cache', async () => {
    const deleted = { ...task('7'), applicationId: 3 };
    const kept = { ...task('8'), applicationId: 4 };
    useTaskStore.setState({ items: [deleted, kept] });
    useWorkspaceBootstrapStore.setState({ recentTasks: [deleted, kept] });
    useWorkspaceStore.getState().rememberConversation(3, 7);
    useWorkspaceStore.getState().setDraft(3, 'preserve me');
    useWorkspaceStore.getState().rememberConversation(4, 8);
    useRunChatStore.setState({
      conversations: {
        7: { id: 7, title: deleted.title, messages: [], activeRunId: null },
        8: { id: 8, title: kept.title, messages: [], activeRunId: null },
      },
      activeConversationId: 7,
      lastConversationId: 7,
    });
    mocks.deleteTask.mockResolvedValue(undefined);
    mocks.fetchTasks.mockResolvedValue({ items: [kept], nextCursor: '' });

    await useTaskStore.getState().remove('7');

    expect(useTaskStore.getState().items).toEqual([kept]);
    expect(useWorkspaceBootstrapStore.getState().recentTasks).toEqual([kept]);
    expect(workspaceStateOf(useWorkspaceStore.getState().workspaces, 3)).toMatchObject({
      conversationId: null,
      draft: 'preserve me',
    });
    expect(workspaceStateOf(useWorkspaceStore.getState().workspaces, 4).conversationId).toBe(8);
    expect(useRunChatStore.getState().conversations[7]).toBeUndefined();
    expect(useRunChatStore.getState().conversations[8]).toBeDefined();
    expect(useRunChatStore.getState().activeConversationId).toBeNull();
    expect(useRunChatStore.getState().lastConversationId).toBeNull();
  });

  it('reloads the latest query when a pending delete finishes', async () => {
    const deletion = deferred<void>();
    const staleSearch = deferred<TaskPage>();
    const refreshedSearch = deferred<TaskPage>();
    mocks.deleteTask.mockReturnValueOnce(deletion.promise);
    mocks.fetchTasks
      .mockReturnValueOnce(staleSearch.promise)
      .mockReturnValueOnce(refreshedSearch.promise);
    useTaskStore.setState({
      items: [task('delete-me')],
      query: { limit: 20, q: 'A' },
      loading: false,
    });

    const removing = useTaskStore.getState().remove('delete-me');
    const searching = useTaskStore.getState().load({ limit: 20, q: 'B' });
    deletion.resolve();
    refreshedSearch.resolve({ items: [task('B-fresh')], nextCursor: '' });
    await removing;
    staleSearch.resolve({ items: [task('B-stale')], nextCursor: '' });
    await searching;

    expect(useTaskStore.getState()).toMatchObject({
      items: [task('B-fresh')],
      query: { limit: 20, q: 'B' },
      loading: false,
    });
  });
});
