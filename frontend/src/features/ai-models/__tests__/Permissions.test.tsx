// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
const api = vi.hoisted(() => ({ models: vi.fn(), connections: vi.fn(), logs: vi.fn(), sessions: vi.fn(), invocation: vi.fn() }));
vi.mock('@/services/aiModels', () => ({ aiModelsApi: api }));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: (select: (state: unknown) => unknown) => select({ user: { id: 'owner' } }) }));
vi.mock('@/stores/useAdminPermissionStore', () => ({ useAdminPermissionStore: (select: (state: unknown) => unknown) => select({ identity: null }) }));
import { AIModelsWorkspace } from '../AIModelsPage';
import { permissionsFor } from '../modelLogic';
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
const connection = { id: 'c', name: 'Connection', adapter: 'gemini', base_url: 'https://example.com/v1', has_credential: true, enabled: true, timeout_seconds: 77, max_concurrency: 3, version: 1, created_at: '', updated_at: '' };
let root: Root; let host: HTMLDivElement;
beforeEach(() => { vi.resetAllMocks(); api.models.mockResolvedValue([]); api.connections.mockResolvedValue([connection]); api.logs.mockResolvedValue([]); api.sessions.mockResolvedValue([]); host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
function button(text: string) { const found = Array.from(host.querySelectorAll('button')).find((item) => item.textContent === text); if (!found) throw new Error('Missing button: ' + text); return found; }
async function tab(text: string) { await act(async () => { (Array.from(host.querySelectorAll('[role="tab"]')).find((item) => item.textContent === text) as HTMLElement).click(); }); }
describe('permission boundaries and low-risk logs', () => {
  it('does not fetch any private/admin data without read permission', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor([])} />));
    expect(host.textContent).toContain('ai.model.read'); expect(api.models).not.toHaveBeenCalled(); expect(api.connections).not.toHaveBeenCalled();
  });
  it('read-only users cannot write, set secrets, test or read metadata logs', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.read'])} />));
    expect(button('添加模型').disabled).toBe(true);
    await tab('连接管理'); expect(button('设置密钥').disabled).toBe(true); expect(button('编辑').disabled).toBe(true);
    await tab('测试台'); expect(host.textContent).toContain('ai.model.test'); expect(api.sessions).not.toHaveBeenCalled();
    await tab('调用记录'); expect(host.textContent).toContain('ai.model.log.read'); expect(api.logs).not.toHaveBeenCalled();
  });
  it('write permission does not imply secret write', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.read', 'ai.model.write'])} />));
    expect(button('添加模型').disabled).toBe(false); await tab('连接管理'); expect(button('编辑').disabled).toBe(false); expect(button('设置密钥').disabled).toBe(true);
  });
  it('secret write is available independently from metadata write', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.read', 'ai.connection.secret.write'])} />));
    await tab('连接管理'); expect(button('编辑').disabled).toBe(true); expect(button('设置密钥').disabled).toBe(false);
  });
  it('log-only users enter metadata without requesting connections or models', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.log.read'])} />));
    expect(api.logs).toHaveBeenCalledTimes(1); expect(api.models).not.toHaveBeenCalled(); expect(api.connections).not.toHaveBeenCalled(); expect(api.sessions).not.toHaveBeenCalled();
    expect(host.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe('调用记录');
  });
  it('test-only users never fetch configuration without model.read', async () => {
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.test'])} />));
    expect(api.models).not.toHaveBeenCalled(); expect(api.connections).not.toHaveBeenCalled(); expect(host.textContent).toContain('已具备测试权限');
  });
  it('logs fetch only metadata and ignore accidental transcript fields', async () => {
    api.logs.mockResolvedValue([{ id: 'i', model_id: 'm', status: 'succeeded', created_at: '2026-09-21T00:00:00Z', input_tokens: null, output_tokens: undefined, output_text: 'PRIVATE BODY MUST NOT RENDER', prompt: 'PRIVATE PROMPT' }]);
    await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor(['ai.model.read', 'ai.model.log.read'])} />));
    expect(api.logs).not.toHaveBeenCalled(); await tab('调用记录'); expect(api.logs).toHaveBeenCalledTimes(1); expect(api.invocation).not.toHaveBeenCalled(); expect(api.sessions).not.toHaveBeenCalled();
    expect(host.textContent).not.toContain('PRIVATE BODY'); expect(host.textContent).not.toContain('PRIVATE PROMPT'); expect(host.textContent).toContain('— / — tokens');
  });
});
