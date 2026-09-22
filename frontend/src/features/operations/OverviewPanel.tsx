import React from 'react';
import { Alert, Button, Empty, Tag } from 'antd';
import type { OperationsOverview, ProviderCapacity } from './types';

export function formatTime(value: string | null): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}
function Metric({ label, children, note, testId }: { label: string; children: React.ReactNode; note?: string; testId?: string }) {
  return <div className="ops-metric"><span>{label}</span><strong data-testid={testId}>{children}</strong>{note && <small>{note}</small>}</div>;
}
function ProviderDetails({ provider }: { provider: ProviderCapacity }) {
  return <><span>排队 {provider.queued} · 运行 {provider.running}</span><small>等待输入 {provider.waiting_input} · 等待外部 {provider.waiting_external} · 取消中 {provider.cancelling}</small><small>最早排队：{formatTime(provider.oldest_queued_at)}{provider.oldest_queued_at ? `（${provider.oldest_queued_age_seconds} 秒）` : ''}</small></>;
}
function Capacity({ provider }: { provider: ProviderCapacity }) {
  return <><strong>{provider.effective_inflight} / {provider.max_inflight}</strong><small>受控 {provider.controlled_inflight} · 无有效槽的外部执行 {provider.uncontrolled_inflight}</small></>;
}
const PROVIDER_STATUS: Record<string, string> = { active: '启用', enabled: '启用', paused: '已暂停', disabled: '已停用', degraded: '降级', missing: '配置缺失' };

export default function OverviewPanel({ snapshot, mobile, canManage, onEdit }: { snapshot: OperationsOverview; mobile: boolean; canManage: boolean; onEdit: (provider: ProviderCapacity) => void }) {
  const { providers, totals, alerts } = snapshot;
  const capacity = providers.reduce((sum, provider) => ({ effective: sum.effective + provider.effective_inflight, max: sum.max + provider.max_inflight }), { effective: 0, max: 0 });
  const edit = (provider: ProviderCapacity) => canManage ? <Button size="small" disabled={provider.status === 'missing'} title={provider.status === 'missing' ? 'Provider 配置缺失，无法修改额度' : undefined} onClick={() => onEdit(provider)}>修改额度</Button> : null;
  return <>
    <p className="ops-note">快照采样时间：{formatTime(snapshot.sampled_at)} · 手动刷新，不自动轮询</p>
    <section className="ops-metrics" aria-label="容量与积压">
      <Metric label="有效占用 / 总额度" testId="effective-capacity" note="按服务端去重后的有效占用统计；各 Provider 额度不可互借">{providers.length ? `${capacity.effective} / ${capacity.max}` : '—'}</Metric>
      <Metric label="排队任务" note="仅显示采样时的数量，不承诺队列位置或完成时间">{totals.queued}</Metric>
      <Metric label="运行中">{totals.running}</Metric>
      <Metric label="等待输入 / 外部 / 取消中">{totals.waiting_input} / {totals.waiting_external} / {totals.cancelling}</Metric>
      <Metric label="待处理调度实例">{totals.pending_occurrences}</Metric>
      <Metric label="待投递">{totals.pending_deliveries}</Metric>
      <Metric label="投递中">{totals.sending_deliveries}</Metric>
      <Metric label="待处理 Outbox">{totals.pending_outbox}</Metric>
    </section>
    <section className="ops-panel ops-stack" aria-labelledby="ops-alerts-title">
      <h3 id="ops-alerts-title">状态告警</h3>
      {alerts.length ? alerts.map((alert, index) => <Alert key={`${alert.code}-${alert.provider ?? ''}-${index}`} showIcon type={alert.severity === 'critical' ? 'error' : 'warning'} message={alert.message} description={`${alert.code}${alert.provider ? ` · ${alert.provider}` : ''}`} />) : <p className="ops-note">暂无状态告警</p>}
      <p className="ops-note">此处仅显示页面状态，不自动发送飞书通知。</p>
      {snapshot.limitations.length > 0 && <Alert showIcon type="info" message="统计边界" description={<ul>{snapshot.limitations.map((item, i) => <li key={i}>{item}</li>)}</ul>} />}
    </section>
    <section className="ops-panel ops-stack" aria-labelledby="ops-providers-title">
      <h3 id="ops-providers-title">Provider 额度</h3>
      <p className="ops-note">有效占用使用同一采样时刻的服务端去重值；无有效槽的外部执行需核对状态，不可盲目重跑。额度下调不强杀现存任务，仅影响新任务准入；提高额度不等于已经通过容量验收。</p>
      {!providers.length ? <Empty description="暂无 Provider 容量数据" /> : mobile ? <div className="ops-cards">{providers.map(provider => <article className="ops-card" key={provider.key}>
        <div className="ops-heading"><h4>{provider.name}</h4><Tag>{PROVIDER_STATUS[provider.status] ?? '未知状态'}</Tag></div><small>{provider.key}</small>
        <div className="ops-cell"><Capacity provider={provider} /><ProviderDetails provider={provider} /></div>{edit(provider)}
      </article>)}</div> : <div className="ops-table-scroll" tabIndex={0} role="region" aria-label="Provider 额度表"><table className="ops-table"><thead><tr><th scope="col">Provider</th><th scope="col">状态</th><th scope="col">有效占用 / 额度</th><th scope="col">任务与积压</th>{canManage && <th scope="col">操作</th>}</tr></thead><tbody>{providers.map(provider => <tr key={provider.key}>
        <th scope="row">{provider.name}<small>{provider.key}</small></th><td><Tag>{PROVIDER_STATUS[provider.status] ?? '未知状态'}</Tag></td><td><Capacity provider={provider} /></td><td><ProviderDetails provider={provider} /></td>{canManage && <td>{edit(provider)}</td>}
      </tr>)}</tbody></table></div>}
    </section>
  </>;
}
