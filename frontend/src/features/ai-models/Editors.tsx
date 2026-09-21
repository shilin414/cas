import React, { useState } from 'react';
import { Alert, Button, Checkbox, Input, InputNumber, Modal, Select, Switch } from 'antd';
import type { AIConnection, AIConnectionInput, AIModel, AIModelInput } from '@/services/aiModels';
import {
  AI_MODEL_LIMITS, connectionPayload, errorText, GLM_FLASHX_CONNECTION_PRESET, GLM_FLASHX_MODEL_PRESET,
  modelPayload, type ModelDraft,
} from './modelLogic';
import { BUILTIN_OCR_MODEL_ID, BUILTIN_OCR_NAME, BUILTIN_OCR_VERSION, builtinOcrManifest } from './ocr/builtin';

export function Field({ label, children, hint }: { label: string; children: React.ReactNode; hint?: string }) {
  return <div className="aim-field"><div className="aim-field-label">{label}</div>{children}{hint && <small>{hint}</small>}</div>;
}
const freshConnection: AIConnectionInput = { name: '', adapter: 'openai_chat', base_url: '', enabled: true, timeout_seconds: 120, max_concurrency: 2 };
export function ConnectionForm({ initial, onSave, onCancel }: { initial?: AIConnection; onSave: (value: AIConnectionInput) => Promise<void>; onCancel: () => void }) {
  const [draft, setDraft] = useState<AIConnectionInput>(initial ? { ...initial } : freshConnection);
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const update = <K extends keyof AIConnectionInput>(key: K, value: AIConnectionInput[K]) => setDraft((old) => ({ ...old, [key]: value }));
  return <form className="aim-form" onSubmit={async (event) => { event.preventDefault(); if (busy) return; setError(''); setBusy(true); try { await onSave(connectionPayload(draft)); } catch (e) { setError(errorText(e)); } finally { setBusy(false); } }}>
    <Alert type="info" showIcon message="连接配置不含密钥" description="保存后在连接列表独立设置密钥。密钥不可回读，且需要单独权限。" />
    {!initial && <Button type="dashed" onClick={() => setDraft({ ...GLM_FLASHX_CONNECTION_PRESET })}>使用 GLM-4.6V-FlashX 预设</Button>}
    {error && <Alert type="error" showIcon message={error} />}
    <Field label="连接名称"><Input aria-label="连接名称" autoFocus value={draft.name} maxLength={120} onChange={(e) => update('name', e.target.value)} /></Field>
    <Field label="协议适配器"><Select aria-label="协议适配器" value={draft.adapter} onChange={(value) => update('adapter', value)} options={[{ value: 'openai_chat', label: 'OpenAI Chat · 文字 / 图片' }, { value: 'gemini', label: 'Gemini · 原生多模态' }]} /></Field>
    <Field label="服务地址" hint="默认推荐 HTTPS；HTTP 仅用于服务端明确允许的内网或本地连接。网络准入由后端验证。"><Input aria-label="服务地址" value={draft.base_url} placeholder="https://api.example.com/v1" autoComplete="off" onChange={(e) => update('base_url', e.target.value)} /></Field>
    {draft.base_url.trim().toLowerCase().startsWith('http://') && <Alert type="warning" showIcon message="HTTP 为非加密连接" description="密钥、提示词及附件可能以明文传输。仅当服务端 AI_MODEL_ALLOWED_HOSTS 精确允许此 host 时才可使用；在此保存配置不能绕过后端网络校验。" />}
    <div className="aim-form-grid"><Field label="超时（秒）" hint="1–600 秒"><InputNumber aria-label="超时（秒）" min={1} max={AI_MODEL_LIMITS.timeoutSeconds} precision={0} value={draft.timeout_seconds} onChange={(value) => update('timeout_seconds', value ?? 0)} /></Field><Field label="最大并发" hint="1–64"><InputNumber aria-label="最大并发" min={1} max={AI_MODEL_LIMITS.maxConcurrency} precision={0} value={draft.max_concurrency} onChange={(value) => update('max_concurrency', value ?? 0)} /></Field></div>
    <Field label="启用连接"><Switch aria-label="启用连接" checked={draft.enabled} onChange={(value) => update('enabled', value)} /></Field>
    <div className="aim-form-actions"><Button onClick={onCancel} disabled={busy}>取消</Button><Button type="primary" htmlType="submit" loading={busy}>保存连接</Button></div>
  </form>;
}
const builtinManifest = JSON.stringify(builtinOcrManifest(), null, 2);
const freshModel: ModelDraft = { name: '', model_id: '', execution_location: 'server_remote', connection_id: undefined, enabled: true, capabilities: { image: false, video: false, pdf: false, streaming: false }, default_parameters: {}, manifest: builtinManifest };
export function ModelForm({ initial, connections, onSave, onCancel }: { initial?: AIModel; connections: AIConnection[]; onSave: (value: AIModelInput) => Promise<void>; onCancel: () => void }) {
  const [draft, setDraft] = useState<ModelDraft>(() => initial ? { ...initial, connection_id: initial.connection_id ?? undefined, manifest: initial.execution_location === 'browser_local' ? builtinManifest : '' } : freshModel);
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const update = <K extends keyof ModelDraft>(key: K, value: ModelDraft[K]) => setDraft((old) => ({ ...old, [key]: value }));
  const local = draft.execution_location === 'browser_local';
  const connection = connections.find((item) => item.id === draft.connection_id);
  const useGlmPreset = () => setDraft((old) => ({
    ...old, ...GLM_FLASHX_MODEL_PRESET, execution_location: 'server_remote',
    connection_id: old.connection_id ?? connections.find((item) => item.base_url === GLM_FLASHX_CONNECTION_PRESET.base_url)?.id,
    manifest: '',
  }));
  const changeLocation = (value: AIModel['execution_location']) => setDraft((old) => value === 'browser_local' ? {
    ...old, execution_location: value, name: old.name || BUILTIN_OCR_NAME, model_id: BUILTIN_OCR_MODEL_ID,
    connection_id: undefined, capabilities: { image: true, video: false, pdf: false, streaming: false }, default_parameters: {}, manifest: builtinManifest,
  } : { ...old, execution_location: value, model_id: old.model_id === BUILTIN_OCR_MODEL_ID ? '' : old.model_id, manifest: '' });
  return <form className="aim-form" onSubmit={async (event) => { event.preventDefault(); if (busy) return; setError(''); setBusy(true); try { await onSave(modelPayload(draft)); } catch (e) { setError(errorText(e)); } finally { setBusy(false); } }}>
    {error && <Alert type="error" showIcon message={error} />}
    {!initial && !local && <Button type="dashed" onClick={useGlmPreset}>使用 GLM-4.6V-FlashX 预设</Button>}
    <Field label="显示名称"><Input aria-label="显示名称" autoFocus value={draft.name} maxLength={120} onChange={(e) => update('name', e.target.value)} /></Field>
    <Field label="执行位置"><Select aria-label="执行位置" value={draft.execution_location} onChange={changeLocation} options={[{ value: 'server_remote', label: '服务端远程模型' }, { value: 'browser_local', label: '内置浏览器 OCR（免配置）' }]} /></Field>
    {local ? <>
      <Alert type="success" showIcon message="OCR 已内置，无需填写资源 JSON" description={`固定 ${BUILTIN_OCR_MODEL_ID}，资源版本 ${BUILTIN_OCR_VERSION}。生产构建会下载并校验模型，部署子路径会自动适配。`} />
      <Field label="模型标识"><Input aria-label="模型标识" value={BUILTIN_OCR_MODEL_ID} disabled /></Field>
    </> : <>
      <Field label="模型标识"><Input aria-label="模型标识" value={draft.model_id} placeholder="例如 glm-4.6v-flashx" onChange={(e) => update('model_id', e.target.value)} /></Field>
      <Field label="远程连接"><Select aria-label="远程连接" value={draft.connection_id} placeholder="选择连接" onChange={(value) => update('connection_id', value)} options={connections.map((item) => ({ value: item.id, label: `${item.name}${item.enabled ? '' : ' · 已停用'} · ${item.adapter}` }))} notFoundContent="请先在连接页签创建连接" /></Field>
      {connection && !connection.enabled && <Alert type="warning" message="此连接已停用，模型暂不可测试" />}
      <Field label="声明能力" hint="能力声明不代表已验证。协议不支持的输入仍会被阻止。">
        <div className="aim-checks">{(['image', 'video', 'pdf', 'streaming'] as const).map((key) => <Checkbox key={key} checked={draft.capabilities[key]} onChange={(e) => update('capabilities', { ...draft.capabilities, [key]: e.target.checked })}>{({ image: '图片', video: '视频', pdf: 'PDF', streaming: '流式能力' })[key]}</Checkbox>)}</div>
      </Field>
      {connection?.adapter === 'openai_chat' && (draft.capabilities.video || draft.capabilities.pdf) && <Alert type="warning" message="OpenAI Chat 协议不支持视频 / PDF，此类输入将在测试台明确禁用。" />}
      <div className="aim-form-grid"><Field label="默认温度" hint="留空由服务端 / 上游决定"><InputNumber aria-label="默认温度" min={0} max={2} step={0.1} value={draft.default_parameters.temperature} onChange={(value) => update('default_parameters', { ...draft.default_parameters, temperature: value ?? undefined })} /></Field><Field label="默认最大输出 token"><InputNumber aria-label="默认最大输出 token" min={1} max={AI_MODEL_LIMITS.maxOutputTokens} precision={0} value={draft.default_parameters.max_output_tokens} onChange={(value) => update('default_parameters', { ...draft.default_parameters, max_output_tokens: value ?? undefined })} /></Field></div>
    </>}
    <Field label="启用模型"><Switch aria-label="启用模型" checked={draft.enabled} onChange={(value) => update('enabled', value)} /></Field>
    <div className="aim-form-actions"><Button onClick={onCancel} disabled={busy}>取消</Button><Button type="primary" htmlType="submit" loading={busy}>保存模型</Button></div>
  </form>;
}
export function CredentialDialog({ connection, onSave, onClose }: { connection: AIConnection; onSave: (credential: string) => Promise<void>; onClose: () => void }) {
  const [credential, setCredential] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  return <Modal open title={`设置密钥 · ${connection.name}`} onCancel={() => !busy && onClose()} footer={null} destroyOnHidden maskClosable={!busy}>
    <form className="aim-form" onSubmit={async (e) => { e.preventDefault(); if (busy) return; if (!credential.trim()) { setError('请输入新密钥'); return; } setBusy(true); setError(''); try { await onSave(credential.trim()); setCredential(''); onClose(); } catch (err) { setError(errorText(err)); } finally { setBusy(false); } }}>
      <Alert type="warning" showIcon message="密钥仅写入，不可查看" description="保存会替换已有密钥。请勿把密钥填入服务地址、模型标识、系统提示词或资源清单。" />
      {error && <Alert type="error" message={error} />}
      <Field label="新密钥"><Input.Password aria-label="新密钥" autoComplete="new-password" value={credential} onChange={(e) => setCredential(e.target.value)} /></Field>
      <div className="aim-form-actions"><Button disabled={busy} onClick={onClose}>取消</Button><Button type="primary" htmlType="submit" loading={busy}>保存密钥</Button></div>
    </form>
  </Modal>;
}
