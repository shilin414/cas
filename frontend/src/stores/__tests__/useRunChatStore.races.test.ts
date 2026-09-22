import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChatMessage } from '../useRunChatStore';
import type { RunRecord } from '@/services/runApi';
import { applyEvent, closeRunStream, finalizeRun, useRunChatStore } from '../useRunChatStore';

const mocks = vi.hoisted(() => ({ get: vi.fn(), createRun: vi.fn(), openRunStream: vi.fn(), getRun: vi.fn(), fetchRunArtifacts: vi.fn() }));
vi.mock('@/services/axios', () => ({ default: { get: mocks.get } }));
vi.mock('@/services/runApi', () => ({
  createRun: mocks.createRun, newClientRequestId: () => 'request-id',
  getRun: mocks.getRun, fetchRunArtifacts: mocks.fetchRunArtifacts,
}));
vi.mock('@/services/runStream', () => ({ openRunStream: mocks.openRunStream }));
vi.mock('@/stores/useWorkspaceBootstrapStore', () => ({
  useWorkspaceBootstrapStore: { getState: () => ({ invalidate: vi.fn() }) },
}));
vi.mock('../useConversationStore', () => ({
  useConversationStore: { getState: () => ({ fetchConversations: vi.fn() }) },
}));

// Timeline assertions inspect provisional text separately from final answers.
function timelineText(message: ChatMessage): string {
  if (message.status === 'streaming') {
    expect(message.content).toBe('');
    return message.processText ?? '';
  }
  return message.content;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}
const run: RunRecord = {
  id: 'run-a', application: 1, conversation: 11, provider: 'feishu_aily',
  runtime_type: 'agent', status: 'queued', created_at: '',
};
const previousMessages = [
  { id: 101, role: 'user', content: 'previous question', created_at: '', metadata: { run_id: 'run-previous' } },
  { id: 102, role: 'assistant', content: 'previous answer', created_at: '', metadata: { run_id: 'run-previous' } },
];
const send = () => useRunChatStore.getState().sendMessage({
  applicationId: 1, conversationId: 11, content: 'hello', clientRequestId: 'same-intent',
});
const delta = { run_id: run.id, sequence: 0, event_type: 'content.delta', payload: { text: 'live answer' } };
beforeEach(() => {
  useRunChatStore.getState().clearAll();
  vi.resetAllMocks();
  mocks.createRun.mockResolvedValue(run);
  mocks.getRun.mockResolvedValue({ ...run, status: 'succeeded', output: { text: 'final answer' } });
  mocks.fetchRunArtifacts.mockResolvedValue([]);
  mocks.openRunStream.mockImplementation(() => ({ close: vi.fn() }));
});
afterEach(() => useRunChatStore.getState().clearAll());

describe('chat history request ownership', () => {
  it.each(['history-first', 'completion-first'] as const)(
    'preserves initial prior history and the new turn (%s)', async (order) => {
      const history = deferred<{ messages: typeof previousMessages }>();
      mocks.get.mockReturnValueOnce(history.promise);
      const loading = useRunChatStore.getState().loadConversation(11);
      expect(useRunChatStore.getState().conversations[11]).toBeUndefined();
      expect(useRunChatStore.getState().isLoading).toBe(true);
      // Sending remains functional while the initial history fetch is pending.
      expect(await send()).toBe(11);
      expect(mocks.createRun).toHaveBeenCalledTimes(1);
      expect(useRunChatStore.getState().isLoading).toBe(true);
      mocks.openRunStream.mock.calls[0][1].onEvent(delta);
      if (order === 'completion-first') await finalizeRun(run.id);
      history.resolve({ messages: previousMessages });
      await loading;
      const hydrated = useRunChatStore.getState().conversations[11];
      expect(hydrated.messages.map(timelineText)).toEqual([
        'previous question', 'previous answer', 'hello',
        order === 'completion-first' ? 'final answer' : 'live answer',
      ]);
      expect(hydrated.activeRunId).toBe(order === 'completion-first' ? null : run.id);
      if (order === 'history-first') await finalizeRun(run.id);
      const state = useRunChatStore.getState();
      expect(state.conversations[11].messages.map(timelineText)).toEqual([
        'previous question', 'previous answer', 'hello', 'final answer',
      ]);
      expect(state.conversations[11].messages[3].status).toBe('done');
      expect(state.conversations[11].activeRunId).toBeNull();
      expect(state.isLoading).toBe(false);
    },
  );

  it('deduplicates persisted copies of the live turn without replacing its streaming bubble', async () => {
    const history = deferred<{ messages: typeof previousMessages }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    await send();
    mocks.openRunStream.mock.calls[0][1].onEvent(delta);
    const liveMessages = useRunChatStore.getState().conversations[11].messages;
    history.resolve({ messages: [
      ...previousMessages,
      { id: 103, role: 'user', content: 'hello', created_at: '', metadata: { run_id: run.id } },
      { id: 104, role: 'assistant', content: 'stale partial', created_at: '', metadata: { run_id: run.id } },
    ] });
    await loading;
    const conv = useRunChatStore.getState().conversations[11];
    expect(conv.messages.map(timelineText)).toEqual(['previous question', 'previous answer', 'hello', 'live answer']);
    expect(conv.messages[2]).toBe(liveMessages[0]);
    expect(conv.messages[3]).toBe(liveMessages[1]);
    expect(conv.messages[3].status).toBe('streaming');
    expect(conv.activeRunId).toBe(run.id);
    expect(mocks.openRunStream).toHaveBeenCalledTimes(1);
  });

  it('retains multiple legitimate sends while the initial history fetch is pending', async () => {
    const history = deferred<{ messages: typeof previousMessages }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    await send();
    await finalizeRun(run.id);
    mocks.createRun.mockResolvedValueOnce({ ...run, id: 'run-b' });
    await useRunChatStore.getState().sendMessage({
      applicationId: 1, conversationId: 11, content: 'second turn', clientRequestId: 'second-intent',
    });
    mocks.openRunStream.mock.calls[1][1].onEvent({ ...delta, run_id: 'run-b', payload: { text: 'second answer' } });
    history.resolve({ messages: previousMessages });
    await loading;
    const conv = useRunChatStore.getState().conversations[11];
    expect(conv.messages.map(timelineText)).toEqual([
      'previous question', 'previous answer', 'hello', 'final answer', 'second turn', 'second answer',
    ]);
    expect(conv.activeRunId).toBe('run-b');
    expect(conv.messages[3].status).toBe('done');
    expect(conv.messages[5].status).toBe('streaming');
  });

  it.each([false, true])(
    'keeps chronology and one message per role/run when history precedes B acknowledgement (assistant=%s)',
    async (hasPersistedAssistant) => {
      const history = deferred<{ messages: typeof previousMessages }>();
      const acknowledgement = deferred<RunRecord>();
      mocks.get.mockReturnValueOnce(history.promise);
      const loading = useRunChatStore.getState().loadConversation(11);
      await send();
      await finalizeRun(run.id);
      const completedA = useRunChatStore.getState().conversations[11].messages[1];

      // B has committed server-side, but its createRun response is still held.
      mocks.createRun.mockReturnValueOnce(acknowledgement.promise);
      const attachments = [{ id: 'attachment-b', name: 'input.png' }];
      const sendingB = useRunChatStore.getState().sendMessage({
        applicationId: 1, conversationId: 11, content: 'second turn', clientRequestId: 'second-intent', attachments,
      });
      history.resolve({ messages: [
        ...previousMessages,
        { id: 103, role: 'user', content: 'hello', created_at: '', metadata: { run_id: run.id } },
        { id: 104, role: 'assistant', content: 'stale A snapshot', created_at: '', metadata: { run_id: run.id } },
        { id: 105, role: 'user', content: 'second turn', created_at: '', metadata: { run_id: 'run-b' } },
        ...(hasPersistedAssistant
          ? [{ id: 106, role: 'assistant', content: 'second final', created_at: '', metadata: { run_id: 'run-b' } }]
          : []),
      ] });
      await loading;
      const contents = () => useRunChatStore.getState().conversations[11].messages.map(timelineText);
      const prefix = ['previous question', 'previous answer', 'hello', 'final answer', 'second turn'];
      // Soft assertions let the same reproduction exercise the later ack too.
      expect.soft(contents()).toEqual([...prefix, ...(hasPersistedAssistant ? ['second final'] : [])]);
      expect.soft(useRunChatStore.getState().conversations[11].messages[3]).toBe(completedA);

      // This response can describe queued B even if the newer history already
      // contains its terminal assistant. Do not downgrade or erase that bubble.
      acknowledgement.resolve({ ...run, id: 'run-b' });
      await sendingB;
      expect.soft(contents()).toEqual([...prefix, hasPersistedAssistant ? 'second final' : '']);
      const messages = useRunChatStore.getState().conversations[11].messages;
      expect.soft(messages.map((message) => [message.role, message.runId])).toEqual([
        ['user', 'run-previous'], ['assistant', 'run-previous'],
        ['user', run.id], ['assistant', run.id],
        ['user', 'run-b'], ['assistant', 'run-b'],
      ]);
      expect.soft(messages.find((message) => message.role === 'assistant' && message.runId === 'run-b')).toMatchObject({
        id: 'run-run-b', status: hasPersistedAssistant ? 'done' : 'streaming',
      });
      expect.soft(messages.find((message) => message.role === 'user' && message.runId === 'run-b')?.attachments).toEqual(attachments);
      expect.soft(mocks.openRunStream).toHaveBeenCalledTimes(2);

      const text = hasPersistedAssistant ? 'second final' : 'second live';
      mocks.openRunStream.mock.calls[1][1].onEvent({
        run_id: 'run-b', sequence: 1, event_type: 'content.chunk', payload: { text, offset: text.length },
      });
      expect.soft(contents()).toEqual([...prefix, text]);
      if (!hasPersistedAssistant) {
        expect.soft(useRunChatStore.getState().conversations[11].activeRunId).toBe('run-b');
      }
      mocks.getRun.mockResolvedValueOnce({ ...run, id: 'run-b', status: 'succeeded', output: { text: 'second canonical' } });
      await finalizeRun('run-b');
      expect.soft(contents()).toEqual([...prefix, 'second canonical']);
      expect.soft(useRunChatStore.getState().conversations[11].activeRunId).toBeNull();
    },
  );

  it('keeps an unpersisted failed A assistant beside A before a later persisted B user', async () => {
    const history = deferred<{ messages: typeof previousMessages }>();
    const acknowledgement = deferred<RunRecord>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    await send();
    mocks.getRun.mockResolvedValueOnce({ ...run, status: 'failed', output: null, error_message: 'provider failure' });
    await finalizeRun(run.id);
    const failedA = useRunChatStore.getState().conversations[11].messages[1];
    expect(failedA).toMatchObject({ status: 'failed', error: 'provider failure' });

    mocks.createRun.mockReturnValueOnce(acknowledgement.promise);
    const sendingB = useRunChatStore.getState().sendMessage({
      applicationId: 1, conversationId: 11, content: 'second turn', clientRequestId: 'second-intent',
    });
    history.resolve({ messages: [
      ...previousMessages,
      { id: 103, role: 'user', content: 'hello', created_at: '', metadata: { run_id: run.id } },
      { id: 105, role: 'user', content: 'second turn', created_at: '', metadata: { run_id: 'run-b' } },
    ] });
    await loading;
    const contents = () => useRunChatStore.getState().conversations[11].messages.map(timelineText);
    expect(contents()).toEqual(['previous question', 'previous answer', 'hello', '', 'second turn']);
    expect(useRunChatStore.getState().conversations[11].messages[3]).toBe(failedA);

    acknowledgement.resolve({ ...run, id: 'run-b' });
    await sendingB;
    mocks.openRunStream.mock.calls[1][1].onEvent({ ...delta, run_id: 'run-b' });
    expect(contents()).toEqual(['previous question', 'previous answer', 'hello', '', 'second turn', 'live answer']);
    expect(useRunChatStore.getState().conversations[11].messages[3]).toBe(failedA);
    expect(useRunChatStore.getState().conversations[11].activeRunId).toBe('run-b');
  });

  it('merges only the newest initial response when a send races overlapping loads', async () => {
    const first = deferred<{ title: string; messages: typeof previousMessages }>();
    const second = deferred<{ title: string; messages: typeof previousMessages }>();
    mocks.get.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const older = useRunChatStore.getState().loadConversation(11);
    const newer = useRunChatStore.getState().loadConversation(11);
    await send();
    mocks.openRunStream.mock.calls[0][1].onEvent(delta);
    second.resolve({ title: 'current history', messages: previousMessages });
    await newer;
    first.resolve({ title: 'stale history', messages: [] });
    await older;
    const conv = useRunChatStore.getState().conversations[11];
    expect(conv.title).toBe('current history');
    expect(conv.messages.map(timelineText)).toEqual(['previous question', 'previous answer', 'hello', 'live answer']);
    expect(conv.activeRunId).toBe(run.id);
  });

  it('refreshes cached history authoritatively while retaining only the concurrently sent turn', async () => {
    mocks.get.mockResolvedValueOnce({ messages: previousMessages });
    await useRunChatStore.getState().loadConversation(11);
    const history = deferred<{ messages: typeof previousMessages }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    await send();
    mocks.openRunStream.mock.calls[0][1].onEvent(delta);
    history.resolve({ messages: [previousMessages[0], { ...previousMessages[1], content: 'updated prior answer' }] });
    await loading;
    expect(useRunChatStore.getState().conversations[11].messages.map(timelineText)).toEqual([
      'previous question', 'updated prior answer', 'hello', 'live answer',
    ]);
  });

  it('replaces optimistic IDs on a later authoritative reload after the run completes', async () => {
    const history = deferred<{ messages: typeof previousMessages }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    await send();
    history.resolve({ messages: previousMessages });
    await loading;
    await finalizeRun(run.id);
    mocks.get.mockResolvedValueOnce({ messages: [
      ...previousMessages,
      { id: 103, role: 'user', content: 'hello', created_at: '', metadata: { run_id: run.id } },
      { id: 104, role: 'assistant', content: 'final answer', created_at: '', metadata: { run_id: run.id } },
    ] });
    await useRunChatStore.getState().loadConversation(11);
    expect(useRunChatStore.getState().conversations[11].messages.map((m) => m.id)).toEqual(['101', '102', '103', '104']);
  });

  it('keeps the latest same-conversation response when requests finish in reverse order', async () => {
    const first = deferred<{ title: string; messages: unknown[] }>();
    const second = deferred<{ title: string; messages: unknown[] }>();
    mocks.get.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const older = useRunChatStore.getState().loadConversation(11);
    const newer = useRunChatStore.getState().loadConversation(11);
    second.resolve({ title: 'new title', messages: [] });
    await newer;
    first.resolve({ title: 'old title', messages: [] });
    await older;
    expect(useRunChatStore.getState().conversations[11].title).toBe('new title');
  });
  it('keeps the spinner owned by the newer request when an older same-conversation request fails', async () => {
    const first = deferred<unknown>();
    const second = deferred<{ messages: unknown[] }>();
    mocks.get.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const older = useRunChatStore.getState().loadConversation(11);
    const newer = useRunChatStore.getState().loadConversation(11);
    first.reject(new Error('old request failed'));
    await older;
    expect(useRunChatStore.getState()).toMatchObject({ isLoading: true, error: null });
    second.resolve({ messages: [] });
    await newer;
  });
});
describe('history invalidation boundaries', () => {
  it.each(['remove', 'clear'] as const)('does not resurrect history after %s', async (action) => {
    const history = deferred<{ messages: unknown[] }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(11);
    if (action === 'remove') useRunChatStore.getState().removeConversation(11);
    else useRunChatStore.getState().clearAll();
    history.resolve({ messages: [] });
    await loading;
    expect(useRunChatStore.getState().conversations[11]).toBeUndefined();
    expect(useRunChatStore.getState().isLoading).toBe(false);
  });

  it('does not clear another conversation loading indicator when a send starts', async () => {
    const history = deferred<{ messages: unknown[] }>();
    mocks.get.mockReturnValueOnce(history.promise);
    const loading = useRunChatStore.getState().loadConversation(22);
    await send();
    expect(useRunChatStore.getState().isLoading).toBe(true);
    history.resolve({ messages: [] });
    await loading;
    expect(useRunChatStore.getState().isLoading).toBe(false);
  });

  it('reports malformed history without leaving the loading indicator stuck', async () => {
    mocks.get.mockResolvedValueOnce({ messages: [null] });
    await useRunChatStore.getState().loadConversation(11);
    expect(useRunChatStore.getState()).toMatchObject({ isLoading: false, error: '获取对话详情失败' });
  });
});

describe('run stream ownership', () => {
  it('passes explicit empty terminal process through asynchronous GET reconciliation', async () => {
    await send();
    const onEvent = mocks.openRunStream.mock.calls[0][1].onEvent;
    onEvent(delta);
    mocks.getRun.mockResolvedValueOnce({ ...run, status: 'succeeded', output: { text: 'stale answer', process_text: 'stale process' } });
    onEvent({ run_id: run.id, sequence: 2, event_type: 'run.completed', payload: { text: 'final answer', process_text: '' } });
    // Drain the GET and artifact-reconciliation awaits, not just the synchronous reducer.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(mocks.getRun).toHaveBeenCalledWith(run.id);
    expect(mocks.fetchRunArtifacts).toHaveBeenCalledWith(run.id);
    expect(useRunChatStore.getState().conversations[11].messages[1]).toMatchObject({ content: 'final answer', processText: '', status: 'done' });
  });

  it('does not clear newer C when a completed B terminal event is replayed', async () => {
    const b = { ...run, id: 'run-b' };
    mocks.createRun.mockResolvedValueOnce(b);
    await send();
    mocks.getRun.mockResolvedValueOnce({ ...b, status: 'succeeded', output: { text: 'B final' } });
    await finalizeRun(b.id);
    mocks.createRun.mockResolvedValueOnce({ ...run, id: 'run-c' });
    await useRunChatStore.getState().sendMessage({
      applicationId: 1, conversationId: 11, content: 'C question', clientRequestId: 'C-intent',
    });
    const liveC = useRunChatStore.getState().conversations[11].messages[3];
    // Exercise the reducer independently of the closed-transport guard: a
    // legitimate replay subscription for B can coexist with C's live stream.
    useRunChatStore.setState(applyEvent(useRunChatStore.getState(), {
      run_id: b.id, sequence: 10, event_type: 'run.completed', payload: { text: 'B final' },
    }));
    expect(useRunChatStore.getState().conversations[11].activeRunId).toBe('run-c');
    expect(useRunChatStore.getState().conversations[11].messages[3]).toBe(liveC);
    expect(liveC.status).toBe('streaming');

    useRunChatStore.setState(applyEvent(useRunChatStore.getState(), {
      run_id: 'run-c', sequence: 11, event_type: 'run.completed', payload: { text: 'C final' },
    }));
    expect(useRunChatStore.getState().conversations[11].activeRunId).toBeNull();
    expect(useRunChatStore.getState().conversations[11].messages[3]).toMatchObject({ status: 'done', content: 'C final' });
  });

  it('accepts events from a replacement subscription but ignores the closed handle', async () => {
    await send();
    const oldHandlers = mocks.openRunStream.mock.calls[0][1];
    closeRunStream(run.id);
    await send();
    oldHandlers.onEvent(delta);
    expect(useRunChatStore.getState().conversations[11].messages[1].content).toBe('');
    mocks.openRunStream.mock.calls[1][1].onEvent(delta);
    expect(useRunChatStore.getState().conversations[11].messages[1]).toMatchObject({ content: '', processText: 'live answer' });
  });

  it('shares one subscription on idempotent replay and closes every opened handle', async () => {
    await send();
    await send();
    const handles = mocks.openRunStream.mock.results.map((result) => result.value);
    useRunChatStore.getState().clearAll();
    expect(handles.every((handle) => handle.close.mock.calls.length === 1)).toBe(true);
    expect(mocks.openRunStream).toHaveBeenCalledTimes(1);
  });
  it('ignores a queued event after deleting its conversation instead of mixing it into the active one', async () => {
    await send();
    const handlers = mocks.openRunStream.mock.calls[0][1];
    useRunChatStore.setState((state) => ({ conversations: {
      ...state.conversations,
      22: { id: 22, title: 'other conversation', messages: [], activeRunId: null },
    } }));
    useRunChatStore.getState().setActiveConversation(22);
    useRunChatStore.getState().removeConversation(11);
    handlers.onEvent(delta);
    expect(useRunChatStore.getState().conversations[22].messages).toEqual([]);
    expect(useRunChatStore.getState().conversations[22].activeRunId).toBeNull();
  });
});
