import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Skeleton } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useAuthStore } from '@/stores/useAuthStore';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { operationsApi } from './operationsApi';
import { useOperationsQuery, useOperationsSession } from './useOperationsQuery';
import OverviewPanel from './OverviewPanel';
import { RunFilters, RunResults } from './RunResults';
import CapacityDialog from './CapacityDialog';
import type { ProviderCapacity, RunsQuery } from './types';
import './OperationsPage.css';

export default function OperationsPage({ mobile = false }: { mobile?: boolean }) {
  const userId = useAuthStore(state => state.user?.id);
  const identity = useAdminPermissionStore(state => state.identity);
  const loadedForUserId = useAdminPermissionStore(state => state.loadedForUserId);
  const permissionLoading = useAdminPermissionStore(state => state.loading);
  const permissionError = useAdminPermissionStore(state => state.error);
  const loadPermissions = useAdminPermissionStore(state => state.load);
  const generation = useOperationsSession();
  // Mobile direct entry need not mount the navigation drawer. Resolve the
  // current account explicitly; staff is not a replacement for a loaded grant.
  useEffect(() => {
    if (userId && loadedForUserId !== userId) void loadPermissions(userId);
  }, [userId, loadedForUserId, loadPermissions, generation]);
  // PermissionGuard elsewhere admits is_staff; this feature deliberately requires resolved permissions.
  const currentIdentity = Boolean(userId && loadedForUserId === userId);
  const has = (permission: string) => currentIdentity && Boolean(identity?.is_super_admin || identity?.permissions.some(item => item.code === permission));
  if (userId && (!currentIdentity || !identity)) {
    if (permissionError || (!permissionLoading && loadedForUserId === userId && !identity)) return <section className="ops-page"><Alert type="error" showIcon message="运行中心权限加载失败" description="尚未读取运行数据，请重新加载当前账号权限。" action={<Button onClick={() => void loadPermissions(userId, true)}>重新加载权限</Button>} /></section>;
    return <section className="ops-page"><div role="status" aria-live="polite">正在加载运行中心权限…<Skeleton active paragraph={{ rows: 2 }} /></div></section>;
  }
  if (!has('run.monitor.read')) return <section className="ops-page"><Alert type="warning" showIcon message="无运行中心访问权限" description="需要权限：run.monitor.read。请确认当前账号权限已加载。" /></section>;
  const canManage = has('provider.manage');
  return <OperationsWorkspace key={`${generation}:${userId}:${canManage}`} canManage={canManage} mobile={mobile} />;
}

function OperationsWorkspace({ canManage, mobile }: { canManage: boolean; mobile: boolean }) {
  const [overviewRevision, setOverviewRevision] = useState(0);
  const [runsRevision, setRunsRevision] = useState(0);
  const [filters, setFilters] = useState<RunsQuery>({ status: 'active', limit: 50 });
  const [cursors, setCursors] = useState<Array<string | undefined>>([undefined]);
  const [editor, setEditor] = useState<ProviderCapacity | null>(null);
  const [notice, setNotice] = useState('');
  const cursor = cursors[cursors.length - 1];
  const loadRuns = useCallback((signal: AbortSignal) => operationsApi.runs({ ...filters, ...(cursor ? { cursor } : {}) }, signal), [filters, cursor]);
  const overview = useOperationsQuery(operationsApi.overview, overviewRevision);
  const runs = useOperationsQuery(loadRuns, runsRevision);
  const refreshOverview = () => setOverviewRevision(value => value + 1);
  const refresh = () => { setNotice(''); setCursors([undefined]); refreshOverview(); setRunsRevision(value => value + 1); };
  const apply = (query: RunsQuery) => { setFilters(query); setCursors([undefined]); };
  // Fence rapid clicks against the cursor represented by this render, not a stale page number.
  const previousPage = () => setCursors(values =>
    values.length > 1 && values[values.length - 1] === cursor ? values.slice(0, -1) : values);
  const nextPage = () => {
    const next = runs.data?.next_cursor;
    if (!next || next === cursor) return;
    setCursors(values => values[values.length - 1] === cursor ? [...values, next] : values);
  };
  return <section className={`ops-page${mobile ? ' ops-page--mobile' : ''}`}>
    <header className="ops-heading"><div><h2>运行中心</h2><p>观察容量、积压和任务状态；不展示输入、输出或错误原文。</p></div><Button icon={<ReloadOutlined />} onClick={refresh}>手动刷新</Button></header>
    {notice && <Alert type="success" showIcon message={notice} />}
    {overview.loading ? <div role="status" aria-live="polite" className="ops-panel">正在加载运行快照…<Skeleton active paragraph={{ rows: 3 }} /></div> : overview.failed ? <Alert type="error" showIcon message="运行快照加载失败" description="容量、积压和告警当前不可用，不将未知数据视为 0。请手动刷新重试。" /> : overview.data && <OverviewPanel snapshot={overview.data} mobile={mobile} canManage={canManage} onEdit={setEditor} />}
    <section className="ops-panel ops-stack" aria-labelledby="ops-runs-title">
      <div><h3 id="ops-runs-title">任务</h3><p className="ops-note">按创建时间、ID 倒序的键集分页；筛选或刷新后回到第一页。列表仅含元数据，不提供取消、重试或补发操作。</p></div>
      <RunFilters onApply={apply} busy={false} />
      {runs.loading ? <div role="status" aria-live="polite">正在加载任务…<Skeleton active paragraph={{ rows: 3 }} /></div> : runs.failed ? <Alert type="error" showIcon message="任务加载失败" description="未能读取当前页，请重试。" action={<Button onClick={() => setRunsRevision(value => value + 1)}>重新加载任务</Button>} /> : runs.data && <RunResults page={runs.data} mobile={mobile} />}
      <nav className="ops-pagination" aria-label="任务分页"><Button disabled={runs.loading || cursors.length === 1} onClick={previousPage}>上一页</Button><span>第 {cursors.length} 页{runs.data ? ` · 本页 ${runs.data.results.length} 条` : ''}</span><Button disabled={runs.loading || runs.failed || !runs.data?.next_cursor} onClick={nextPage}>下一页</Button></nav>
    </section>
    {editor && canManage && <CapacityDialog provider={editor} onClose={() => setEditor(null)} onSaved={() => { setEditor(null); setNotice('额度已更新，仅对新任务准入生效。'); refreshOverview(); }} onConflictRefresh={() => { setEditor(null); setNotice(''); refreshOverview(); }} />}
  </section>;
}
