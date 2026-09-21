import http from './axios';

export type AIAdapter = 'openai_chat' | 'gemini';
export interface AIConnection {
  id: string; name: string; adapter: AIAdapter; base_url: string; has_credential: boolean;
  enabled: boolean; timeout_seconds: number; max_concurrency: number; version: number;
  created_at: string; updated_at: string;
}
export interface BrowserResource { name: string; url: string; sha256: string; size_bytes: number }
export interface BrowserManifest { adapter: 'paddleocr_tiny'; version: string; resources: BrowserResource[] }
export interface AIModelCapabilities { image: boolean; video: boolean; pdf: boolean; streaming: boolean }
export interface AIModelParameters { temperature?: number; max_output_tokens?: number }
export interface AIModel {
  id: string; name: string; model_id: string; capability_kind: 'chat' | 'ocr';
  execution_location: 'server_remote' | 'browser_local'; connection_id?: string | null;
  browser_manifest?: BrowserManifest | null; capabilities: AIModelCapabilities;
  default_parameters: AIModelParameters; enabled: boolean; version: number;
  created_at: string; updated_at: string;
}
export type AIConnectionInput = Pick<AIConnection, 'name' | 'adapter' | 'base_url' | 'enabled' | 'timeout_seconds' | 'max_concurrency'>;
export type AIModelInput = Pick<AIModel, 'name' | 'model_id' | 'capability_kind' | 'execution_location' | 'connection_id' | 'browser_manifest' | 'capabilities' | 'default_parameters' | 'enabled'>;
export interface Attachment { id: string; name: string; mime_type: string; size_bytes: number; url?: string; kind: 'image' | 'video' | 'pdf' }
export interface TestMessage { id: string; role: 'system' | 'user' | 'assistant'; text: string; attachments: Attachment[]; created_at: string }
export interface TestSession { id: string; model_id: string; title: string; created_at: string; expires_at: string }
export interface TestSessionDetail extends TestSession { messages: TestMessage[]; active_invocation_id?: string }
export type InvocationStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'indeterminate';
export interface InvocationMetadata {
  id: string; session_id: string; model_id: string; status: InvocationStatus;
  error_code?: string; duration_ms?: number | null; input_tokens?: number | null; output_tokens?: number | null;
  cancel_requested?: boolean; cancellation_confirmed?: boolean; created_at: string; completed_at?: string;
}
export interface Invocation extends InvocationMetadata { output_text: string; error_message?: string }
export interface TestMessageInput { text: string; attachment_ids: string[]; system_prompt?: string; parameters?: AIModelParameters; request_id: string }

const base = '/v2/admin/ai-models';
const id = encodeURIComponent;
const options = { silentError: true };
// The shared interceptor returns response.data and supplies cookie auth + CSRF.
const get = <T>(path: string, signal?: AbortSignal): Promise<T> => http.get(path, { ...options, signal }) as Promise<T>;
const post = <T>(path: string, data?: unknown): Promise<T> => http.post(path, data, options) as Promise<T>;
const patch = <T>(path: string, data: unknown): Promise<T> => http.patch(path, data, options) as Promise<T>;
const del = (path: string): Promise<void> => http.delete(path, options) as Promise<void>;
export const aiModelsApi = {
  connections: (signal?: AbortSignal) => get<AIConnection[]>(`${base}/connections`, signal),
  createConnection: (data: AIConnectionInput) => post<AIConnection>(`${base}/connections`, data),
  updateConnection: (key: string, data: Partial<AIConnectionInput>) => patch<AIConnection>(`${base}/connections/${id(key)}`, data),
  deleteConnection: (key: string) => del(`${base}/connections/${id(key)}`),
  setCredential: (key: string, credential: string): Promise<void> => http.put(`${base}/connections/${id(key)}/credential`, { credential }, options) as Promise<void>,
  models: (signal?: AbortSignal) => get<AIModel[]>(`${base}/models`, signal),
  createModel: (data: AIModelInput) => post<AIModel>(`${base}/models`, data),
  updateModel: (key: string, data: Partial<AIModelInput>) => patch<AIModel>(`${base}/models/${id(key)}`, data),
  deleteModel: (key: string) => del(`${base}/models/${id(key)}`),
  sessions: (signal?: AbortSignal) => get<TestSession[]>(`${base}/test-sessions`, signal),
  createSession: (model_id: string, title?: string) => post<TestSession>(`${base}/test-sessions`, { model_id, title }),
  session: (key: string, signal?: AbortSignal) => get<TestSessionDetail>(`${base}/test-sessions/${id(key)}`, signal),
  deleteSession: (key: string) => del(`${base}/test-sessions/${id(key)}`),
  upload: (session: string, file: File) => { const data = new FormData(); data.append('file', file); return post<Attachment>(`${base}/test-sessions/${id(session)}/attachments`, data); },
  attachment: (session: string, attachment: string): Promise<Blob> => http.get(`${base}/test-sessions/${id(session)}/attachments/${id(attachment)}`, { ...options, responseType: 'blob' }) as Promise<Blob>,
  deleteAttachment: (session: string, attachment: string) => del(`${base}/test-sessions/${id(session)}/attachments/${id(attachment)}`),
  send: (session: string, data: TestMessageInput) => post<{ invocation_id: string }>(`${base}/test-sessions/${id(session)}/messages`, data),
  invocation: (key: string, signal?: AbortSignal) => get<Invocation>(`${base}/invocations/${id(key)}`, signal),
  cancel: (key: string) => post<unknown>(`${base}/invocations/${id(key)}/cancel`),
  logs: (model?: string, signal?: AbortSignal) => get<InvocationMetadata[]>(`${base}/invocations${model ? `?model_id=${id(model)}` : ''}`, signal),
};
