import { describe, expect, it } from 'vitest';
import { attachmentError, connectionPayload, modelPayload, parseBrowserManifest, validateParameters, type ModelDraft } from '../modelLogic';
import { builtinOcrManifest } from '../ocr/builtin';
import type { AIConnectionInput, AIModel } from '@/services/aiModels';
const connection: AIConnectionInput = { name: 'internal', adapter: 'openai_chat', base_url: 'https://example.com/v1', enabled: true, timeout_seconds: 600, max_concurrency: 64 };
const model = { execution_location: 'server_remote', capabilities: { image: true, video: true, pdf: true, streaming: false } } as AIModel;
const resource = (name: string) => ({ name, url: 'ocr-assets/v1/' + name + '.tar', sha256: 'a'.repeat(64), size_bytes: 12 });
const manifest = () => ({ adapter: 'paddleocr_tiny', version: 'v1', resources: ['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_rec'].map(resource) });
const draft = (value = ''): ModelDraft => ({ name: 'OCR', model_id: '', execution_location: 'browser_local', enabled: true, capabilities: model.capabilities, default_parameters: {}, manifest: value });
describe('frontend / backend / OCR validation alignment', () => {
  it.each(['openai_chat', 'gemini'] as const)('rejects GIF before upload for %s', (adapter) => {
    expect(attachmentError(model, adapter, { type: 'image/gif', size: 100 }, 0)).toContain('不支持');
    for (const type of ['image/jpeg', 'image/png', 'image/webp']) expect(attachmentError(model, adapter, { type, size: 100 }, 0)).toBeNull();
  });
  it('accepts HTTP input without claiming the deployment permits the host', () => {
    expect(connectionPayload({ ...connection, base_url: 'http://127.0.0.1:18080/v1' }).base_url).toBe('http://127.0.0.1:18080/v1');
    expect(connectionPayload({ ...connection, base_url: 'http://internal.example/v1' })).not.toHaveProperty('allow_private');
  });
  it.each(['ftp://example.com', 'javascript:alert(1)', 'https://user:secret@example.com', 'http://example.com/?key=secret', 'http://example.com/#fragment', 'http://example.com/?'])('rejects unsafe or unsupported connection URL %s', (base_url) => {
    expect(() => connectionPayload({ ...connection, base_url })).toThrow();
  });
  it('accepts the documented 600 second timeout and rejects one over the limit or non-integers', () => {
    expect(connectionPayload(connection)).toEqual(connection);
    for (const timeout_seconds of [0, 601, 1.5, NaN]) expect(() => connectionPayload({ ...connection, timeout_seconds })).toThrow();
    for (const max_concurrency of [0, 65, 1.5, NaN]) expect(() => connectionPayload({ ...connection, max_concurrency })).toThrow();
    expect(validateParameters({ temperature: 0, max_output_tokens: 32768 })).toEqual({ temperature: 0, max_output_tokens: 32768 });
    for (const max_output_tokens of [0, 32769, 1.5, NaN]) expect(() => validateParameters({ max_output_tokens })).toThrow();
    for (const temperature of [-0.1, 2.1, NaN, Infinity]) expect(() => validateParameters({ temperature })).toThrow();
  });
  it('stores only the fixed built-in OCR manifest', () => {
    expect(modelPayload(draft('{ignored}')).browser_manifest).toEqual(builtinOcrManifest());
    expect(() => modelPayload({ ...draft(), model_id: 'other-model' })).toThrow();
  });
  it('accepts portable and deployment-prefixed manifests, normalizing both to portable storage', () => {
    expect(parseBrowserManifest(JSON.stringify(manifest()), '/xiaoan-platform/')).toEqual(manifest());
    const based = manifest(); for (const item of based.resources) item.url = '/xiaoan-platform/' + item.url;
    expect(parseBrowserManifest(JSON.stringify(based), '/xiaoan-platform/')).toEqual(manifest());
    expect(() => parseBrowserManifest(JSON.stringify(based), '/')).toThrow();
  });
  it('requires exactly the trusted tiny det and rec resource names', () => {
    for (const names of [[], ['PP-OCRv6_tiny_det'], ['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_det'], ['PP-OCRv6_tiny_det', 'untrusted'], ['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_rec', 'extra']]) {
      expect(() => parseBrowserManifest(JSON.stringify({ ...manifest(), resources: names.map(resource) }))).toThrow();
    }
  });
  it.each(['https://example.com/a.tar', '//example.com/a.tar', 'ocr-assets/v2/PP-OCRv6_tiny_det.tar', 'ocr-assets/v1/PP-OCRv6_tiny_rec.tar', 'ocr-assets/v1/PP-OCRv6_tiny_det.onnx', 'ocr-assets/v1/PP-OCRv6_tiny_det.tar?q=1', '../ocr-assets/v1/PP-OCRv6_tiny_det.tar'])('rejects non-canonical or non-matching OCR path %s', (url) => {
    const value = manifest(); value.resources[0].url = url; expect(() => parseBrowserManifest(JSON.stringify(value))).toThrow();
  });
  it.each(['../v1', 'v1/path', '', 'a'.repeat(65)])('rejects invalid resource version %s', (version) => {
    expect(() => parseBrowserManifest(JSON.stringify({ ...manifest(), version }))).toThrow();
  });
  it('rejects oversized resources, malformed hashes, unknown fields and null resource entries', () => {
    const oversized = manifest(); oversized.resources[0].size_bytes = 64 * 1024 * 1024 + 1; expect(() => parseBrowserManifest(JSON.stringify(oversized))).toThrow();
    const badHash = manifest(); badHash.resources[0].sha256 = 'A'.repeat(64); expect(() => parseBrowserManifest(JSON.stringify(badHash))).toThrow();
    expect(() => parseBrowserManifest(JSON.stringify({ ...manifest(), script: 'evil' }))).toThrow();
    expect(() => parseBrowserManifest(JSON.stringify({ ...manifest(), resources: [null, resource('PP-OCRv6_tiny_rec')] }))).toThrow();
  });
});
