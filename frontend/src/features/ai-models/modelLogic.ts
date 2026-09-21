import type { AIAdapter, AIConnectionInput, AIModel, AIModelInput, AIModelParameters, BrowserManifest, Invocation, InvocationStatus } from '@/services/aiModels';
import { BUILTIN_OCR_MODEL_ID, builtinOcrManifest } from './ocr/builtin';

export const statusLabels: Record<InvocationStatus, string> = { queued: '排队中', running: '执行中', succeeded: '已完成', failed: '失败', cancelled: '已取消', indeterminate: '结果不确定' };
export const terminal = (status: InvocationStatus) => !['queued', 'running'].includes(status);
export const permissionsFor = (codes: string[], all = false) => ({ read: all || codes.includes('ai.model.read'), write: all || codes.includes('ai.model.write'), secret: all || codes.includes('ai.connection.secret.write'), test: all || codes.includes('ai.model.test'), logs: all || codes.includes('ai.model.log.read') });
export type ModelPermissions = ReturnType<typeof permissionsFor>;
export function errorText(error: unknown): string {
  const e = error as { response?: { data?: { detail?: unknown; code?: string } }; message?: string };
  const detail = e?.response?.data?.detail;
  return typeof detail === 'string' ? detail : e?.message || '请求失败，请稍后重试';
}
export const AI_MODEL_LIMITS = { timeoutSeconds: 600, maxConcurrency: 64, maxOutputTokens: 32768, manifestBytes: 1024 * 1024, resourceBytes: 64 * 1024 * 1024 } as const;
export const GLM_FLASHX_CONNECTION_PRESET: AIConnectionInput = {
  name: 'GLM-4.6V-FlashX 内网连接', adapter: 'openai_chat', base_url: 'http://192.168.212.121:3000/v1',
  enabled: true, timeout_seconds: 600, max_concurrency: 2,
};
export const GLM_FLASHX_MODEL_PRESET = {
  name: 'GLM-4.6V-FlashX', model_id: 'glm-4.6v-flashx',
  capabilities: { image: true, video: false, pdf: false, streaming: false },
  default_parameters: { temperature: 0.1 },
} as const;
export const REMOTE_IMAGE_MIME_TYPES = ['image/jpeg', 'image/png', 'image/webp'] as const;
export function connectionPayload(value: AIConnectionInput): AIConnectionInput {
  if (!value.name.trim()) throw new Error('请填写连接名称');
  const raw = value.base_url.trim();
  let url: URL; try { url = new URL(raw); } catch { throw new Error('请填写有效的 HTTP(S) 地址，推荐 HTTPS'); }
  if (!raw.toLowerCase().startsWith(url.protocol + '//') || !['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password || /[?#\s]/.test(raw) || raw.includes(String.fromCharCode(92))) throw new Error('连接地址必须为 HTTP(S)，且不能包含凭据、查询参数、片段或空白');
  // Admission here never authorizes network access: backend enforces exact-host allowlisting and DNS/IP checks.
  if (!Number.isInteger(value.timeout_seconds) || value.timeout_seconds < 1 || value.timeout_seconds > AI_MODEL_LIMITS.timeoutSeconds) throw new Error('超时必须为 1–600 秒的整数');
  if (!Number.isInteger(value.max_concurrency) || value.max_concurrency < 1 || value.max_concurrency > AI_MODEL_LIMITS.maxConcurrency) throw new Error('最大并发必须为 1–64 的整数');
  return { name: value.name.trim(), adapter: value.adapter, base_url: raw, enabled: value.enabled, timeout_seconds: value.timeout_seconds, max_concurrency: value.max_concurrency };
}
export interface ModelDraft {
  name: string; model_id: string; execution_location: AIModel['execution_location']; connection_id?: string;
  enabled: boolean; capabilities: AIModel['capabilities']; default_parameters: AIModelParameters; manifest: string;
}
export function validateParameters(value: AIModelParameters): AIModelParameters {
  if (value.temperature !== undefined && (!Number.isFinite(value.temperature) || value.temperature < 0 || value.temperature > 2)) throw new Error('温度应在 0–2 之间');
  if (value.max_output_tokens !== undefined && (!Number.isInteger(value.max_output_tokens) || value.max_output_tokens < 1 || value.max_output_tokens > AI_MODEL_LIMITS.maxOutputTokens)) throw new Error('最大输出 token 数必须为 1–32768 的整数');
  return { ...value };
}
// Configuration validation intentionally has no dependency on the OCR module or runtime.
const OCR_RESOURCE_NAMES = new Set(['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_rec']);
const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value && typeof value === 'object' && !Array.isArray(value));
export function parseBrowserManifest(raw: string, basePath = import.meta.env.BASE_URL || '/'): BrowserManifest {
  if (new TextEncoder().encode(raw).byteLength > AI_MODEL_LIMITS.manifestBytes) throw new Error('资源清单不能超过 1 MB');
  let value: unknown;
  try { value = JSON.parse(raw); } catch { throw new Error('资源清单不是有效的 JSON'); }
  if (!isRecord(value) || Object.keys(value).some((key) => !['adapter', 'version', 'resources'].includes(key)) || value.adapter !== 'paddleocr_tiny' || typeof value.version !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(value.version)) throw new Error('资源清单仅允许 paddleocr_tiny、合法版本及 resources 字段');
  if (!basePath.startsWith('/') || !basePath.endsWith('/') || !basePath.split('/').slice(1, -1).every((part) => /^[A-Za-z0-9_-]+$/.test(part))) throw new Error('当前部署基础路径不合法');
  if (!Array.isArray(value.resources) || value.resources.length !== 2) throw new Error('资源清单必须且只能包含 PP-OCRv6_tiny_det 和 PP-OCRv6_tiny_rec 两项');
  const names = new Set<string>();
  const version = value.version;
  const resources = value.resources.map((resource: unknown) => {
    if (!isRecord(resource) || Object.keys(resource).some((key) => !['name', 'url', 'sha256', 'size_bytes'].includes(key)) || typeof resource.name !== 'string' || !OCR_RESOURCE_NAMES.has(resource.name) || names.has(resource.name)) throw new Error('资源名称必须为唯一的 PP-OCRv6_tiny_det / PP-OCRv6_tiny_rec，不接受其他字段');
    if (typeof resource.sha256 !== 'string' || !/^[a-f0-9]{64}$/.test(resource.sha256) || typeof resource.size_bytes !== 'number' || !Number.isSafeInteger(resource.size_bytes) || resource.size_bytes < 1 || resource.size_bytes > AI_MODEL_LIMITS.resourceBytes) throw new Error('资源需要 64 位小写 SHA-256 和 1 字节至 64 MB 范围内的真实整数字节数');
    const portable = 'ocr-assets/' + version + '/' + resource.name + '.tar';
    const expected = basePath + portable;
    if (resource.url !== portable && resource.url !== expected) throw new Error('资源必须使用内置相对路径：' + portable + '，不接受外部 URL、查询参数或片段');
    names.add(resource.name);
    return { name: resource.name, url: portable, sha256: resource.sha256, size_bytes: resource.size_bytes };
  });
  return { adapter: 'paddleocr_tiny', version, resources };
}
export function modelPayload(value: ModelDraft): AIModelInput {
  if (!value.name.trim()) throw new Error('请填写模型名称');
  const local = value.execution_location === 'browser_local';
  let manifest: BrowserManifest | null = null;
  if (local) {
    if (value.model_id.trim() && value.model_id.trim() !== BUILTIN_OCR_MODEL_ID) throw new Error(`本地 OCR 模型标识固定为 ${BUILTIN_OCR_MODEL_ID}`);
    manifest = builtinOcrManifest();
  } else if (!value.connection_id || !value.model_id.trim()) throw new Error('请选择连接并填写模型标识');
  return { name: value.name.trim(), model_id: local ? BUILTIN_OCR_MODEL_ID : value.model_id.trim(), capability_kind: local ? 'ocr' : 'chat', execution_location: value.execution_location, connection_id: local ? null : value.connection_id, browser_manifest: manifest, capabilities: local ? { image: true, video: false, pdf: false, streaming: false } : { ...value.capabilities }, default_parameters: local ? {} : validateParameters(value.default_parameters), enabled: value.enabled };
}
export function attachmentError(model: AIModel, adapter: AIAdapter | undefined, file: { type: string; size: number }, count: number): string | null {
  if (model.execution_location === 'browser_local') return '本地 OCR 不上传到服务器';
  if (count >= 4) return '每轮最多 4 个附件';
  if (file.size > 20 * 1024 * 1024) return '每个文件最多 20 MB';
  const kind = REMOTE_IMAGE_MIME_TYPES.some((type) => type === file.type) ? 'image' : ['video/mp4', 'video/webm', 'video/quicktime'].includes(file.type) ? 'video' : file.type === 'application/pdf' ? 'pdf' : null;
  if (!kind || !model.capabilities[kind] || !adapter || (adapter === 'openai_chat' && kind !== 'image')) return '当前模型或协议不支持此输入类型';
  return null;
}
function abortCheck(signal: AbortSignal) { if (signal.aborted) throw new DOMException('Polling aborted', 'AbortError'); }
export async function pollInvocation(read: (signal: AbortSignal) => Promise<Invocation>, onSnapshot: (value: Invocation) => void, signal: AbortSignal, interval = 1200): Promise<Invocation> {
  while (!signal.aborted) {
    abortCheck(signal);
    const value = await read(signal);
    abortCheck(signal);
    onSnapshot(value);
    if (terminal(value.status)) return value;
    await new Promise<void>((resolve, reject) => {
      const abort = () => { clearTimeout(timer); signal.removeEventListener('abort', abort); reject(new DOMException('Polling aborted', 'AbortError')); };
      const timer = setTimeout(() => { signal.removeEventListener('abort', abort); resolve(); }, interval);
      signal.addEventListener('abort', abort, { once: true });
      if (signal.aborted) abort();
    });
  }
  throw new DOMException('Polling aborted', 'AbortError');
}
export const bytesLabel = (bytes: number) => bytes >= 1024 * 1024 ? (bytes / 1024 / 1024).toFixed(1) + ' MB' : Math.ceil(bytes / 1024) + ' KB';
