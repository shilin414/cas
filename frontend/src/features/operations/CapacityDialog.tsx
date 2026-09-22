import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Modal } from 'antd';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { captureSessionGeneration, sessionStillCurrent } from '@/stores/resetSessionState';
import { operationsApi, validateCapacity } from './operationsApi';
import type { CapacityChange, ProviderCapacity } from './types';

export default function CapacityDialog({ provider, onClose, onSaved, onConflictRefresh }: { provider: ProviderCapacity; onClose: () => void; onSaved: () => void; onConflictRefresh: () => void }) {
  const [capacity, setCapacity] = useState(String(provider.max_inflight));
  const [reason, setReason] = useState('');
  const [confirmedChange, setConfirmedChange] = useState<CapacityChange | null>(null);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState('');
  const [conflict, setConflict] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const active = useRef(true);
  useEffect(() => { active.current = true; return () => { active.current = false; pending.current?.abort(); }; }, []);
  const prepare = (event: React.FormEvent) => {
    event.preventDefault();
    const change = { max_inflight: Number(capacity), expected_max_inflight: provider.max_inflight, reason: reason.trim() };
    try { validateCapacity(change); setConfirmedChange(change); setError(''); } catch (e) { setError((e as Error).message); }
  };
  const save = async () => {
    if (!confirmedChange || pending.current || conflict) return;
    if (!useAdminPermissionStore.getState().has('provider.manage') || !useAdminPermissionStore.getState().has('run.monitor.read')) { setError('权限已变化，请重新进入运行中心'); return; }
    const generation = captureSessionGeneration();
    const controller = new AbortController(); pending.current = controller;
    const current = () => active.current && !controller.signal.aborted && sessionStillCurrent(generation);
    setWorking(true); setError('');
    try {
      await operationsApi.updateCapacity(provider.key, confirmedChange, controller.signal);
      if (current()) onSaved();
    } catch (e) {
      if (!current()) return;
      const status = (e as { response?: { status?: number } })?.response?.status;
      if (status === 409) { setConflict(true); setError('额度已被其他管理员修改（409），请刷新最新额度后重新确认。'); }
      else setError(status === 403 ? '无修改权限，请刷新权限后重试。' : '额度修改失败，请刷新核对实际额度后重试。');
    } finally {
      if (current()) { pending.current = null; setWorking(false); }
    }
  };
  return <Modal open title={`修改额度 · ${provider.name}`} onCancel={onClose} footer={null} closable={!working} maskClosable={false} keyboard={!working}>
    <div className="ops-stack">
      <Alert type="warning" showIcon message="仅影响新任务准入" description="额度下调不强杀现存任务；提高额度不等于已经通过容量验收。" />
      {error && <Alert type="error" showIcon message={error} />}
      {!confirmedChange ? <form className="ops-stack" onSubmit={prepare} noValidate>
        <p>快照额度：{provider.max_inflight} · 有效占用：{provider.effective_inflight}</p>
        <label className="ops-field">新额度（1–10000）<Input aria-label="新额度" inputMode="numeric" value={capacity} onChange={event => setCapacity(event.target.value)} /></label>
        <label className="ops-field">修改原因（必填，最多 500 字符）<Input.TextArea aria-label="修改原因" value={reason} rows={3} onChange={event => setReason(event.target.value)} /></label>
        <div className="ops-actions"><Button onClick={onClose}>取消</Button><Button type="primary" htmlType="submit">继续确认</Button></div>
      </form> : <>
        <p>确认将 {provider.name} 的额度从 <strong>{provider.max_inflight}</strong> 调整为 <strong>{confirmedChange.max_inflight}</strong>？</p>
        <p className="ops-reason">修改原因：{confirmedChange.reason}</p>
        {conflict && <Button onClick={onConflictRefresh}>刷新最新额度</Button>}
        <div className="ops-actions"><Button onClick={onClose} disabled={working}>取消</Button><Button disabled={working || conflict} onClick={() => { setConfirmedChange(null); setError(''); }}>返回编辑</Button><Button type="primary" loading={working} disabled={working || conflict} onClick={() => void save()}>确认修改</Button></div>
      </>}
    </div>
  </Modal>;
}
