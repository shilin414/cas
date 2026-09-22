// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';

vi.mock('@/shell/useIsMobile', () => ({ useIsMobile: () => true }));
vi.mock('@/components/Agents/AgentAvatar', () => ({ default: ({ application }: any) => <span>{application?.icon}</span> }));
vi.mock('@/workbench/capability/CapabilityPicker', () => ({ default: () => null }));
import WorkbenchHome from '@/workbench/home/WorkbenchHome';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('WorkbenchHome mobile presentation', () => {
  it('keeps the mobile home focused and leaves recent/recommended items in the picker', async () => {
    const host = document.createElement('div'); const root = createRoot(host);
    const app = { id: 1, slug: 'xiaoan', name: '问数小安', description: '', icon: '🤖', kind: 'chat' } as any;
    await act(async () => root.render(<WorkbenchHome current={app} onOpen={() => undefined} />));
    const hero = host.querySelector('.workbench-home__hero');
    const children = Array.from(hero?.children ?? []);
    expect(children[0]?.textContent).toBe('🤖');
    expect(children[1]?.classList.contains('workbench-home__current')).toBe(true);
    expect(children[2]?.tagName).toBe('H1');
    expect(children[2]?.textContent).toContain('今天想完成什么');
    expect(host.textContent).not.toContain('最近使用');
    expect(host.textContent).not.toContain('推荐能力');
    await act(async () => root.unmount());
  });
});
