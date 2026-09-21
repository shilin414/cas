import { Button } from 'antd';
import { EmptyState, LoadingState } from '@/components/ProductUI';
import type { TaskSummary } from '@/types/task';
import TaskListItem from './TaskListItem';

function groupLabel(value: string): string {
  const date = new Date(value); const now = new Date(); const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime(); const day = 86400000;
  if (date.getTime() >= start) return '今天'; if (date.getTime() >= start - day) return '昨天'; if (date.getTime() >= start - 7 * day) return '本周'; return '更早';
}

export default function TaskList({ items, loading, nextCursor, onOpen, onRename, onDelete, onLoadMore }: { items: TaskSummary[]; loading: boolean; nextCursor?: string; onOpen: (task: TaskSummary) => void; onRename: (task: TaskSummary, title: string) => Promise<void>; onDelete: (task: TaskSummary) => Promise<void>; onLoadMore?: () => void; }) {
  const groups = items.reduce<Record<string, TaskSummary[]>>((all, item) => { (all[groupLabel(item.updatedAt)] ||= []).push(item); return all; }, {});
  if (loading && items.length === 0) return <LoadingState label="正在加载任务…" rows={5} />;
  if (items.length === 0) return <EmptyState title="暂无任务" description="开始使用智能体执行任务后，会在这里显示。" />;
  return <div className="task-list">{Object.entries(groups).map(([label, tasks]) => <section key={label} className="task-list__group"><h2>{label}</h2><div className="task-list__items">{tasks.map((task) => <TaskListItem key={task.id} task={task} onOpen={() => onOpen(task)} onRename={(title) => onRename(task, title)} onDelete={() => onDelete(task)} />)}</div></section>)}{nextCursor && <Button block loading={loading} onClick={onLoadMore}>加载更多</Button>}</div>;
}
