import { useEffect, useMemo, useState } from 'react';
import { Select } from 'antd';
import { useNavigate } from 'react-router-dom';
import { ErrorState, PageHeader, PageSurface, PageToolbar, SearchField } from '@/components/ProductUI';
import { useTaskStore } from '@/stores/useTaskStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import type { TaskSummary } from '@/types/task';
import TaskList from './TaskList';
import './tasks.css';

export default function TaskCenterPage() {
  const navigate = useNavigate();
  const { items, loading, error, nextCursor, load, loadMore, rename, remove } = useTaskStore();
  const capabilities = useWorkspaceBootstrapStore((state) => state.recentCapabilities);
  const loadBootstrap = useWorkspaceBootstrapStore((state) => state.load);
  const [q, setQ] = useState('');
  const [applicationId, setApplicationId] = useState<number | undefined>();

  useEffect(() => { void loadBootstrap(); }, [loadBootstrap]);
  useEffect(() => {
    const timer = window.setTimeout(() => { void load({ q, applicationId, limit: 20 }); }, 220);
    return () => window.clearTimeout(timer);
  }, [applicationId, load, q]);

  const options = useMemo(() => capabilities.map((item) => ({ value: item.id, label: item.name })), [capabilities]);
  const open = (task: TaskSummary) => navigate(task.applicationSlug ? `/chat/${task.applicationSlug}?conversation=${task.id}` : `/?conversation=${task.id}`);

  return (
    <PageSurface width="default" padded className="task-center">
      <PageHeader title="任务" description="继续、搜索和管理你的工作。" />
      <PageToolbar
        search={<SearchField value={q} onChange={setQ} onClear={() => setQ('')} placeholder="搜索任务" />}
        filters={<Select allowClear placeholder="全部能力" options={options} value={applicationId} onChange={setApplicationId} className="task-center__capability-filter" />}
      />
      {error && <ErrorState compact title="任务加载失败" description={error} />}
      <TaskList items={items} loading={loading} nextCursor={nextCursor} onOpen={open} onRename={(task, title) => rename(task.id, title)} onDelete={(task) => remove(task.id)} onLoadMore={loadMore} />
    </PageSurface>
  );
}
