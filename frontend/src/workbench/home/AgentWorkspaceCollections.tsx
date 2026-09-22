import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Spin, Switch } from 'antd';
import { ClockCircleOutlined, FileTextOutlined, MessageOutlined, PlusOutlined, SearchOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { enableSchedule, disableSchedule } from '@/services/scheduleApi';
import type { Schedule } from '@/types/schedule';
import { describeSchedulePlan, formatDateTime } from '@/lib/scheduleFormat';
import { ScheduleEditorModal } from '@/components/Schedules/ScheduleEditorModal';
import { apiUrl } from '@/lib/deploymentPaths';
import { fetchAgentCollection, type Tab, type Row } from './agentWorkspaceData';

const tabs: { key: Tab; label: string }[] = [{key:'tasks',label:'任务'}, {key:'automations',label:'自动化'}, {key:'resources',label:'资源'}];

// Parent keys this component by agent ID: query, tab and in-flight responses never cross agents.
export default function AgentWorkspaceCollections({applicationId, slug}: {applicationId:number; slug:string}) {
  const navigate = useNavigate();
  const [tab, setTab] = useState<Tab>('tasks');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<Row[]>([]);
  const [next, setNext] = useState('');
  const [loading, setLoading] = useState(true);
  const [moreLoading, setMoreLoading] = useState(false);
  const [error, setError] = useState('');
  const [errorPhase, setErrorPhase] = useState<'initial' | 'more' | 'mutation'>('initial');
  const [revision, setRevision] = useState(0);
  const [editor, setEditor] = useState<{editing:Schedule | null} | null>(null);
  const [mutating, setMutating] = useState<number | null>(null);
  const generation = useRef(0);
  useEffect(() => {
    const version = ++generation.current;
    setErrorPhase('initial'); setLoading(true); setRows([]); setNext(''); setError(''); setMoreLoading(false);
    const timer = window.setTimeout(() => {
      void fetchAgentCollection(applicationId, tab, query).then(page => {
        if (version !== generation.current) return;
        setRows(page.rows); setNext(page.next);
      }).catch(() => {if(version === generation.current) setError('内容加载失败，请重试。');})
        .finally(() => {if(version === generation.current) setLoading(false);});
    }, query ? 200 : 0);
    return () => {window.clearTimeout(timer); generation.current = version + 1;};
  },[applicationId,tab,query,revision]);
  const loadMore = async () => {
    const version = generation.current;
    setMoreLoading(true); setError(''); setErrorPhase('more');
    try {
      const page = await fetchAgentCollection(applicationId,tab,query,next);
      if(version === generation.current) {setRows(old => [...old,...page.rows]); setNext(page.next);}
    } catch {if(version === generation.current) setError('加载更多失败，已有内容已保留，请重试。');}
    finally {if(version === generation.current) setMoreLoading(false);}
  };
  const toggle = async (schedule: Schedule, enabled:boolean) => {
    const version = generation.current;
    setMutating(schedule.id); setError(''); setErrorPhase('mutation');
    try {
      const updated = await (enabled ? enableSchedule : disableSchedule)(schedule.id);
      if(version === generation.current) setRows(old => old.map(row => row.kind === 'automations' && row.value.id === updated.id ? {kind:'automations',value:updated} : row));
    } catch {if(version === generation.current) setError('自动化状态更新失败，未改变原状态。');}
    finally {setMutating(null);}
  };
  const openTask = (id:string) => navigate(`/chat/${slug}?conversation=${id}`);
  return <section className="agent-home-collections" aria-label="当前智能体的工作内容">
    <div className="agent-home-collections__toolbar">
      <div role="tablist" aria-label="工作内容" className="agent-home-tabs">
        {tabs.map(item => <button key={item.key} id={`agent-tab-${item.key}`} role="tab" aria-selected={tab===item.key} aria-controls="agent-collection-panel" tabIndex={tab===item.key ? 0 : -1} onKeyDown={event => {
          const index=tabs.findIndex(item => item.key===tab);
          const destination=event.key==='ArrowRight' ? (index+1)%3 : event.key==='ArrowLeft' ? (index+2)%3 : event.key==='Home' ? 0 : event.key==='End' ? 2 : -1;
          if(destination<0) return; event.preventDefault(); setTab(tabs[destination].key); document.getElementById(`agent-tab-${tabs[destination].key}`)?.focus();
        }} onClick={() => {setTab(item.key); setQuery('');}}>{item.label}</button>)}
      </div>
      <div className="agent-home-collections__actions">
        {tab==='automations' && <Button aria-label="新建自动化" icon={<PlusOutlined />} onClick={() => setEditor({editing:null})} />}
        <Input aria-label={`搜索${tabs.find(item=>item.key===tab)?.label}`} placeholder="搜索" prefix={<SearchOutlined />} maxLength={200} allowClear value={query} onChange={e=>setQuery(e.target.value)} />
      </div>
    </div>
    <div role="tabpanel" id="agent-collection-panel" aria-labelledby={`agent-tab-${tab}`} aria-busy={loading || moreLoading}>
      {tab==='resources' && <p className="agent-home-collections__note">该智能体历史任务生成的文件与产物，不包含上传附件。</p>}
      {error && <Alert type="error" message={error} action={<Button size="small" onClick={()=> errorPhase === 'more' && next ? void loadMore() : setRevision(v=>v+1)}>重试</Button>} />}
      {loading ? <div className="agent-home-collections__empty"><Spin /><span>正在加载…</span></div> : <>
        {rows.map(row => <div className="agent-home-row" key={`${row.kind}-${row.value.id}`}>
          {row.kind==='tasks' ? <>
            <MessageOutlined className="agent-home-row__icon" />
            <button className="agent-home-row__main" onClick={()=>openTask(row.value.id)}><strong>{row.value.title || '未命名任务'}</strong><span>{row.value.preview ? row.value.preview.replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[#*`|>]/g, '').slice(0, 120) : '继续这个任务'}</span></button>
            <span className={`agent-home-state agent-home-state--${row.value.executionState}`}>{row.value.executionState==='running' ? '执行中' : row.value.executionState==='error' ? '异常' : '可继续'}</span>
            <time>{formatDateTime(row.value.updatedAt)}</time>
          </> : row.kind==='automations' ? <>
            <ThunderboltOutlined className="agent-home-row__icon" />
            <button className="agent-home-row__main" onClick={()=>setEditor({editing:row.value})}><strong>{row.value.name}</strong><span>{row.value.enabled && row.value.next_run_at ? `下次执行 ${formatDateTime(row.value.next_run_at)}` : '点击查看或编辑自动化'}</span></button>
            <span className="agent-home-row__plan"><ClockCircleOutlined /> {describeSchedulePlan(row.value)}</span>
            <Switch aria-label={`${row.value.enabled ? '停用':'启用'} ${row.value.name}`} checked={row.value.enabled} loading={mutating===row.value.id} disabled={mutating!==null} onChange={value=>void toggle(row.value,value)} />
          </> : <>
            <FileTextOutlined className="agent-home-row__icon" />
            <div className="agent-home-row__main"><a href={`${apiUrl()}/v2/artifacts/${row.value.id}/open`} target="_blank" rel="noopener noreferrer"><strong>{row.value.name || '生成产物'}</strong></a><button className="agent-home-row__source" onClick={()=>openTask(row.value.conversation_id)}>来自：{row.value.task_title || '未命名任务'}</button></div>
            <span className="agent-home-row__type">{row.value.normalized_type}</span><time>{formatDateTime(row.value.created_at)}</time>
          </>}
        </div>)}
        {!rows.length && !error && <div className="agent-home-collections__empty">
          {tab==='tasks' ? <MessageOutlined /> : tab==='automations' ? <ThunderboltOutlined /> : <FileTextOutlined />}
          <strong>{query ? '没有找到匹配的内容' : tab==='tasks' ? '还没有任务' : tab==='automations' ? '还没有自动化' : '还没有生成的资源'}</strong>
          <span>{query ? '试试其他关键词' : tab==='tasks' ? '在上方输入目标，开始与这个智能体协作' : tab==='automations' ? '将重复工作交给智能体按计划执行' : '任务生成的文件和产物会汇集在这里'}</span>
          {tab==='automations' && !query && <Button onClick={()=>setEditor({editing:null})} icon={<PlusOutlined />}>新建自动化</Button>}
        </div>}
        {next && <div className="agent-home-collections__more"><Button loading={moreLoading} onClick={()=>void loadMore()}>加载更多</Button></div>}
      </>}
    </div>
    {editor && <ScheduleEditorModal open editing={editor.editing} presetApplicationId={applicationId} onClose={()=>setEditor(null)} onSaved={()=>setRevision(v=>v+1)} />}
  </section>;
}
