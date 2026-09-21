// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';
import TaskListItem from '@/workbench/tasks/TaskListItem';

vi.mock('@/workbench/tasks/TaskMenu', () => ({ default: () => null }));
const task = { id: '1', title: '销售分析', applicationId: 1, applicationName: '问数小安', preview: '这是一段很长的详细对话内容，不应该出现在最近任务侧栏里', createdAt: '2026-09-19T00:00:00Z', updatedAt: '2026-09-19T00:00:00Z', executionState: 'idle' as const };

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('TaskListItem compact mode', () => {
  it('keeps recent tasks compact and hides message preview text', async () => {
    const host = document.createElement('div'); const root = createRoot(host);
    await act(async () => root.render(<TaskListItem task={task} compact onOpen={() => undefined} onRename={async () => undefined} onDelete={async () => undefined} />));
    expect(host.textContent).toContain('销售分析');
    expect(host.textContent).toContain('问数小安');
    expect(host.textContent).not.toContain('详细对话内容');
    await act(async () => root.unmount());
  });
});
