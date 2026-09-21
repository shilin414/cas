import { beforeEach, describe, expect, it, vi } from 'vitest';
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), patch: vi.fn(), put: vi.fn(), delete: vi.fn() }));
vi.mock('@/services/axios', () => ({ default: http }));
import { aiModelsApi } from '@/services/aiModels';
import { filterEnterpriseSections } from '@/pages/Enterprise/enterpriseNav';
import { groupAdminPermissions } from '@/pages/Enterprise/permissionCatalog';
beforeEach(() => vi.resetAllMocks());
describe('AI HTTP contract', () => {
  it('updates credentials only through the independent credential endpoint', async () => {
    http.put.mockResolvedValue(undefined); await aiModelsApi.setCredential('a/b', 'secret');
    expect(http.put).toHaveBeenCalledWith('/v2/admin/ai-models/connections/a%2Fb/credential', { credential: 'secret' }, { silentError: true }); expect(http.patch).not.toHaveBeenCalled();
  });
  it('sends one stable invocation request and polls a cancellable GET', async () => {
    const request = { text: 'hello', attachment_ids: ['f'], system_prompt: 'system', parameters: { temperature: 0 }, request_id: 'request' };
    await aiModelsApi.send('s', request); expect(http.post).toHaveBeenCalledWith('/v2/admin/ai-models/test-sessions/s/messages', request, { silentError: true });
    const signal = new AbortController().signal; await aiModelsApi.invocation('i', signal); expect(http.get).toHaveBeenCalledWith('/v2/admin/ai-models/invocations/i', { silentError: true, signal });
  });
  it('requests metadata list separately from owner-only details', async () => {
    await aiModelsApi.logs('m/1'); expect(http.get).toHaveBeenCalledWith('/v2/admin/ai-models/invocations?model_id=m%2F1', { silentError: true, signal: undefined });
  });
  it('uploads the real file as multipart and downloads through authenticated transport', async () => {
    const file = new File(['test'], 'page.pdf', { type: 'application/pdf' }); await aiModelsApi.upload('s', file);
    const form = http.post.mock.calls[0][1] as FormData; expect(form.get('file')).toBe(file);
    await aiModelsApi.attachment('s', 'f'); expect(http.get).toHaveBeenCalledWith('/v2/admin/ai-models/test-sessions/s/attachments/f', { silentError: true, responseType: 'blob' });
  });
});
describe('enterprise navigation', () => {
  it.each(['ai.model.read', 'ai.model.test', 'ai.model.log.read'])('shows model entry independently for %s', (code) => {
    const items = filterEnterpriseSections(new Set([code]), false).flatMap((section) => section.items); expect(items.some((item) => item.key === 'ai-models')).toBe(true);
  });
  it('does not infer read access merely from write or secret permissions', () => {
    const items = filterEnterpriseSections(new Set(['ai.model.write', 'ai.connection.secret.write']), false).flatMap((section) => section.items); expect(items.some((item) => item.key === 'ai-models')).toBe(false);
  });
  it('labels all AI permissions as a distinct group', () => {
    expect(groupAdminPermissions([{ id: 1, category: 'ai', code: 'ai.model.read', name: 'read', description: '', created_at: '' }])[0].label).toBe('AI 模型');
  });
});
