import { useNavigate } from 'react-router-dom';
import type { TaskSummary } from '@/types/task';
import { useTaskStore } from '@/stores/useTaskStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';
import TaskListItem from './TaskListItem';

/** 最近任务的详情路径（Architecture 2.0 §25：PUSH 语义）。 */
export function taskPath(task: TaskSummary): string {
  return task.applicationSlug
    ? `/chat/${task.applicationSlug}?conversation=${task.id}`
    : `/?conversation=${task.id}`;
}

export default function RecentTaskList({ items, compact = true, limit = 6, onNavigate }: {
  items: TaskSummary[];
  compact?: boolean;
  limit?: number;
  /**
   * 导航回调（审查 M-2）：传入时接管导航 —— 如移动抽屉先关抽屉、
   * afterOpenChange(false) 后再 pushPage 的延迟时序；缺省时本组件
   * 直接 PUSH（带 appNavigation state，供详情页 POP 返回）。
   */
  onNavigate?: (path: string) => void;
}) {
  const navigate = useNavigate();
  const rename = useTaskStore((state) => state.rename);
  const remove = useTaskStore((state) => state.remove);
  const refreshBootstrap = useWorkspaceBootstrapStore((state) => state.load);
  const open = (task: TaskSummary) => {
    const path = taskPath(task);
    if (onNavigate) { onNavigate(path); return; }
    navigate(path, { state: { appNavigation: { type: 'push' as const } } });
  };
  return (
    <div className="recent-task-list">
      {items.slice(0, limit).map((task) => (
        <TaskListItem key={task.id} task={task} compact={compact} onOpen={() => open(task)}
          onRename={async (title) => { await rename(task.id, title); await refreshBootstrap(true); }} onDelete={async () => { await remove(task.id); await refreshBootstrap(true); }} />
      ))}
    </div>
  );
}
