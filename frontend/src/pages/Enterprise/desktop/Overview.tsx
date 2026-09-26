import React, { useEffect, useState } from 'react';
import { Alert } from 'antd';
import { MetricCard, PageHeader } from '@/components/ProductUI';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { enterpriseApi, type DirectoryStats, type SyncRun } from '../enterpriseApi';

export default function Overview() {
  const identity = useAdminPermissionStore((state) => state.identity);
  const canDirectory = useAdminPermissionStore((state) => state.can('directory.read'));
  const canSync = useAdminPermissionStore((state) => state.can('directory.sync.read'));
  const [stats, setStats] = useState<DirectoryStats | null>(null);
  const [runs, setRuns] = useState<SyncRun[]>([]);
  const [error, setError] = useState(false);

  useEffect(() => {
    setError(false);
    const tasks: Promise<unknown>[] = [];
    if (canDirectory) tasks.push(enterpriseApi.stats().then(setStats));
    else setStats(null);
    if (canSync) tasks.push(enterpriseApi.syncRuns(1).then(setRuns));
    else setRuns([]);
    void Promise.all(tasks).catch(() => setError(true));
  }, [canDirectory, canSync]);

  const match = stats?.oauth_users
    ? Math.round((stats.linked_directory_users * 100) / stats.oauth_users)
    : 0;

  return (
    <section className="enterprise-section">
      <PageHeader title="企业控制台" description="统一管理资源、企业目录与访问权限" />
      {error && <Alert type="warning" showIcon message="部分概览数据暂时无法加载" />}
      {!canDirectory && !canSync && (
        <Alert type="info" showIcon message="当前角色可进入企业控制台；概览指标会按权限隐藏。" />
      )}
      <div className="enterprise-metric-grid">
        {canDirectory && (
          <>
            <MetricCard
              label="部门（有效 / 总数）"
              value={stats ? `${stats.departments_active} / ${stats.departments_total}` : '—'}
            />
            <MetricCard
              label="员工（有效 / 总数）"
              value={stats ? `${stats.users_active} / ${stats.users_total}` : '—'}
            />
            <MetricCard
              label="OAuth 关联"
              value={stats ? `${stats.linked_directory_users} / ${stats.oauth_users}` : '—'}
              hint={stats ? `匹配率 ${match}%` : undefined}
            />
          </>
        )}
        {canSync && <MetricCard label="最近同步" value={runs[0]?.status || '未执行'} />}
      </div>
      {stats && stats.users_resigned > 0 && (
        <Alert
          type="info"
          showIcon
          message={`目录中有 ${stats.users_resigned} 名离职员工，ACL 已自动排除。`}
        />
      )}
    </section>
  );
}
