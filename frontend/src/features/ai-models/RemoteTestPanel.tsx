import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Collapse, Empty, Input, InputNumber, Modal, Popconfirm, Select, Spin, Tag } from 'antd';
import { DeleteOutlined, PaperClipOutlined, PlusOutlined, SendOutlined, StopOutlined } from '@ant-design/icons';
import { aiModelsApi, type AIConnection, type AIModel, type AIModelParameters, type Attachment, type Invocation, type TestMessageInput, type TestSession, type TestSessionDetail } from '@/services/aiModels';
import { useAuthStore } from '@/stores/useAuthStore';
import { AI_MODEL_LIMITS, REMOTE_IMAGE_MIME_TYPES, attachmentError, bytesLabel, errorText, pollInvocation, statusLabels, terminal, validateParameters } from './modelLogic';
import { Field } from './Editors';

export function AttachmentPreview({ attachment, sessionId, onDelete, disabled }: { attachment: Attachment; sessionId: string; onDelete?: () => void; disabled?: boolean }) {
  const [preview, setPreview] = useState(''); const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  const mounted = useRef(true); const objectUrl = useRef('');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; if (objectUrl.current) URL.revokeObjectURL(objectUrl.current); }; }, []);
  const close = () => { if (objectUrl.current) URL.revokeObjectURL(objectUrl.current); objectUrl.current = ''; setPreview(''); };
  return <div className="aim-attachment">
    <PaperClipOutlined /><div className="aim-attachment-name"><Button type="link" loading={busy} onClick={async () => { setError(''); setBusy(true); try { const blob = await aiModelsApi.attachment(sessionId, attachment.id); if (mounted.current) { if (objectUrl.current) URL.revokeObjectURL(objectUrl.current); objectUrl.current = URL.createObjectURL(blob); setPreview(objectUrl.current); } } catch (e) { if (mounted.current) setError(errorText(e)); } finally { if (mounted.current) setBusy(false); } }}>{attachment.name}</Button><small>{attachment.kind.toUpperCase()} · {bytesLabel(attachment.size_bytes)}</small></div>
    {onDelete && <Button type="text" danger aria-label={`移除 ${attachment.name}`} icon={<DeleteOutlined />} disabled={disabled} onClick={onDelete} />}
    {error && <span role="alert" className="aim-inline-error">{error}</span>}
    <Modal open={Boolean(preview)} title={attachment.name} footer={null} onCancel={close} width={760} destroyOnHidden>{preview && (attachment.kind === 'image' ? <img className="aim-preview" src={preview} alt={attachment.name} /> : attachment.kind === 'video' ? <video className="aim-preview" controls src={preview} /> : <><iframe className="aim-pdf-preview" title={attachment.name} src={preview} sandbox="" /><a href={preview} download={attachment.name}>下载 PDF 查看</a></>)}</Modal>
  </div>;
}

export default function RemoteTestPanel({ model, connection, onBusyChange }: { model: AIModel; connection?: AIConnection; onBusyChange: (busy: boolean) => void }) {
  const owner = useAuthStore((state) => String(state.user?.id ?? ''));
  const [sessions, setSessions] = useState<TestSession[]>([]); const [session, setSession] = useState<TestSessionDetail | null>(null);
  const [loading, setLoading] = useState(true); const [operation, setOperation] = useState(false); const lock = useRef(false);
  const [text, setText] = useState(''); const [system, setSystem] = useState(''); const [parameters, setParameters] = useState<AIModelParameters>({ ...model.default_parameters });
  const [attachments, setAttachments] = useState<Attachment[]>([]); const [error, setError] = useState('');
  const [invocation, setInvocation] = useState<Invocation | null>(null); const [invocationId, setInvocationId] = useState('');
  const [pollError, setPollError] = useState(''); const [pollRevision, setPollRevision] = useState(0); const [stopSent, setStopSent] = useState(false);
  const [pending, setPending] = useState<TestMessageInput | null>(null); const [historySynced, setHistorySynced] = useState(false);
  const [historyRefreshing, setHistoryRefreshing] = useState(false); const [historyError, setHistoryError] = useState('');
  const [cancelling, setCancelling] = useState(false); const cancelLock = useRef(false); const activeInvocationRef = useRef(false);
  const mounted = useRef(true); const selectGeneration = useRef(0);
  const historyRequest = useRef({ sequence: 0, controller: null as AbortController | null, sessionId: '', invocationId: '' });
  const remember = useCallback((sessionId: string, value: string) => {
    if (!owner) return;
    try { const key = 'aim:invocation:' + owner + ':' + sessionId; value ? sessionStorage.setItem(key, value) : sessionStorage.removeItem(key); } catch { /* Optional local hint; server remains authoritative. */ }
  }, [owner]);
  const remembered = useCallback((sessionId: string) => {
    if (!owner) return '';
    try { return sessionStorage.getItem('aim:invocation:' + owner + ':' + sessionId) || ''; } catch { return ''; }
  }, [owner]);
  const invalidateHistory = useCallback(() => {
    const history = historyRequest.current;
    history.sequence++; history.controller?.abort(); history.controller = null;
    setHistoryRefreshing(false); setHistoryError('');
  }, []);
  const setCurrentInvocation = useCallback((id: string) => {
    historyRequest.current.invocationId = id;
    // Synchronous guard closes the receipt-to-render window for a second send.
    activeInvocationRef.current = Boolean(id);
    setInvocation(null); setInvocationId(id); setStopSent(false); setHistorySynced(false);
  }, []);
  const refreshHistory = useCallback(async (sessionId: string, completed: boolean) => {
    const history = historyRequest.current;
    if (!mounted.current || history.sessionId !== sessionId) return;
    history.controller?.abort();
    const controller = new AbortController(); const sequence = ++history.sequence;
    history.controller = controller;
    const current = () => mounted.current && !controller.signal.aborted && history.sequence === sequence && history.sessionId === sessionId;
    setHistoryRefreshing(true); setHistoryError('');
    try {
      const latest = await aiModelsApi.session(sessionId, controller.signal);
      if (!current()) return;
      setSession(latest); setHistorySynced(completed);
      // Another tab may have started a new invocation since our terminal snapshot.
      if (latest.active_invocation_id && latest.active_invocation_id !== history.invocationId) {
        remember(sessionId, latest.active_invocation_id); setCurrentInvocation(latest.active_invocation_id);
      }
    } catch (e) {
      if (current()) setHistoryError('对话历史刷新失败：' + errorText(e) + '。调用状态不受影响，可单独重试刷新。');
    } finally {
      if (current()) { history.controller = null; setHistoryRefreshing(false); }
    }
  }, [remember, setCurrentInvocation]);
  const active = Boolean(invocationId && (!invocation || !terminal(invocation.status)));
  const busy = operation || active || Boolean(pending) || cancelling;
  const unavailable = !model.enabled ? '此模型已停用，无法发起调用' : !connection ? '未找到远程连接' : !connection.enabled ? '远程连接已停用' : !connection.has_credential ? '远程连接尚未设置密钥' : '';
  const sessionId = session?.id;
  useEffect(() => { onBusyChange(busy); return () => onBusyChange(false); }, [busy, onBusyChange]);
  useEffect(() => {
    mounted.current = true; const history = historyRequest.current;
    return () => { mounted.current = false; history.sequence++; history.controller?.abort(); };
  }, []);
  useEffect(() => {
    const abort = new AbortController(); setLoading(true);
    aiModelsApi.sessions(abort.signal).then((items) => { if (!abort.signal.aborted) setSessions(items.filter((item) => item.model_id === model.id)); }).catch((e) => { if (!abort.signal.aborted) setError(errorText(e)); }).finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [model.id]);
  useEffect(() => {
    if (!invocationId || !sessionId) return;
    const abort = new AbortController();
    setPollError('');
    pollInvocation((signal) => aiModelsApi.invocation(invocationId, signal), (value) => {
      activeInvocationRef.current = !terminal(value.status); setInvocation(value);
    }, abort.signal).then(() => {
      if (abort.signal.aborted || historyRequest.current.invocationId !== invocationId) return;
      remember(sessionId, '');
      // History is a separate, ordered read: it never owns send/cancel locks.
      void refreshHistory(sessionId, true);
    }).catch((e) => { if (!abort.signal.aborted) setPollError('状态查询中断：' + errorText(e) + '。这不代表上游已停止，请重新查询。'); });
    return () => abort.abort();
  }, [invocationId, sessionId, pollRevision, refreshHistory, remember]);
  const run = async (action: () => Promise<void>) => {
    if (lock.current) return; lock.current = true; setOperation(true); setError('');
    try { await action(); } catch (e) { if (mounted.current) setError(errorText(e)); } finally { lock.current = false; if (mounted.current) setOperation(false); }
  };
  const selectSession = async (key: string) => {
    if (lock.current || activeInvocationRef.current || cancelLock.current || pending) return;
    const generation = ++selectGeneration.current;
    invalidateHistory();
    await run(async () => {
      const detail = await aiModelsApi.session(key);
      if (!mounted.current || generation !== selectGeneration.current) return;
      historyRequest.current.sessionId = key;
      setSession(detail); setText(''); setAttachments([]); setPollError(''); setPending(null);
      // An explicit empty server value clears a stale hint; fallback supports older servers.
      const restored = detail.active_invocation_id ?? remembered(key);
      remember(key, restored); setCurrentInvocation(restored);
    });
  };
  const newSession = () => run(async () => {
    if (activeInvocationRef.current || cancelLock.current || pending) return;
    invalidateHistory();
    const created = await aiModelsApi.createSession(model.id, model.name + ' · 测试');
    if (!mounted.current) return;
    historyRequest.current.sessionId = created.id;
    setSessions((old) => [created, ...old]); setSession({ ...created, messages: [] }); setText(''); setAttachments([]); setCurrentInvocation(''); setPending(null); setPollError('');
  });
  const send = () => run(async () => {
    if (!session || unavailable || activeInvocationRef.current || cancelLock.current) return;
    if (!pending && !text.trim() && !attachments.length) throw new Error('请输入消息或添加支持的附件');
    const request = pending || { text: text.trim(), attachment_ids: attachments.map((item) => item.id), system_prompt: system, parameters: validateParameters(parameters), request_id: crypto.randomUUID() };
    invalidateHistory(); setPending(request);
    try {
      const response = await aiModelsApi.send(session.id, request);
      remember(session.id, response.invocation_id);
      if (!mounted.current) return;
      setCurrentInvocation(response.invocation_id); setPending(null); setText(''); setAttachments([]);
      // Fire-and-forget with its own sequence/error state. Release operation on receipt.
      void refreshHistory(session.id, false);
    } catch (e) {
      const status = (e as { response?: { status?: number } }).response?.status;
      if (mounted.current && status && status >= 400 && status < 500 && status !== 408) setPending(null);
      throw e;
    }
  });
  const cancel = async () => {
    if (!invocationId || !activeInvocationRef.current || cancelLock.current || stopSent || invocation?.cancel_requested) return;
    cancelLock.current = true; setCancelling(true); setError('');
    const target = invocationId;
    try {
      await aiModelsApi.cancel(target);
      if (mounted.current && historyRequest.current.invocationId === target) { setStopSent(true); setPollRevision((value) => value + 1); }
    } catch (e) {
      if (mounted.current && historyRequest.current.invocationId === target) setError(errorText(e));
    } finally {
      cancelLock.current = false;
      if (mounted.current) setCancelling(false);
    }
  };
  const upload = (files: File[]) => run(async () => {
    if (!session || unavailable || busy) return;
    let count = attachments.length;
    // Validate the whole selection before performing any network upload.
    for (const file of files) { const reason = attachmentError(model, connection?.adapter, file, count++); if (reason) throw new Error(`${file.name}：${reason}`); }
    for (const file of files) {
      const attachment = await aiModelsApi.upload(session.id, file);
      if (!mounted.current) return;
      setAttachments((old) => [...old, attachment]);
    }
  });
  const disabledSwitch = busy || attachments.length > 0 || Boolean(text.trim());
  const supported = [model.capabilities.image && '图片', model.capabilities.video && connection?.adapter === 'gemini' && '视频', model.capabilities.pdf && connection?.adapter === 'gemini' && 'PDF'].filter(Boolean);
  return <div className="aim-chat-workspace">
    <div className="aim-chat-toolbar"><div><strong>我的测试会话</strong><small>正文与附件仅本人可见，到期后不可访问</small></div><Button icon={<PlusOutlined />} onClick={newSession} disabled={disabledSwitch || Boolean(unavailable)} loading={operation}>新建会话</Button></div>
    <div className="aim-session-picker"><Select aria-label="我的测试会话" value={session?.id} placeholder={loading ? '正在加载会话…' : '选择历史会话或新建'} disabled={disabledSwitch || loading} options={sessions.map((item) => ({ value: item.id, label: `${item.title || '未命名会话'} · ${new Date(item.created_at).toLocaleString()}` }))} onChange={selectSession} notFoundContent="暂无本人会话" />{session && <Popconfirm title="删除此测试会话及其数据？" description="此操作不可撤销。" onConfirm={() => run(async () => { if (activeInvocationRef.current || cancelLock.current) return; await aiModelsApi.deleteSession(session.id); remember(session.id, ''); if (!mounted.current) return; setSessions((old) => old.filter((item) => item.id !== session.id)); invalidateHistory(); historyRequest.current.sessionId = ''; setSession(null); setAttachments([]); setText(''); setCurrentInvocation(''); })}><Button aria-label="删除当前会话" danger icon={<DeleteOutlined />} disabled={busy} /></Popconfirm>}</div>
    {unavailable && <Alert type="warning" showIcon message={unavailable} />}
    {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}
    {pending && !operation && <Alert type="warning" showIcon message="提交结果尚未确认" description="不要另发一条消息。点击“确认同一次提交”将使用原始内容及相同幂等标识确认，不会自动生成新的调用。" />}
    {pollError && <Alert type="error" showIcon message={pollError} action={<Button size="small" onClick={() => setPollRevision((value) => value + 1)}>重新查询</Button>} />}
    {historyRefreshing && <div role="status" className="aim-footnote">正在刷新对话记录…</div>}
    {historyError && <Alert type="error" showIcon message={historyError} action={<Button size="small" onClick={() => session && void refreshHistory(session.id, Boolean(invocation && terminal(invocation.status)))}>刷新对话</Button>} />}
    <Collapse className="aim-settings" items={[{ key: 'parameters', label: '系统提示词与调用参数', children: <div className="aim-form"><Field label="系统提示词"><Input.TextArea aria-label="系统提示词" disabled={busy} value={system} onChange={(e) => setSystem(e.target.value)} autoSize={{ minRows: 2, maxRows: 6 }} placeholder="可选；将在远程请求中发送，请勿包含密钥" /></Field><div className="aim-form-grid"><Field label="温度"><InputNumber aria-label="温度" disabled={busy} min={0} max={2} step={0.1} value={parameters.temperature} onChange={(value) => setParameters((old) => ({ ...old, temperature: value ?? undefined }))} /></Field><Field label="最大输出 token"><InputNumber aria-label="最大输出 token" disabled={busy} min={1} max={AI_MODEL_LIMITS.maxOutputTokens} precision={0} value={parameters.max_output_tokens} onChange={(value) => setParameters((old) => ({ ...old, max_output_tokens: value ?? undefined }))} /></Field></div></div> }]} />
    <div className="aim-transcript" aria-label="本人测试对话" aria-live="polite">
      {loading ? <Spin /> : !session ? <Empty description="新建会话，开始真实远程多轮测试" /> : session.messages.length === 0 && !invocationId ? <Empty description="会话已创建，发送第一条消息" /> : session.messages.map((item) => <article key={item.id} className={`aim-message aim-message--${item.role}`}><div className="aim-message-label">{item.role === 'user' ? '你' : item.role === 'assistant' ? model.name : '系统'}<time>{new Date(item.created_at).toLocaleTimeString()}</time></div><div className="aim-message-text">{item.text}</div>{item.attachments?.map((attachment) => <AttachmentPreview key={attachment.id} attachment={attachment} sessionId={session.id} />)}</article>)}
      {invocationId && <article className="aim-invocation"><div className="aim-message-label"><span>服务端调用快照</span><Tag color={invocation?.status === 'failed' ? 'error' : invocation?.status === 'succeeded' ? 'success' : 'processing'}>{invocation ? statusLabels[invocation.status] : '查询中'}</Tag></div>
        <small>轮询服务端快照，不模拟逐字输出 · {invocationId}</small>
        {invocation?.output_text && !(invocation.status === 'succeeded' && historySynced && session?.messages.some((message) => message.role === 'assistant' && message.text === invocation.output_text)) && <div className="aim-message-text">{invocation.output_text}</div>}
        {invocation?.error_message && <Alert type="error" message={invocation.error_code || '调用错误'} description={invocation.error_message} />}
        {invocation?.status === 'failed' && !invocation.error_message && <Alert type="error" message={invocation.error_code || '远程调用失败，未返回详细错误'} />}
        {invocation?.status === 'indeterminate' && <Alert type="warning" message="上游结果不确定" description="请求可能已经执行或计费。请核查服务商记录，不要盲目重试。" />}
        {(stopSent || invocation?.cancel_requested) && <Alert type={invocation?.cancellation_confirmed ? 'success' : 'warning'} message={invocation?.cancellation_confirmed ? '服务端已确认取消' : '停止请求已发送，上游是否停止及是否计费仍待确认'} />}
        {invocation && <div className="aim-inline-meta"><span>耗时 {invocation.duration_ms != null ? `${(invocation.duration_ms / 1000).toFixed(2)}s` : '—'}</span><span>输入 {invocation.input_tokens ?? '—'} tokens</span><span>输出 {invocation.output_tokens ?? '—'} tokens</span></div>}
      </article>}
    </div>
    <div className="aim-composer">
      {attachments.length > 0 && <div className="aim-attachments">{attachments.map((item) => <AttachmentPreview key={item.id} attachment={item} sessionId={session!.id} disabled={busy} onDelete={() => run(async () => { await aiModelsApi.deleteAttachment(session!.id, item.id); if (mounted.current) setAttachments((old) => old.filter((a) => a.id !== item.id)); })} />)}</div>}
      <Input.TextArea aria-label="测试消息" value={text} disabled={!session || busy || Boolean(unavailable)} onChange={(e) => setText(e.target.value)} autoSize={{ minRows: 3, maxRows: 10 }} placeholder={session ? '输入测试消息；对话历史由服务端保存并用于多轮上下文' : '请先新建或选择会话'} />
      <div className="aim-composer-actions"><label className={`aim-upload ${!session || busy || unavailable || !supported.length ? 'is-disabled' : ''}`}><PaperClipOutlined /> 添加附件<input aria-label="添加测试附件" type="file" multiple disabled={!session || busy || Boolean(unavailable) || !supported.length} accept={[model.capabilities.image ? REMOTE_IMAGE_MIME_TYPES.join(',') : '', model.capabilities.video && connection?.adapter === 'gemini' ? 'video/mp4,video/webm,video/quicktime' : '', model.capabilities.pdf && connection?.adapter === 'gemini' ? 'application/pdf' : ''].filter(Boolean).join(',')} onChange={(e) => { const files = Array.from(e.target.files || []); e.target.value = ''; void upload(files); }} /></label>
        {active ? <Button danger icon={<StopOutlined />} disabled={cancelling || stopSent || invocation?.cancel_requested} loading={cancelling} onClick={cancel}>请求停止</Button> : <Button type="primary" icon={<SendOutlined />} onClick={send} loading={operation} disabled={!session || operation || cancelling || Boolean(unavailable) || (!pending && !text.trim() && !attachments.length)}>{pending ? '确认同一次提交' : '发送消息'}</Button>}
      </div>
      <p className="aim-footnote">支持文字{supported.length ? `、${supported.join('、')}` : ''}。图片仅支持 JPEG / PNG / WebP，不支持 GIF。其他不支持的输入不会上传。每轮最多 4 个文件，每个 20 MB。附件会发送至服务端及模型供应商。</p>
      {session && <p className="aim-footnote">会话到期：{new Date(session.expires_at).toLocaleString()}。离开页面不会自动停止上游调用。切换会话前请先发送或清除草稿与附件。</p>}
    </div>
  </div>;
}
