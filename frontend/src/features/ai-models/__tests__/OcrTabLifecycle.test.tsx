// @vitest-environment jsdom
import React, { act, useEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
const state = vi.hoisted(() => ({ disposed: vi.fn(), mounted: vi.fn(), models: vi.fn(), connections: vi.fn() }));
vi.mock('@/services/aiModels', () => ({ aiModelsApi: { models: state.models, connections: state.connections } }));
vi.mock('../OcrEntry', () => ({ default: function Entry() { useEffect(() => { state.mounted(); return () => state.disposed(); }, []); return <div data-testid="ocr-entry-instance">Local OCR</div>; } }));
vi.mock('../RemoteTestPanel', () => ({ default: () => null }));
vi.mock('../InvocationLogs', () => ({ default: () => null }));
import { AIModelsWorkspace } from '../AIModelsPage';
import { permissionsFor } from '../modelLogic';
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
let root: Root; let host: HTMLDivElement;
beforeEach(() => {
 vi.clearAllMocks();
 state.models.mockResolvedValue([{ id: 'local', name: 'Local model', model_id: 'PP-OCRv6_tiny', execution_location: 'browser_local', capability_kind: 'ocr', enabled: true, version: 1, capabilities: { image: true }, browser_manifest: { version: 'v1' } }]);
 state.connections.mockResolvedValue([]);host = document.createElement('div');document.body.appendChild(host);root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount());host.remove(); });
it('unmounts local OCR when leaving its tab even though other AntD panes retain their state', async () => {
 await act(async () => root.render(<AIModelsWorkspace permissions={permissionsFor([], true)} />));
 expect(state.mounted).not.toHaveBeenCalled();
 const test = [...host.querySelectorAll('button')].find(item => item.textContent === '测试模型')!;
 await act(async () => test.click());
 expect(state.mounted).toHaveBeenCalledTimes(1);
 const list = [...host.querySelectorAll<HTMLElement>('[role="tab"]')].find(item => item.textContent === '模型列表')!;
 await act(async () => list.click());
 expect(state.disposed).toHaveBeenCalledTimes(1);
 expect(host.querySelector('[data-testid="ocr-entry-instance"]')).toBeNull();
});
