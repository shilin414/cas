import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Result, Select, Space, Steps, Tag } from 'antd';
import { useApplicationPage } from '@/hooks/useApplicationPage';
import { enterpriseApi, type AccessDecision } from '../enterpriseApi';
import { useDirectoryUsers } from '../hooks/useDirectoryUsers';
import { useIsMobile } from '@/shell/useIsMobile';

export default function AccessDiagnosisPage() {
  const isMobile = useIsMobile();
  const [userId, setUserId] = useState<number>(); const [appId, setAppId] = useState<number>(); const [decision, setDecision] = useState<AccessDecision | null>(null); const [loading, setLoading] = useState(false); const [error, setError] = useState<string | null>(null); const requestSeq = useRef(0); const [userQuery, setUserQuery] = useState(''); const [appQuery, setAppQuery] = useState('');
  const users = useDirectoryUsers({ query: userQuery, includeInactive: true, sessionKey: 'diagnosis' });
  const apps = useApplicationPage({ kind: 'all', scope: 'manage', mode: 'manage', includeUnbound: true, query: appQuery, limit: 100 });
  useEffect(() => { requestSeq.current += 1; setDecision(null); setError(null); setLoading(false); }, [userId, appId]);
  const diagnose = async () => { if (!userId || !appId) return; const seq=++requestSeq.current; const requestedUser=userId; const requestedApp=appId; setLoading(true); setError(null); try { const next=await enterpriseApi.diagnose(requestedUser, requestedApp); if (requestSeq.current===seq && userId===requestedUser && appId===requestedApp) setDecision(next); } catch { if (requestSeq.current===seq) setError('权限诊断失败，请重试'); } finally { if (requestSeq.current===seq) setLoading(false); } };
  return <section className="enterprise-section"><div className="enterprise-section__head"><div><h2>权限诊断</h2><p>诊断与运行时 ACL 使用同一 Resolver，解释最终允许或拒绝的原因。</p></div></div><Card><Space wrap direction={isMobile ? 'vertical' : 'horizontal'} style={{ width: '100%' }}>
    <Select style={{ width: isMobile ? '100%' : 260 }} showSearch filterOption={false} searchValue={userQuery} onSearch={setUserQuery} placeholder="选择企业用户" value={userId} onChange={setUserId} loading={users.loading} onPopupScroll={(event) => { const { scrollTop, scrollHeight, clientHeight } = event.currentTarget; if (users.hasMore && !users.loadingMore && scrollHeight - scrollTop - clientHeight < 24) void users.loadMore(); }} options={users.items.map((user) => ({ value: user.id, label: user.name }))}/>
    <Select style={{ width: isMobile ? '100%' : 300 }} showSearch filterOption={false} searchValue={appQuery} onSearch={setAppQuery} placeholder="选择资源" value={appId} onChange={setAppId} loading={apps.loading} onPopupScroll={(event) => { const { scrollTop, scrollHeight, clientHeight } = event.currentTarget; if (apps.hasMore && !apps.loadingMore && scrollHeight - scrollTop - clientHeight < 24) void apps.loadMore(); }} options={apps.items.map((app) => ({ value: app.id, label: app.name }))}/>
    <Button type="primary" loading={loading} disabled={!userId || !appId} onClick={() => void diagnose()}>开始诊断</Button>
  </Space>{error && <Alert type="error" showIcon message={error} style={{ marginTop: 12 }} />}</Card>{decision && <Card>{decision.user?.resigned && <Alert type="error" showIcon message="该用户已离职，系统始终拒绝访问。"/>}<Result status={decision.allowed ? 'success' : 'error'} title={decision.allowed ? 'ALLOW' : 'DENY'} subTitle={<><Tag>{decision.reason_code}</Tag> {decision.application?.name} · {decision.user?.name}</>}/><Steps direction="vertical" size="small" current={decision.access_path.length - 1} items={decision.access_path.map((title) => ({ title }))}/></Card>}</section>;
}
