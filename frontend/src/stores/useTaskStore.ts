import { create } from 'zustand';
import { deleteTask, fetchTasks, renameTask, type TaskQuery } from '@/services/taskApi';
import type { TaskSummary } from '@/types/task';
import { captureSessionGeneration, registerSessionReset, sessionStillCurrent } from './resetSessionState';
import { useWorkspaceBootstrapStore } from './useWorkspaceBootstrapStore';
import { useWorkspaceStore } from './useWorkspaceStore';
import { useRunChatStore } from './useRunChatStore';

interface TaskState {
  items: TaskSummary[];
  nextCursor: string;
  loading: boolean;
  error: string | null;
  query: Omit<TaskQuery, 'cursor'>;
  load: (query?: Omit<TaskQuery, 'cursor'>) => Promise<void>;
  loadMore: () => Promise<void>;
  rename: (id: string, title: string) => Promise<void>;
  remove: (id: string) => Promise<void>;
  replaceRecent: (items: TaskSummary[]) => void;
  clear: () => void;
}

const initial = {
  items: [] as TaskSummary[],
  nextCursor: '',
  loading: false,
  error: null as string | null,
  query: { limit: 20 } as Omit<TaskQuery, 'cursor'>,
};

let querySequence = 0;

function conversationIdFromTaskId(id: string): number | null {
  const conversationId = Number(id);
  return Number.isInteger(conversationId) && conversationId > 0
    ? conversationId
    : null;
}

export const useTaskStore = create<TaskState>((set, get) => ({
  ...initial,
  load: async (query = get().query) => {
    const generation = captureSessionGeneration();
    const sequence = ++querySequence;
    set({ loading: true, error: null, query });
    try {
      const page = await fetchTasks(query);
      if (!sessionStillCurrent(generation) || sequence !== querySequence) return;
      set({ items: page.items, nextCursor: page.nextCursor, loading: false });
    } catch {
      if (!sessionStillCurrent(generation) || sequence !== querySequence) return;
      set({ error: '任务加载失败', loading: false });
    }
  },
  loadMore: async () => {
    const { loading, nextCursor, query, items } = get();
    if (loading || !nextCursor) return;
    const generation = captureSessionGeneration();
    const sequence = querySequence;
    set({ loading: true, error: null });
    try {
      const page = await fetchTasks({ ...query, cursor: nextCursor });
      if (!sessionStillCurrent(generation) || sequence !== querySequence) return;
      const seen = new Set(items.map((item) => item.id));
      set({
        items: [...items, ...page.items.filter((item) => !seen.has(item.id))],
        nextCursor: page.nextCursor,
        loading: false,
      });
    } catch {
      if (!sessionStillCurrent(generation) || sequence !== querySequence) return;
      set({ error: '更多任务加载失败', loading: false });
    }
  },
  rename: async (id, title) => {
    const generation = captureSessionGeneration();
    const updated = await renameTask(id, title);
    if (!sessionStillCurrent(generation)) return;
    // A pre-mutation list response must not restore the old title after the
    // rename succeeds. Invalidating the sequence also releases its spinner.
    querySequence += 1;
    set((state) => ({
      items: state.items.map((item) => item.id === id ? updated : item),
      loading: false,
    }));
    const bootstrap = useWorkspaceBootstrapStore.getState();
    bootstrap.patchTask(id, updated);
    try {
      await bootstrap.load(true);
    } catch {
      bootstrap.invalidate();
    }
    await get().load(get().query);
  },
  remove: async (id) => {
    const generation = captureSessionGeneration();
    await deleteTask(id);
    if (!sessionStillCurrent(generation)) return;
    // Cancel any older Task Center page before reconciling every other cache.
    querySequence += 1;
    set((state) => ({
      items: state.items.filter((item) => item.id !== id),
      loading: false,
    }));

    const bootstrap = useWorkspaceBootstrapStore.getState();
    bootstrap.removeTask(id);
    // Deletion changes the server-computed Top-N membership; keep the exact
    // local removal now and let the next bootstrap load backfill the list.
    bootstrap.invalidate();

    const conversationId = conversationIdFromTaskId(id);
    if (conversationId != null) {
      useWorkspaceStore.getState().forgetConversation(conversationId);
      useRunChatStore.getState().removeConversation(conversationId);
    }
    await get().load(get().query);
  },
  replaceRecent: (items) => set({ items }),
  clear: () => {
    querySequence += 1;
    set({ ...initial });
  },
}));

registerSessionReset(() => useTaskStore.getState().clear());
