import { describe, expect, it, vi } from 'vitest';
import { connectionPayload, modelPayload, permissionsFor, attachmentError, pollInvocation } from '../modelLogic';
import { builtinOcrManifest } from '../ocr/builtin';
import type { AIModel, Invocation } from '@/services/aiModels';
const model = { execution_location: 'server_remote', capabilities: { image: true, video: true, pdf: true, streaming: false } } as AIModel;
describe('AI model form payloads', () => {
  it('preserves false and all independent connection fields without credentials', () => {
    expect(connectionPayload({ name: ' Test ', adapter: 'gemini', base_url: 'https://example.com/v1/', enabled: false, timeout_seconds: 120, max_concurrency: 2 })).toEqual({ name: 'Test', adapter: 'gemini', base_url: 'https://example.com/v1/', enabled: false, timeout_seconds: 120, max_concurrency: 2 });
  });
  it('validates connection limits and does not lose remote capability/default fields', () => {
    expect(() => connectionPayload({ name: 'n', adapter: 'gemini', base_url: 'http://example.com', enabled: true, timeout_seconds: 0, max_concurrency: 1 })).toThrow();
    expect(modelPayload({ name: 'test', model_id: 'test', execution_location: 'server_remote', connection_id: 'c', enabled: false, capabilities: model.capabilities, default_parameters: { temperature: 0, max_output_tokens: 12 }, manifest: '' })).toMatchObject({ capability_kind: 'chat', connection_id: 'c', enabled: false, default_parameters: { temperature: 0, max_output_tokens: 12 }, capabilities: model.capabilities, browser_manifest: null });
  });
  it('always uses the fixed OCR resource manifest and clears remote connection', () => {
    expect(modelPayload({ name: 'ocr', model_id: '', execution_location: 'browser_local', connection_id: 'old', enabled: true, capabilities: model.capabilities, default_parameters: {}, manifest: '{stale-admin-input}' })).toMatchObject({
      model_id: 'PP-OCRv6_tiny', capability_kind: 'ocr', connection_id: null, browser_manifest: builtinOcrManifest(),
    });
    expect(() => modelPayload({ name: 'ocr', model_id: 'other', execution_location: 'browser_local', enabled: true, capabilities: model.capabilities, default_parameters: {}, manifest: '' })).toThrow();
  });
});
describe('permissions and supported inputs', () => {
  it('separates read/write/secret/test/log', () => {
    expect(permissionsFor(['ai.model.read', 'ai.model.test'])).toEqual({ read: true, write: false, secret: false, test: true, logs: false });
    expect(permissionsFor(['ai.model.write']).secret).toBe(false);
    expect(permissionsFor([], true)).toEqual({ read: true, write: true, secret: true, test: true, logs: true });
  });
  it('rejects unsupported adapter inputs, oversize and local uploads', () => {
    expect(attachmentError(model, 'openai_chat', { type: 'application/pdf', size: 10 }, 0)).toContain('不支持');
    expect(attachmentError(model, 'gemini', { type: 'application/pdf', size: 10 }, 0)).toBeNull();
    expect(attachmentError(model, 'gemini', { type: 'image/png', size: 21 * 1024 * 1024 }, 0)).toContain('20 MB');
    expect(attachmentError(model, 'gemini', { type: 'image/png', size: 10 }, 4)).toContain('4');
    expect(attachmentError({ ...model, execution_location: 'browser_local' }, 'gemini', { type: 'image/png', size: 10 }, 0)).toContain('本地');
  });
});
describe('invocation polling', () => {
  const snapshot = (status: Invocation['status']) => ({ id: 'i', status, output_text: 'snapshot', cancel_requested: false, cancellation_confirmed: false }) as Invocation;
  it('reports real complete snapshots and stops on failed terminal status', async () => {
    const read = vi.fn().mockResolvedValueOnce(snapshot('running')).mockResolvedValueOnce(snapshot('failed')); const seen = vi.fn();
    expect((await pollInvocation(read, seen, new AbortController().signal, 0)).status).toBe('failed');
    expect(seen).toHaveBeenCalledTimes(2); expect(read).toHaveBeenCalledTimes(2);
  });
  it('surfaces network failures rather than silently marking success', async () => {
    await expect(pollInvocation(() => Promise.reject(new Error('offline')), vi.fn(), new AbortController().signal, 0)).rejects.toThrow('offline');
  });
  it('aborts without delivering a late snapshot or cancelling upstream implicitly', async () => {
    const controller = new AbortController(); const seen = vi.fn();
    const read = async () => { controller.abort(); return snapshot('running'); };
    await expect(pollInvocation(read, seen, controller.signal, 0)).rejects.toMatchObject({ name: 'AbortError' }); expect(seen).not.toHaveBeenCalled();
  });
  it.each(['succeeded', 'cancelled', 'indeterminate'] as const)('terminates on %s', async (status) => {
    const read = vi.fn().mockResolvedValue(snapshot(status)); await pollInvocation(read, vi.fn(), new AbortController().signal, 0); expect(read).toHaveBeenCalledTimes(1);
  });
});
