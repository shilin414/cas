import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Empty, Input, Modal, Popconfirm, Select, Skeleton, Switch, Tabs, Tag, Tooltip } from 'antd';
import { ApiOutlined, CloudOutlined, DatabaseOutlined, EditOutlined, ExperimentOutlined, KeyOutlined, PlusOutlined, ReloadOutlined, ScanOutlined, SearchOutlined, DeleteOutlined } from '@ant-design/icons';
import { aiModelsApi, type AIConnection, type AIModel } from '@/services/aiModels';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useAuthStore } from '@/stores/useAuthStore';
import { ConnectionForm, CredentialDialog, ModelForm } from './Editors';
import { errorText, permissionsFor } from './modelLogic';
import RemoteTestPanel from './RemoteTestPanel';
import OcrEntry from './OcrEntry';
import InvocationLogs from './InvocationLogs';
import './AIModelsPage.css';

export default function AIModelsPage() {
  const identity = useAdminPermissionStore((state) => state.identity);
  const user = useAuthStore((state) => state.user);
  const permissions = useMemo(() => permissionsFor(identity?.permissions.map((item) => item.code) ?? [], Boolean(user?.is_staff || identity?.is_super_admin)), [identity, user?.is_staff]);
  // Identity changes unmount all private session/body state, including already-open tabs.
  return <AIModelsWorkspace key={String(user?.id ?? 'anonymous')} permissions={permissions} />;
}
export function AIModelsWorkspace({ permissions }: { permissions: ReturnType<typeof permissionsFor> }) {
  const [models, setModels] = useState<AIModel[]>([]); const [connections, setConnections] = useState<AIConnection[]>([]);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const [revision, setRevision] = useState(0); const [tab, setTab] = useState(permissions.read ? 'models' : permissions.logs ? 'logs' : 'test'); const [query, setQuery] = useState('');
  const [filter, setFilter] = useState('all'); const [selectedId, setSelectedId] = useState<string>();
  const [modelEditor, setModelEditor] = useState<AIModel | 'new' | null>(null); const [connectionEditor, setConnectionEditor] = useState<AIConnection | 'new' | null>(null);
  const [secret, setSecret] = useState<AIConnection | null>(null); const [working, setWorking] = useState(false); const [testBusy, setTestBusy] = useState(false);
  const busyChange = useCallback((value: boolean) => setTestBusy(value), []);
  const refresh = () => setRevision((value) => value + 1);
  useEffect(() => {
    if (!permissions.read) return;
    const abort = new AbortController(); setLoading(true); setError('');
    Promise.all([aiModelsApi.models(abort.signal), aiModelsApi.connections(abort.signal)]).then(([nextModels, nextConnections]) => { if (!abort.signal.aborted) { setModels(nextModels); setConnections(nextConnections); } }).catch((e) => { if (!abort.signal.aborted) setError(errorText(e)); }).finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [permissions.read, revision]);
  const act = async (action: () => Promise<unknown>, success: string) => {
    if (working) return; setWorking(true); setError(''); setNotice('');
    try { await action(); setNotice(success); refresh(); } catch (e) { setError(errorText(e)); } finally { setWorking(false); }
  };
  const selected = models.find((model) => model.id === selectedId);
  const filtered = models.filter((model) => (filter === 'all' || model.execution_location === filter) && `${model.name} ${model.model_id}`.toLowerCase().includes(query.toLowerCase()));
  const denied = (code: string) => <Alert type="warning" showIcon message="当前账号没有此操作权限" description={`需要 ${code}。权限由管理员分配，不由角色名称判断。`} />;
  if (!permissions.read && !permissions.logs && !permissions.test) return <section className="aim-page">{denied('ai.model.read / ai.model.test / ai.model.log.read')}</section>;
  const modelList = <div className="aim-stack"><div className="aim-section-heading"><div><h3>模型资产</h3><p>远程模型与本地 OCR 共用管理入口，执行边界明确分离。</p></div><Tooltip title={!permissions.write ? '需要 ai.model.write' : undefined}><Button type="primary" icon={<PlusOutlined />} disabled={!permissions.write || working} onClick={() => setModelEditor('new')}>添加模型</Button></Tooltip></div>
    <div className="aim-filters"><Input aria-label="搜索模型" prefix={<SearchOutlined />} allowClear placeholder="搜索名称或模型标识" value={query} onChange={(e) => setQuery(e.target.value)} /><Select aria-label="筛选执行位置" value={filter} onChange={setFilter} options={[{ value: 'all', label: '全部执行位置' }, { value: 'server_remote', label: '服务端远程' }, { value: 'browser_local', label: '浏览器本地' }]} /></div>
    {!permissions.write && <p className="aim-footnote">只读视图 · 修改配置需要 ai.model.write；密钥单独授权。</p>}
    {loading && !models.length ? <Skeleton active /> : filtered.length === 0 ? <Empty description={error ? '模型未能加载，请刷新重试' : query || filter !== 'all' ? '没有符合筛选条件的模型' : '还没有模型。先添加远程连接，或直接配置本地 OCR。'} /> : <div className="aim-model-grid">{filtered.map((model) => {
      const local = model.execution_location === 'browser_local'; const connection = connections.find((item) => item.id === model.connection_id);
      return <article className="aim-model-card" key={model.id}><div className="aim-card-top"><span className={`aim-model-icon ${local ? 'is-local' : ''}`}>{local ? <ScanOutlined /> : <CloudOutlined />}</span><div className="aim-card-title"><h3>{model.name}</h3><span className="aim-model-id">{model.model_id}</span></div><Tag color={model.enabled ? 'success' : 'default'}>{model.enabled ? '启用' : '停用'}</Tag></div>
        <div className="aim-card-tags"><Tag>{local ? '浏览器本地' : '服务端远程'}</Tag><Tag>{local ? 'OCR' : '对话'}</Tag><Tag>能力未验证</Tag></div>
        <div className="aim-card-facts"><span>{local ? '资源版本' : '远程连接'}</span><strong>{local ? model.browser_manifest?.version || '未配置' : connection?.name || '连接不可用'}</strong><span>输入声明</span><strong>{local ? '图片' : ['文字', model.capabilities.image && '图片', model.capabilities.video && '视频', model.capabilities.pdf && 'PDF'].filter(Boolean).join(' / ')}</strong></div>
        <div className="aim-card-footer"><Button icon={<ExperimentOutlined />} disabled={!permissions.test || testBusy} onClick={() => { setSelectedId(model.id); setTab('test'); }}>测试模型</Button><div className="aim-card-actions"><Tooltip title="编辑模型"><Button aria-label={`编辑 ${model.name}`} type="text" icon={<EditOutlined />} disabled={!permissions.write || working || testBusy} onClick={() => setModelEditor(model)} /></Tooltip><Popconfirm title={`${model.enabled ? '停用' : '启用'}模型 ${model.name}？`} onConfirm={() => act(() => aiModelsApi.updateModel(model.id, { enabled: !model.enabled }), '模型状态已更新')}><Switch aria-label={`${model.name} 启用状态`} size="small" checked={model.enabled} disabled={!permissions.write || working || testBusy} /></Popconfirm><Popconfirm title={`删除模型 ${model.name}？`} description="已有引用或调用可能阻止删除，服务端校验失败会明确显示。" onConfirm={() => act(() => aiModelsApi.deleteModel(model.id), '模型已删除')}><Button aria-label={`删除 ${model.name}`} danger type="text" icon={<DeleteOutlined />} disabled={!permissions.write || working || testBusy} /></Popconfirm></div></div>
      </article>;
    })}</div>}
  </div>;
  const connectionList = <div className="aim-stack"><div className="aim-section-heading"><div><h3>远程连接</h3><p>协议、服务地址与调用限制集中维护；密钥写入单独授权。</p></div><Button type="primary" icon={<PlusOutlined />} disabled={!permissions.write || working} onClick={() => setConnectionEditor('new')}>添加连接</Button></div>
    {loading && !connections.length ? <Skeleton active /> : !connections.length ? <Empty description={error ? '连接未能加载，请刷新重试' : '暂无远程连接。本地 OCR 不需要连接或密钥。'} /> : <div className="aim-connection-list">{connections.map((connection) => <article className="aim-connection-card" key={connection.id}><div className="aim-card-top"><span className="aim-model-icon"><ApiOutlined /></span><div className="aim-card-title"><h3>{connection.name}</h3><span className="aim-model-id">{connection.base_url}</span></div><Tag color={connection.enabled ? 'success' : 'default'}>{connection.enabled ? '启用' : '停用'}</Tag></div><div className="aim-inline-meta"><Tag>{connection.adapter === 'gemini' ? 'Gemini' : 'OpenAI Chat'}</Tag>{connection.base_url.toLowerCase().startsWith('http://') && <Tag color="warning">HTTP · 非加密</Tag>}<span>{connection.timeout_seconds}s 超时</span><span>并发 {connection.max_concurrency}</span><Tag color={connection.has_credential ? 'default' : 'warning'}>{connection.has_credential ? '已设置密钥' : '待设置密钥'}</Tag></div><div className="aim-connection-actions"><Button icon={<EditOutlined />} disabled={!permissions.write || working || testBusy} onClick={() => setConnectionEditor(connection)}>编辑</Button><Tooltip title={!permissions.secret ? '需要 ai.connection.secret.write' : '不可回读现有密钥'}><Button icon={<KeyOutlined />} disabled={!permissions.secret || working} onClick={() => setSecret(connection)}>设置密钥</Button></Tooltip><Popconfirm title={`${connection.enabled ? '停用' : '启用'}此连接？`} description="使用此连接的所有模型均受影响。" onConfirm={() => act(() => aiModelsApi.updateConnection(connection.id, { enabled: !connection.enabled }), '连接状态已更新')}><Button disabled={!permissions.write || working || testBusy}>{connection.enabled ? '停用' : '启用'}</Button></Popconfirm><Popconfirm title={`删除连接 ${connection.name}？`} description="请先解除模型引用，删除失败会显示具体错误。" onConfirm={() => act(() => aiModelsApi.deleteConnection(connection.id), '连接已删除')}><Button danger icon={<DeleteOutlined />} disabled={!permissions.write || working || testBusy}>删除</Button></Popconfirm></div></article>)}</div>}
  </div>;
  const testPanel = !permissions.test ? denied('ai.model.test') : !permissions.read ? <Alert type="info" showIcon message="已具备测试权限，但没有模型配置读取权限" description="选择模型需要 ai.model.read；当前不会请求模型或连接配置。请联系管理员按需授权。" /> : <div className="aim-stack"><div className="aim-section-heading"><div><h3>模型测试台</h3><p>远程对话发送至服务端；本地 OCR 仅在明确启动后加载。</p></div><Tag icon={<ExperimentOutlined />}>真实执行</Tag></div><Select aria-label="选择测试模型" value={selectedId} disabled={testBusy} placeholder="选择一个模型" options={models.map((model) => ({ value: model.id, label: `${model.name} · ${model.execution_location === 'browser_local' ? '本地 OCR' : '远程对话'}${model.enabled ? '' : ' · 已停用'}` }))} onChange={setSelectedId} />{!selected ? <Empty description="选择模型后查看能力并开始测试" /> : selected.execution_location === 'browser_local' ? (tab === 'test' ? <OcrEntry key={selected.id + ':' + selected.version} model={selected} allowed={permissions.test} /> : null) : <RemoteTestPanel key={selected.id} model={selected} connection={connections.find((item) => item.id === selected.connection_id)} onBusyChange={busyChange} />}</div>;
  return <section className="aim-page enterprise-section">
    <header className="aim-page-header"><div><div className="aim-eyebrow"><DatabaseOutlined /> 模型基础设施</div><h2>AI 模型管理</h2><p>管理连接与模型，在清晰的权限和数据边界内验证能力。</p></div>{permissions.read && <Button icon={<ReloadOutlined />} loading={loading} disabled={testBusy} onClick={refresh}>刷新</Button>}</header>
    {permissions.read && <div className="aim-summary"><div><span>模型</span><strong>{models.length}</strong></div><div><span>已启用</span><strong>{models.filter((model) => model.enabled).length}</strong></div><div><span>远程连接</span><strong>{connections.length}</strong></div><div><span>本地 OCR</span><strong>{models.filter((model) => model.execution_location === 'browser_local').length}</strong></div></div>}
    {error && <Alert type="error" showIcon message={error} action={<Button size="small" onClick={refresh}>重试</Button>} />}
    {notice && <Alert type="success" showIcon message={notice} closable onClose={() => setNotice('')} />}
    <div className="aim-workbench"><Tabs activeKey={tab} onChange={setTab} items={[{ key: 'models', label: '模型列表', children: permissions.read ? modelList : denied('ai.model.read') }, { key: 'connections', label: '连接管理', children: permissions.read ? connectionList : denied('ai.model.read') }, { key: 'test', label: '测试台', children: testPanel }, { key: 'logs', label: '调用记录', children: permissions.logs ? <InvocationLogs models={permissions.read ? models : []} /> : denied('ai.model.log.read') }]} /></div>
    {modelEditor && permissions.write && <Modal open title={modelEditor === 'new' ? '添加模型' : '编辑模型'} footer={null} width={680} onCancel={() => setModelEditor(null)} destroyOnHidden maskClosable={false}><ModelForm key={modelEditor === 'new' ? 'new' : modelEditor.id} initial={modelEditor === 'new' ? undefined : modelEditor} connections={connections} onCancel={() => setModelEditor(null)} onSave={async (value) => { if (modelEditor === 'new') await aiModelsApi.createModel(value); else await aiModelsApi.updateModel(modelEditor.id, value); setModelEditor(null); setNotice('模型配置已保存，能力仍需实际验证'); refresh(); }} /></Modal>}
    {connectionEditor && permissions.write && <Modal open title={connectionEditor === 'new' ? '添加连接' : '编辑连接'} footer={null} width={600} onCancel={() => setConnectionEditor(null)} destroyOnHidden maskClosable={false}><ConnectionForm key={connectionEditor === 'new' ? 'new' : connectionEditor.id} initial={connectionEditor === 'new' ? undefined : connectionEditor} onCancel={() => setConnectionEditor(null)} onSave={async (value) => { if (connectionEditor === 'new') await aiModelsApi.createConnection(value); else await aiModelsApi.updateConnection(connectionEditor.id, value); setConnectionEditor(null); setNotice('连接配置已保存'); refresh(); }} /></Modal>}
    {secret && permissions.secret && <CredentialDialog key={secret.id} connection={secret} onClose={() => setSecret(null)} onSave={async (credential) => { await aiModelsApi.setCredential(secret.id, credential); setNotice('新密钥已保存，不可回读'); refresh(); }} />}
  </section>;
}
