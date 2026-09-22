// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
const api = vi.hoisted(() => ({ overview: vi.fn(), runs: vi.fn(), updateCapacity: vi.fn() }));
vi.mock('../operationsApi', async original => ({ ...await original<object>(), operationsApi: api }));
vi.mock('@/services/axios', () => ({ default: { get: vi.fn(() => { throw new Error('Unexpected HTTP'); }), post: vi.fn(), patch: vi.fn() } }));
import OperationsPage from '../OperationsPage';
import { useAuthStore } from '@/stores/useAuthStore';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
const originalLoad = useAdminPermissionStore.getState().load;
let host: HTMLDivElement; let root: Root;
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
const originalStyle = window.getComputedStyle.bind(window); window.getComputedStyle = element => originalStyle(element);
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
function grant() { useAdminPermissionStore.setState({ loading: false, error: null, loadedForUserId: '7', identity: { can_access_console: true, is_super_admin: false, roles: [], permissions: [{ id: 1, code: 'run.monitor.read', category: 'run', name: 'read', description: '', created_at: '2026-09-22T00:00:00Z' }] } }); }
beforeEach(() => {
 vi.resetAllMocks(); host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
 useAuthStore.setState({ user: { id: '7', username: 'admin', email: '', role: '', created_at: '', is_staff: true }, isAuthenticated: true });
 useAdminPermissionStore.setState({ identity: null, loadedForUserId: null, loading: false, error: null });
 api.overview.mockResolvedValue({ sampled_at: '2026-09-22T00:00:00Z', providers: [], alerts: [], limitations: [], totals: { queued: 0, running: 0, waiting_input: 0, waiting_external: 0, cancelling: 0, pending_occurrences: 0, pending_deliveries: 0, sending_deliveries: 0, pending_outbox: 0 } });
 api.runs.mockResolvedValue({ results: [], next_cursor: null });
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); useAdminPermissionStore.setState({ load: originalLoad, error: null }); });
it('loads permissions on mobile direct entry rather than trusting staff or waiting for a drawer', async () => {
 let release!: () => void; const ready = new Promise<void>(resolve => { release = resolve; });
 const load = vi.fn(async () => { useAdminPermissionStore.setState({ loading: true }); await ready; grant(); });
 useAdminPermissionStore.setState({ load });
 await act(async () => root.render(<OperationsPage mobile />));
 expect(load).toHaveBeenCalledWith('7'); expect(host.textContent).toContain('正在加载运行中心权限'); expect(api.overview).not.toHaveBeenCalled();
 await act(async () => { release(); await ready; });
 expect(api.overview).toHaveBeenCalledOnce(); expect(host.textContent).toContain('运行中心'); expect(host.textContent).not.toContain('修改额度');
});
it('shows a permission-load error with explicit retry and does not loop requests', async () => {
 const load = vi.fn(async (_userId: string, force?: boolean) => { if (force) grant(); else useAdminPermissionStore.setState({ loading: false, error: 'failed' }); });
 useAdminPermissionStore.setState({ load });
 await act(async () => root.render(<OperationsPage mobile />));
 expect(host.textContent).toContain('运行中心权限加载失败'); expect(load).toHaveBeenCalledTimes(1); expect(api.overview).not.toHaveBeenCalled();
 const retry = [...host.querySelectorAll('button')].find(button => button.textContent === '重新加载权限'); expect(retry).toBeTruthy();
 await act(async () => retry!.click());
 expect(load).toHaveBeenLastCalledWith('7', true); expect(api.overview).toHaveBeenCalledOnce();
});
