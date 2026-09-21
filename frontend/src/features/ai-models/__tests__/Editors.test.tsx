// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ConnectionForm, ModelForm } from '../Editors';
import OcrEntry from '../OcrEntry';
import type { AIConnection, AIModel } from '@/services/aiModels';
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
Object.defineProperty(window, 'matchMedia', { writable: true, value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }) });
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
const connection: AIConnection = { id: 'c', name: 'Connection', adapter: 'gemini', base_url: 'https://example.com/v1', has_credential: true, enabled: false, timeout_seconds: 77, max_concurrency: 3, version: 1, created_at: '', updated_at: '' };
const model: AIModel = { id: 'm', name: 'Chat', model_id: 'real-model', execution_location: 'server_remote', capability_kind: 'chat', connection_id: 'c', capabilities: { image: true, video: true, pdf: false, streaming: true }, default_parameters: { temperature: 0, max_output_tokens: 42 }, enabled: false, version: 1, created_at: '', updated_at: '' };
let root: Root; let host: HTMLDivElement;
beforeEach(() => { host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host); });
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
async function submit() { await act(async () => { host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); }); }
async function change(label: string, value: string) { const input = host.querySelector('[aria-label="' + label + '"]') as HTMLInputElement; await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })); }); }
describe('actual controlled configuration forms', () => {
  it('submits every connection field including untouched fields and enabled=false, never secret', async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    await act(async () => root.render(<ConnectionForm initial={connection} onSave={save} onCancel={() => {}} />));
    await change('连接名称', ' Renamed '); await submit();
    expect(save).toHaveBeenCalledWith({ name: 'Renamed', adapter: 'gemini', base_url: 'https://example.com/v1', timeout_seconds: 77, max_concurrency: 3, enabled: false });
  });
  it('submits complete model capability/default fields without unregistered field omissions', async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    await act(async () => root.render(<ModelForm initial={model} connections={[connection]} onSave={save} onCancel={() => {}} />));
    await change('显示名称', ' Renamed Model '); await submit();
    expect(save).toHaveBeenCalledWith({ name: 'Renamed Model', model_id: 'real-model', capability_kind: 'chat', execution_location: 'server_remote', connection_id: 'c', browser_manifest: null, enabled: false, capabilities: model.capabilities, default_parameters: { temperature: 0, max_output_tokens: 42 } });
  });
  it('shows invalid form errors without sending', async () => {
    const save = vi.fn(); await act(async () => root.render(<ConnectionForm onSave={save} onCancel={() => {}} />)); await submit();
    expect(host.textContent).toContain('请填写连接名称'); expect(save).not.toHaveBeenCalled();
  });
  it('keeps API errors visible without closing', async () => {
    const save = vi.fn().mockRejectedValue({ response: { data: { detail: '连接已被占用' } } }); const cancel = vi.fn();
    await act(async () => root.render(<ConnectionForm initial={connection} onSave={save} onCancel={cancel} />)); await submit();
    expect(host.textContent).toContain('连接已被占用'); expect(cancel).not.toHaveBeenCalled();
  });
});
describe('cross-layer form alignment', () => {
  it('warns prominently for HTTP without rejecting it or adding a network bypass', async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    await act(async () => root.render(<ConnectionForm initial={connection} onSave={save} onCancel={() => {}} />));
    await change('服务地址', 'http://127.0.0.1:18080/v1');
    expect(host.textContent).toContain('HTTP 为非加密连接'); expect(host.textContent).toContain('AI_MODEL_ALLOWED_HOSTS');
    expect(host.textContent).toContain('不能绕过后端网络校验');
    expect(host.querySelector('[aria-label="超时（秒）"]')?.getAttribute('aria-valuemax')).toBe('600');
    expect(host.querySelector('[aria-label="最大并发"]')?.getAttribute('aria-valuemax')).toBe('64');
    await submit(); expect(save).toHaveBeenCalledWith({ name: 'Connection', adapter: 'gemini', base_url: 'http://127.0.0.1:18080/v1', timeout_seconds: 77, max_concurrency: 3, enabled: false });
  });
  it('aligns the configured default output token input maximum', async () => {
    await act(async () => root.render(<ModelForm initial={model} connections={[connection]} onSave={vi.fn()} onCancel={() => {}} />));
    expect(host.querySelector('[aria-label="默认最大输出 token"]')?.getAttribute('aria-valuemax')).toBe('32768');
  });
  const local: AIModel = { ...model, model_id: 'PP-OCRv6_tiny', execution_location: 'browser_local', capability_kind: 'ocr', browser_manifest: null };
  it('uses the fixed built-in OCR manifest without exposing JSON configuration', async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    await act(async () => root.render(<ModelForm initial={local} connections={[]} onSave={save} onCancel={() => {}} />));
    expect(host.textContent).toContain('OCR 已内置，无需填写资源 JSON');
    expect(host.textContent).toContain('ppocrv6-tiny-20260921');
    expect(host.querySelector('[aria-label="资源 Manifest（JSON）"]')).toBeNull();
    await submit();
    expect(save).toHaveBeenCalledWith(expect.objectContaining({
      model_id: 'PP-OCRv6_tiny', connection_id: null,
      browser_manifest: expect.objectContaining({ adapter: 'paddleocr_tiny', version: 'ppocrv6-tiny-20260921' }),
    }));
  });
  it('fills the GLM connection preset without copying any credential', async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    await act(async () => root.render(<ConnectionForm onSave={save} onCancel={() => {}} />));
    const preset = [...host.querySelectorAll('button')].find((button) => button.textContent?.includes('GLM-4.6V-FlashX 预设'))!;
    await act(async () => preset.click()); await submit();
    expect(save).toHaveBeenCalledWith({ name: 'GLM-4.6V-FlashX 内网连接', adapter: 'openai_chat', base_url: 'http://192.168.212.121:3000/v1', enabled: true, timeout_seconds: 600, max_concurrency: 2 });
    expect(JSON.stringify(save.mock.calls)).not.toContain('credential');
  });
});
describe('explicit OCR consent boundary', () => {
  const local: AIModel = { ...model, enabled: true, execution_location: 'browser_local', capability_kind: 'ocr', browser_manifest: { adapter: 'paddleocr_tiny', version: '1', resources: [] } };
  it('imports only after the explicit start click, not initial rendering', async () => {
    const loader = vi.fn().mockResolvedValue({ default: ({ model: value }: { model: AIModel }) => <div>OCR {value.id}</div> });
    await act(async () => root.render(<OcrEntry model={local} allowed loader={loader} />));
    expect(loader).not.toHaveBeenCalled(); expect(host.textContent).toContain('本地 OCR 尚未加载');
    await act(async () => (host.querySelector('button') as HTMLButtonElement).click());
    expect(loader).toHaveBeenCalledTimes(1); expect(host.textContent).toContain('OCR m');
  });
  it('disables loading for denied permissions and disabled models', async () => {
    const loader = vi.fn(); await act(async () => root.render(<OcrEntry model={local} allowed={false} loader={loader} />));
    expect((host.querySelector('button') as HTMLButtonElement).disabled).toBe(true); expect(loader).not.toHaveBeenCalled();
    await act(async () => root.render(<OcrEntry model={{ ...local, enabled: false }} allowed loader={loader} />));
    expect((host.querySelector('button') as HTMLButtonElement).disabled).toBe(true); expect(loader).not.toHaveBeenCalled();
  });
  it('displays import failure and permits explicit retry', async () => {
    const loader = vi.fn().mockRejectedValue(new Error('resource unavailable'));
    await act(async () => root.render(<OcrEntry model={local} allowed loader={loader} />)); await act(async () => (host.querySelector('button') as HTMLButtonElement).click());
    expect(host.textContent).toContain('resource unavailable'); expect((host.querySelector('button') as HTMLButtonElement).disabled).toBe(false);
  });
});
