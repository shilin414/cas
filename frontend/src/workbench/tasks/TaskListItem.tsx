import type { TaskSummary } from '@/types/task';
import RecentNavigationIcon from '@/components/Navigation/RecentNavigationIcon';
import TaskMenu from './TaskMenu';
import './tasks.css';

function relativeTime(value: string): string { return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value)); }

export default function TaskListItem({ task, compact = false, onOpen, onRename, onDelete }: { task: TaskSummary; compact?: boolean; onOpen: () => void; onRename: (title: string) => Promise<void>; onDelete: () => Promise<void>; }) {
  return <div className={`task-list-item${compact ? ' task-list-item--compact' : ''}`}>
    <button type="button" className="task-list-item__open" onClick={onOpen}>
      {compact ? <RecentNavigationIcon task /> : <span className={`task-list-item__state task-list-item__state--${task.executionState}`} aria-label={task.executionState} />}
      <span className="task-list-item__body"><strong>{task.title || '未命名任务'}</strong><small>{task.applicationName || '默认智能体'}{compact && task.executionState !== 'idle' ? ` · ${task.executionState === 'running' ? '运行中' : '执行失败'}` : ''}{!compact && task.preview ? ` · ${task.preview}` : ''}</small></span>
      <time dateTime={task.updatedAt}>{relativeTime(task.updatedAt)}</time>
    </button>
    {!compact && <TaskMenu task={task} onRename={onRename} onDelete={onDelete} />}
  </div>;
}
