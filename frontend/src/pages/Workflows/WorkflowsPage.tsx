import { useEffect, useState } from 'react';
import { Button, Input, Modal, message } from 'antd';
import { EditOutlined, HistoryOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { ContentSection, EmptyState, EntityCard, EntityRow, LoadingState, PageHeader, PageSurface, StatusBadge } from '@/components/ProductUI';
import { api } from '@/services/api';
import type { Workflow, WorkflowRun } from '@/types';
import './Workflows.css';
const unwrap = <T,>(value: T[] | { results?: T[] }): T[] => Array.isArray(value) ? value : value.results ?? [];
const WorkflowsPage = () => {
  const navigate = useNavigate(); const [workflows, setWorkflows] = useState<Workflow[]>([]); const [runs, setRuns] = useState<WorkflowRun[]>([]); const [loading, setLoading] = useState(true); const [creating, setCreating] = useState(false); const [name, setName] = useState('');
  const load = async () => { setLoading(true); try { const [workflowResponse, runResponse] = await Promise.all([api.get<Workflow[] | { results?: Workflow[] }>('/workflows/'), api.get<WorkflowRun[] | { results?: WorkflowRun[] }>('/workflows/runs/')]); setWorkflows(unwrap(workflowResponse)); setRuns(unwrap(runResponse)); } finally { setLoading(false); } };
  useEffect(() => { void load(); }, []);
  const create = async () => { if (!name.trim()) return; const workflow = await api.post<Workflow>('/workflows/', { name: name.trim(), description: '', icon: '🔀', is_public: false, steps: [] }); setCreating(false); setName(''); navigate(`/workflows/${workflow.id}/edit`); };
  const start = async (workflow: Workflow) => { try { const run = await api.post<{ id: string }>(`/workflows/${workflow.id}/start/`); navigate(`/workflow-runs/${run.id}`); } catch (error: any) { message.error(error?.response?.data?.detail || '工作流启动失败'); } };
  return <PageSurface width="wide" className="workflows-page">
    <PageHeader title="工作流" description="把多个应用组合成一个人工执行的创作流程" actions={<Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>新建工作流</Button>} />
    {loading ? <LoadingState label="正在加载工作流…" rows={5} /> : <>
      <ContentSection title="工作流" description="编辑步骤并按需运行">
        {workflows.length === 0 ? <EmptyState compact title="还没有工作流" description="创建工作流，把多个应用串联起来。" /> : <div className="workflow-grid">{workflows.map((workflow) => <EntityCard key={workflow.id} leading={<span className="workflow-card-icon">{workflow.icon || '🔀'}</span>} title={workflow.name} description={workflow.description || '未填写说明'} meta={`${workflow.step_count || 0} 个应用`} trailing={<Button icon={<EditOutlined />} onClick={() => navigate(`/workflows/${workflow.id}/edit`)}>编辑</Button>} footer={<Button block type="primary" icon={<PlayCircleOutlined />} disabled={!workflow.step_count} onClick={() => void start(workflow)}>运行</Button>} />)}</div>}
      </ContentSection>
      <ContentSection title={<><HistoryOutlined /> 执行历史</>} description="重新打开时仅恢复聊天应用的对话">
        {runs.length === 0 ? <EmptyState compact title="暂无执行历史" /> : <div className="workflow-history-list">{runs.map((run) => { const completed = run.step_runs.filter((stepRun) => stepRun.status === 'completed').length; const tone = run.status === 'completed' ? 'success' : run.status === 'archived' ? 'neutral' : 'info'; return <EntityRow key={run.id} leading={<HistoryOutlined />} title={run.workflow_name} description={new Date(run.updated_at || run.created_at || '').toLocaleString()} meta={`${completed}/${run.step_runs.length} 个应用`} trailing={<StatusBadge tone={tone}>{run.status === 'completed' ? '已完成' : run.status === 'archived' ? '已归档' : '进行中'}</StatusBadge>} onClick={() => navigate(`/workflow-runs/${run.id}`)} />; })}</div>}
      </ContentSection>
    </>}
    <Modal title="新建工作流" open={creating} okText="创建" cancelText="取消" onOk={create} onCancel={() => setCreating(false)}><Input autoFocus value={name} placeholder="例如：小红书内容生产" onChange={(event) => setName(event.target.value)} onPressEnter={create} /></Modal>
  </PageSurface>;
};
export default WorkflowsPage;
