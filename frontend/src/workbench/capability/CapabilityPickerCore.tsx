import { useEffect, useMemo, useState } from 'react';
import AgentAvatar from '@/components/Agents/AgentAvatar';
import { EmptyState, EntityRow, LoadingState, SearchField, SegmentedTabs, StatusBadge } from '@/components/ProductUI';
import { fetchApplicationPage, type ApplicationSummary } from '@/services/runApi';
import { fetchTasks } from '@/services/taskApi';
import type { TaskSummary } from '@/types/task';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';

export type CapabilityFilter = 'all' | 'agents' | 'apps';
interface Props { onSelect: (capability: ApplicationSummary) => void; onTaskSelect: (task: TaskSummary) => void; }

const PAGE_SIZE = 50;

function mergeCapabilities(...groups: ApplicationSummary[][]): ApplicationSummary[] {
  const byId = new Map<number, ApplicationSummary>();
  groups.flat().forEach((item) => byId.set(item.id, item));
  return Array.from(byId.values());
}

function fetchCapabilities(query: string) {
  const common = { scope: 'accessible' as const, mode: 'consume' as const, q: query, limit: PAGE_SIZE };
  return Promise.all([
    fetchApplicationPage({ ...common, kind: 'chat' }),
    fetchApplicationPage({ ...common, kind: 'fixed' }),
  ]).then(([agents, apps]) => mergeCapabilities(agents.items, apps.items));
}

export default function CapabilityPickerCore({ onSelect, onTaskSelect }: Props) {
  const recentCapabilities = useWorkspaceBootstrapStore((state) => state.recentCapabilities);
  const [filter, setFilter] = useState<CapabilityFilter>('all');
  const [query, setQuery] = useState('');
  const [catalogItems, setCatalogItems] = useState<ApplicationSummary[]>(recentCapabilities);
  const [searchItems, setSearchItems] = useState<ApplicationSummary[]>([]);
  const [tasks, setTasks] = useState<TaskSummary[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [searchLoading, setSearchLoading] = useState(false);
  const normalizedQuery = query.trim();

  // Load both bounded groups once. Tabs then switch locally instead of paying a
  // debounce + network round-trip every time the user clicks 智能体 / 应用.
  useEffect(() => {
    let active = true;
    setCatalogLoading(true);
    void fetchCapabilities('').then((items) => {
      if (active) setCatalogItems(items);
    }).catch(() => {
      // Keep bootstrap recents as a usable fallback when the catalog request fails.
    }).finally(() => {
      if (active) setCatalogLoading(false);
    });
    return () => { active = false; };
    // The catalog is a mount-scoped snapshot. Bootstrap updates must not restart
    // the same two requests while the picker is open.
  }, []);

  // Only typed search is debounced. It searches both groups together so tab
  // switches remain instant even while a keyword is active.
  useEffect(() => {
    if (!normalizedQuery) {
      setSearchItems([]);
      setTasks([]);
      setSearchLoading(false);
      return undefined;
    }

    let active = true;
    setSearchItems([]);
    setTasks([]);
    setSearchLoading(true);
    const timer = window.setTimeout(() => {
      void Promise.all([
        fetchCapabilities(normalizedQuery),
        fetchTasks({ q: normalizedQuery, limit: 8 }),
      ]).then(([items, taskPage]) => {
        if (!active) return;
        setSearchItems(items);
        setTasks(taskPage.items);
      }).catch(() => {
        if (!active) return;
        setSearchItems([]);
        setTasks([]);
      }).finally(() => {
        if (active) setSearchLoading(false);
      });
    }, 180);

    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [normalizedQuery]);

  const items = normalizedQuery ? searchItems : catalogItems;
  const loading = normalizedQuery ? searchLoading : catalogLoading;
  const shown = useMemo(() => (
    filter === 'agents'
      ? items.filter((item) => item.kind === 'chat')
      : filter === 'apps'
        ? items.filter((item) => item.kind !== 'chat')
        : items
  ), [filter, items]);

  return <div className="capability-picker">
    <SearchField value={query} onChange={setQuery} onClear={() => setQuery('')} placeholder="搜索智能体、应用或任务" />
    <SegmentedTabs value={filter} onChange={(value) => setFilter(value as CapabilityFilter)} options={[{ value: 'all', label: '全部' }, { value: 'agents', label: '智能体' }, { value: 'apps', label: '应用' }]} />
    {tasks.length > 0 && <section className="capability-picker__tasks"><h2>任务</h2>{tasks.map((task) => <EntityRow key={task.id} title={task.title || '未命名任务'} description={task.applicationName || '默认智能体'} onClick={() => onTaskSelect(task)} />)}</section>}
    <div className="capability-picker__list" aria-label="能力列表" aria-busy={loading}>
      {loading && shown.length === 0
        ? <LoadingState compact rows={4} label={normalizedQuery ? '正在搜索…' : '正在加载能力…'} />
        : shown.length === 0
          ? <EmptyState compact title="没有匹配的能力" description="尝试更换关键词或筛选条件。" />
          : shown.map((item) => <EntityRow key={item.id} leading={<AgentAvatar application={item} size={38} tint={item.color} />} title={item.name} description={item.description || (item.kind === 'chat' ? '智能体' : '应用')} trailing={<StatusBadge>{item.kind === 'chat' ? '智能体' : '应用'}</StatusBadge>} onClick={() => onSelect(item)} />)}
    </div>
  </div>;
}
