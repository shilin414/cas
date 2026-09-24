// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';
import RecentNavigationIcon from '../RecentNavigationIcon';
import TaskListItem from '@/workbench/tasks/TaskListItem';
import { useNavigationPreferencesStore } from '@/stores/useNavigationPreferencesStore';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('switches recent capability and task icons together without losing task state', async () => {
  const host = document.createElement('div');
  const root = createRoot(host);
  try {
    useNavigationPreferencesStore.setState({ iconMode: 'outline' });
    await act(async () => root.render(<>
      <RecentNavigationIcon kind="chat" emoji="🤖" />
      <TaskListItem compact task={{ id: '1', title: '测试任务', applicationId: 1, applicationName: '小安', executionState: 'running', createdAt: '2026-09-24', updatedAt: '2026-09-24' }} onOpen={() => {}} onRename={async () => {}} onDelete={async () => {}} />
    </>));
    expect(host.querySelectorAll('.sidebar-navigation-icon svg')).toHaveLength(2);
    await act(async () => useNavigationPreferencesStore.getState().setIconMode('emoji'));
    expect(host.querySelectorAll('svg')).toHaveLength(0);
    expect(host.textContent).toContain('🤖');
    expect(host.textContent).toContain('💬');
    await act(async () => useNavigationPreferencesStore.getState().setIconMode('hidden'));
    expect(host.querySelectorAll('.sidebar-navigation-icon')).toHaveLength(0);
    expect(host.textContent).toContain('测试任务');
    expect(host.textContent).toContain('运行中');
  } finally {
    await act(async () => root.unmount());
    useNavigationPreferencesStore.setState({ iconMode: 'outline' });
  }
});
