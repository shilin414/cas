// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
vi.mock('@/shell/useIsMobile', () => ({ useIsMobile: () => false }));
vi.mock('@/components/Agents/AgentAvatar', () => ({ default: () => <span>头像</span> }));
vi.mock('@/workbench/capability/CapabilityPicker', () => ({ default: ({open}: {open: boolean}) => open ? <div>现有选择弹框</div> : null }));
import WorkbenchHome from '../home/WorkbenchHome';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
it('desktop shows the selected agent identity, without generic greeting or capability lists, and reuses picker', async () => {
 const host = document.createElement('div'); const root = createRoot(host);
 await act(async () => root.render(<WorkbenchHome current={{id: 7, name:'问数小安', description:'查询经营数据', kind:'chat'} as any} onOpen={() => {}} />));
 expect(host.textContent).toContain('查询经营数据');
 expect(host.textContent).not.toContain('今天想完成什么');
 await act(async () => (host.querySelector('.workbench-home__current') as HTMLButtonElement).click());
 expect(host.textContent).toContain('现有选择弹框');
 await act(async () => root.unmount());
});
