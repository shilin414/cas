import React, { useEffect, useState } from 'react';
import { Alert, Button, Empty, Select, Spin, Tag } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { aiModelsApi, type AIModel, type InvocationMetadata } from '@/services/aiModels';
import { errorText, statusLabels } from './modelLogic';
export default function InvocationLogs({ models }: { models: AIModel[] }) {
  const [model, setModel] = useState<string>(); const [revision, setRevision] = useState(0);
  const [rows, setRows] = useState<InvocationMetadata[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState('');
  useEffect(() => {
    const abort = new AbortController(); setLoading(true); setError(''); setRows([]);
    aiModelsApi.logs(model, abort.signal).then((items) => { if (!abort.signal.aborted) setRows(items); }).catch((e) => { if (!abort.signal.aborted) setError(errorText(e)); }).finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [model, revision]);
  return <div className="aim-stack"><div className="aim-section-heading"><div><h3>调用记录</h3><p>仅展示低风险元数据，不提供提示词、正文、输出内容或附件。</p></div><Button icon={<ReloadOutlined />} onClick={() => setRevision((value) => value + 1)} loading={loading}>刷新</Button></div>
    <Select aria-label="筛选记录模型" allowClear placeholder="全部模型" value={model} options={models.map((item) => ({ value: item.id, label: item.name }))} onChange={setModel} />
    {error && <Alert showIcon type="error" message={error} />}
    {loading ? <Spin /> : rows.length === 0 ? <Empty description={error ? '调用记录加载失败，可点击刷新重试' : '暂无调用记录'} /> : <div className="aim-log-list">{rows.map((item) => <article className="aim-log-row" key={item.id}><div><strong>{models.find((entry) => entry.id === item.model_id)?.name || item.model_id}</strong><Tag color={item.status === 'succeeded' ? 'success' : item.status === 'failed' ? 'error' : 'default'}>{statusLabels[item.status] || item.status}</Tag><small className="aim-id">{item.id}</small></div><div className="aim-inline-meta"><span>{new Date(item.created_at).toLocaleString()}</span><span>{item.duration_ms != null ? `${(item.duration_ms / 1000).toFixed(2)}s` : '耗时未知'}</span><span>输入 / 输出 {item.input_tokens ?? '—'} / {item.output_tokens ?? '—'} tokens</span>{item.error_code && <span className="aim-inline-error">{item.error_code}</span>}{item.cancel_requested && <span>{item.cancellation_confirmed ? '取消已确认' : '已请求取消，未确认'}</span>}</div></article>)}</div>}
  </div>;
}
