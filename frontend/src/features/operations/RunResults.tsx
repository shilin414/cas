import React, { useState } from 'react';
import { Alert, Button, Empty, Input, Tag } from 'antd';
import { buildRunsParams } from './operationsApi';
import { formatTime } from './OverviewPanel';
import { RUN_STATUSES, type RunStatusFilter, type OperationRun, type RunsPage, type RunsQuery } from './types';

export const STATUS_LABELS: Record<string, string> = { active: '活跃任务', all: '全部任务', queued: '排队中', running: '运行中', waiting_input: '等待输入', waiting_external: '等待外部', cancelling: '取消中', cancelled: '已取消', succeeded: '已成功', failed: '已失败', interrupted: '已中断' };
const WAIT_LABELS: Record<string, string> = { deferred: '已延后准入', provider_paused: 'Provider 已暂停', provider_unavailable: 'Provider 不可用', queued: '排队中' };
function RunState({ run }: { run: OperationRun }) {
  return <><Tag color={run.status === 'failed' || run.status === 'interrupted' ? 'error' : run.status === 'succeeded' ? 'success' : 'default'}>{STATUS_LABELS[run.status] ?? '未知状态'}</Tag>
    <small>{WAIT_LABELS[run.wait_reason] ?? (run.wait_reason ? '未提供可验证等待原因' : '—')}</small>
    <small>排队时长：{run.queue_age_seconds === null ? '—' : `${run.queue_age_seconds} 秒`}</small></>;
}
function RunIdentity({ run }: { run: OperationRun }) {
  return <><span>用户：{run.owner_user_id ?? '—'}</span><small>应用：{run.application_id ?? '—'}</small><small>会话：{run.conversation_id ?? '—'}</small></>;
}
function RunMetadata({ run }: { run: OperationRun }) {
  // Only metadata fields are rendered. Never stringify a run or show error/message bodies.
  const code = /^[A-Za-z0-9_.:-]{1,100}$/.test(run.error_code) ? run.error_code : '—';
  return <><span>{run.runtime_type} · {run.trigger_type} · {run.priority}</span><small>尝试 {run.attempt} / {run.max_attempts}</small><small>错误代码：{code}</small></>;
}
function RunTimes({ run }: { run: OperationRun }) {
  return <><span>创建：{formatTime(run.created_at)}</span><small>排队：{formatTime(run.queued_at)}</small><small>可执行：{formatTime(run.available_at)}</small><small>开始：{formatTime(run.started_at)}</small><small>完成：{formatTime(run.finished_at)}</small></>;
}

export function RunFilters({ onApply, busy }: { onApply: (query: RunsQuery) => void; busy: boolean }) {
  const [status, setStatus] = useState<RunStatusFilter>('active');
  const [provider, setProvider] = useState('');
  const [owner, setOwner] = useState('');
  const [application, setApplication] = useState('');
  const [limit, setLimit] = useState(50);
  const [error, setError] = useState('');
  const apply = (event: React.FormEvent) => {
    event.preventDefault();
    try { const query = buildRunsParams({ status, provider: provider.trim(), owner_user_id: owner.trim(), application_id: application.trim(), limit }); setError(''); onApply(query); }
    catch (e) { setError((e as Error).message); }
  };
  const reset = () => { setStatus('active'); setProvider(''); setOwner(''); setApplication(''); setLimit(50); setError(''); onApply({ status: 'active', limit: 50 }); };
  return <form className="ops-stack" onSubmit={apply} noValidate>
    <div className="ops-filters">
      <label className="ops-field">任务状态<select aria-label="任务状态" value={status} onChange={e => setStatus(e.target.value as RunStatusFilter)}>{RUN_STATUSES.map(value => <option key={value} value={value}>{STATUS_LABELS[value]}</option>)}</select></label>
      <label className="ops-field">Provider<Input aria-label="Provider 筛选" placeholder="Provider key" value={provider} onChange={e => setProvider(e.target.value)} /></label>
      <label className="ops-field">所属用户 ID<Input aria-label="所属用户 ID" inputMode="numeric" value={owner} onChange={e => setOwner(e.target.value)} placeholder="十进制正整数字符串" /></label>
      <label className="ops-field">应用 ID<Input aria-label="应用 ID" inputMode="numeric" value={application} onChange={e => setApplication(e.target.value)} placeholder="十进制正整数字符串" /></label>
      <label className="ops-field">每页条数<select aria-label="每页条数" value={limit} onChange={e => setLimit(Number(e.target.value))}>{[10, 25, 50, 100].map(value => <option key={value} value={value}>{value}</option>)}</select></label>
    </div>
    {error && <Alert type="error" showIcon message={error} />}
    <div className="ops-actions"><Button htmlType="submit" type="primary" disabled={busy}>应用筛选</Button><Button disabled={busy} onClick={reset}>重置筛选</Button></div>
  </form>;
}

export function RunResults({ page, mobile }: { page: RunsPage; mobile: boolean }) {
  if (!page.results.length) return <Empty description="暂无匹配任务" />;
  if (mobile) return <div className="ops-cards" data-testid="run-cards">{page.results.map(run => <article className="ops-card ops-stack" key={run.id}>
    <h4>任务 {run.id}</h4><div className="ops-cell"><span>Provider：{run.provider}</span><RunState run={run} /></div>
    <div className="ops-cell"><RunIdentity run={run} /></div><div className="ops-cell"><RunMetadata run={run} /></div><div className="ops-cell"><RunTimes run={run} /></div>
  </article>)}</div>;
  return <div className="ops-table-scroll" tabIndex={0} role="region" aria-label="任务表"><table className="ops-table ops-runs-table"><thead><tr><th scope="col">任务 / Provider</th><th scope="col">状态 / 等待事实</th><th scope="col">关联 ID</th><th scope="col">执行元数据</th><th scope="col">时间</th></tr></thead><tbody>{page.results.map(run => <tr key={run.id}>
    <th scope="row">{run.id}<small>{run.provider}</small></th><td><RunState run={run} /></td><td><RunIdentity run={run} /></td><td><RunMetadata run={run} /></td><td><RunTimes run={run} /></td>
  </tr>)}</tbody></table></div>;
}
