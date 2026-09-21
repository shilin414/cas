import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Empty, Form, Input, Modal, Result, Segmented, Skeleton, Tag } from 'antd';
import { SendOutlined, CheckCircleOutlined, InfoCircleOutlined, LockOutlined, SafetyCertificateOutlined, SearchOutlined } from '@ant-design/icons';
import { registerSessionLoginReturnTo } from '@/services/authRedirect';
import { appPath } from '@/lib/deploymentPaths';
import type { V2Application } from '@/services/runApi';
import { businessAppApi, type BusinessResult, type BusinessStatus } from '@/services/businessAppApi';
import { appDefinitions, buildInput, isQuery, needsPassword, type BusinessKey, type FormValues } from './model';
import FeishuForwardModal from '@/components/Chat/FeishuForwardModal';
import BusinessResultView from './BusinessResultView';
import './BusinessApp.css';

function errorMessage(error: unknown): string {
 const detail = (error as { response?: { data?: { detail?: unknown } } })?.response?.data?.detail;
 return typeof detail === 'string' ? detail : '暂时无法确认结果。账号操作请先核实实际状态，不要重复提交。';
}
function unavailableDescription(status: BusinessStatus): string {
 if (!status.configured) return '当前环境缺少该应用所需的接口地址或凭证，请由管理员完成服务端配置后重试。';
 if (!status.can_submit) return '接口已配置，但当前账号尚未绑定可信工号；请联系管理员完成身份映射或代办授权。';
 return '当前服务状态暂不允许提交，请稍后重试。';
}
const BusinessApp: React.FC<{ application: V2Application; snapshotToken?: string; onNewQuery?: () => void }> = ({ application, snapshotToken: requestedSnapshotToken, onNewQuery }) => {
 const appKey = application.renderer_key as BusinessKey;
 const definition = appDefinitions[appKey];
 const query = isQuery(appKey);
 const snapshotToken = query ? requestedSnapshotToken : undefined;
 const [form] = Form.useForm<FormValues>();
 const [action, setAction] = useState(definition.options[0]?.value || '');
 const [status, setStatus] = useState<BusinessStatus | null>(null);
 const [statusError, setStatusError] = useState('');
 const [refresh, setRefresh] = useState(0);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [result, setResult] = useState<BusinessResult | null>(null);
 const recoveryToken = result?.snapshot?.token;
 useEffect(() => {
  if (!query || !recoveryToken) return;
  return registerSessionLoginReturnTo(`${appPath(`/app/${application.slug}`)}?result=${encodeURIComponent(recoveryToken)}`);
 }, [query, application.slug, recoveryToken]);
 const [finishedAt, setFinishedAt] = useState('');
 const [forwardOpen, setForwardOpen] = useState(false);
 const sendQuery = useCallback((token: string, targets: {target_type: 'user' | 'chat'; id: string}[]) => businessAppApi.forward(application.id, token, targets), [application.id]);
 const [pending, setPending] = useState<FormValues | null>(null);
 const request = useRef<AbortController | null>(null);
 const submitting = useRef(false);
 useEffect(() => {
  if (snapshotToken) return;
  const controller = new AbortController(); setStatus(null); setStatusError('');
  businessAppApi.status(application.id, controller.signal).then(value => {
   if (controller.signal.aborted) return;
   setStatus(value); form.setFieldValue('usercode', value.account);
  }).catch(e => { if (!controller.signal.aborted) setStatusError(errorMessage(e)); });
  return () => controller.abort();
  }, [application.id, form, refresh, snapshotToken]);
 useEffect(() => {
  if (!snapshotToken || !query) return;
  const controller = new AbortController(); setBusy(true); setError(''); setResult(null); setForwardOpen(false);
  businessAppApi.snapshot(application.id, snapshotToken, controller.signal).then(value => {
   if (!controller.signal.aborted) { setResult(value); setFinishedAt(new Date(value.snapshot!.queried_at).toLocaleString('zh-CN')); }
  }).catch(e => { if (!controller.signal.aborted) setError(errorMessage(e)); }).finally(() => { if (!controller.signal.aborted) setBusy(false); });
  return () => controller.abort();
 }, [application.id, snapshotToken, query]);
 useEffect(() => () => { request.current?.abort(); }, []);
 const password = needsPassword(appKey, action);
 const operation = definition.options.find(option => option.value === action)?.label || definition.title;
 const clearSensitive = () => { form.resetFields(['password', 'password_confirm']); setPending(null); };
 const execute = async (values: FormValues) => {
  if (submitting.current) return;
  submitting.current = true; setBusy(true); setError(''); setResult(null); setPending(null); setForwardOpen(false);
  const controller = new AbortController(); request.current = controller;
  const input = buildInput(appKey, action, values);
  clearSensitive();
  try {
   const response = await businessAppApi.execute(application.id, input, controller.signal);
   if (!controller.signal.aborted) { setResult(response); setFinishedAt(new Date(response.snapshot?.queried_at || Date.now()).toLocaleString('zh-CN')); if (!query) form.resetFields(['mobile', 'officephone', 'fax']); }
  } catch (e) { if (!controller.signal.aborted) setError(errorMessage(e)); }
  finally { if (!controller.signal.aborted) { setBusy(false); submitting.current = false; } }
 };
 const changeAction = (value: string) => {
  if (busy) return;
  setAction(value); setResult(null); setError(''); setPending(null); setForwardOpen(false);
  form.resetFields(); form.setFieldValue('usercode', status?.account || '');
 };
 return (
  <div className="business-app">
   <section className="business-app__hero">
    <div><div className="business-app__eyebrow">{definition.eyebrow}</div><h1>{definition.title}</h1><p>{definition.description}</p></div>
    <Tag icon={query ? <SearchOutlined /> : <SafetyCertificateOutlined />} bordered={false}>{query ? '业务系统直查' : '安全账号服务'}</Tag>
   </section>
   <div className="business-app__layout">
    <main className="business-app__main">
     {snapshotToken && <Alert showIcon type="info" message="查询结果快照（非实时数据）" description="快照自查询起24小时有效；内容不会随业务系统变化。" action={<Button onClick={onNewQuery}>发起新查询</Button>} />}
     {!snapshotToken && <section className="business-app__panel" aria-label="操作表单">
      <div className="business-app__section-title"><span className="business-app__step">1</span><h2>{query ? '填写查询条件' : '确认账号与信息'}</h2></div>
      {statusError ? <Alert type="error" showIcon message="无法加载服务状态" description={statusError} action={<Button onClick={() => setRefresh(v => v + 1)}>重试</Button>} /> : !status ? <Skeleton active paragraph={{ rows: 2 }} /> : !status.can_submit ? <Alert type="warning" showIcon message={status.reason || '服务暂不可用'} description={unavailableDescription(status)} /> : null}
      <Form form={form} layout="vertical" requiredMark={false} disabled={busy || !status?.can_submit} preserve={false} onValuesChange={() => {setError(''); setResult(null); setForwardOpen(false);}} onFinish={values => query ? void execute(values) : setPending(values)}>
       {!!definition.options.length && <div className="business-app__modes"><Segmented block options={definition.options} value={action} onChange={changeAction} disabled={busy || !!pending} aria-label="操作类型" /></div>}
       {appKey === 'barcode-query' && <Form.Item name="barcode" label="20位条码" rules={[{ required: true, message: '请输入条码' }, { pattern: /^\d{20}$/, message: '条码必须为20位数字' }]}><Input size="large" inputMode="numeric" maxLength={20} placeholder="请输入或粘贴20位条码" autoComplete="off" allowClear /></Form.Item>}
       {appKey === 'material-query' && <Form.Item name="query" label={action === 'article' ? '货号' : '69码'} rules={[{ required: true, whitespace: true, message: '请输入查询内容' }, { validator: (_, value: string) => !value || (value.trim().toLowerCase() !== 'a' && (action !== 'ean' || /^69\d{11}$/.test(value.trim()))) ? Promise.resolve() : Promise.reject(new Error(action === 'ean' ? '请输入69开头的13位数字' : 'a为接口保留值，请输入完整货号')) }]}><Input size="large" maxLength={action === 'ean' ? 13 : 100} inputMode={action === 'ean' ? 'numeric' : 'text'} placeholder={action === 'article' ? '例如：A8305' : '请输入13位69码'} autoComplete="off" allowClear /></Form.Item>}
       {!query && <Form.Item name="usercode" label="操作工号" extra={status?.can_manage_others ? '你有代办权限，请仔细核对目标工号。' : '工号由服务端绑定，仅可操作本人账号。'} rules={[{ required: true, message: '请填写工号或联系管理员绑定' }, { pattern: /^[A-Za-z0-9_.-]{1,32}$/, message: '工号格式不正确' }]}><Input size="large" readOnly={!status?.can_manage_others} autoComplete="off" maxLength={32} prefix={<LockOutlined />} /></Form.Item>}
       {password && <>
        <Form.Item name="password" label="新密码" extra="12—64位，包含字母和数字，不含空白字符。" rules={[{ required: true, message: '请输入新密码' }, { min: 12, max: 64, message: '请输入12—64位密码' }, { pattern: /^(?=.*[\p{L}])(?=.*[\p{N}])[^\s\p{Cc}]+$/u, message: '需包含字母和数字，不能包含空白或控制字符' }]}><Input.Password size="large" maxLength={64} autoComplete="new-password" placeholder="设置新密码" /></Form.Item>
        <Form.Item name="password_confirm" label="再次输入新密码" dependencies={['password']} rules={[{ required: true, message: '请再次输入新密码' }, ({getFieldValue}) => ({validator: (_, value) => !value || getFieldValue('password') === value ? Promise.resolve() : Promise.reject(new Error('两次输入的密码不一致'))})]}><Input.Password size="large" maxLength={64} autoComplete="new-password" placeholder="再次输入，确保一致" /></Form.Item>
       </>}
       {appKey === 'oa-phone' && <>
        <Form.Item name="mobile" label="新手机号码" rules={[{required: true, message: '请输入手机号码'}, { pattern: /^1[3-9]\d{9}$/, message: '请输入有效的11位手机号码' }]}><Input size="large" inputMode="tel" maxLength={11} autoComplete="off" placeholder="请输入新手机号码" /></Form.Item>
        <div className="business-app__fields"><Form.Item name="officephone" label="办公电话（可选）"><Input size="large" inputMode="tel" maxLength={32} /></Form.Item><Form.Item name="fax" label="传真（可选）"><Input size="large" inputMode="tel" maxLength={32} /></Form.Item></div>
       </>}
       {appKey === 'tpm-account' && action === 'lock' && <Alert className="business-app__warning" showIcon type="warning" message="锁定后该账号将无法登录TPM。" />}
       <Button className="business-app__submit" type="primary" danger={appKey === 'tpm-account' && action === 'lock'} size="large" htmlType="submit" loading={busy} disabled={!status?.can_submit || !!pending} icon={query ? <SearchOutlined /> : <SafetyCertificateOutlined />}>{busy ? (query ? '正在查询…' : '正在提交，请勿重复操作…') : query ? '开始查询' : '核对并继续'}</Button>
      </Form>
     </section>}
     <section className="business-app__panel business-app__results" aria-label="操作结果" aria-live="polite" aria-busy={busy}>
      <div className="business-app__section-title"><span className="business-app__step">{snapshotToken ? 1 : 2}</span><h2>{query ? '查询结果' : '操作结果'}</h2>{result && <span className="business-app__timestamp">{finishedAt}</span>}</div>
      {query && result && !busy && !error && <div className="business-app__forward-bar">
       {result.snapshot && <p><strong>{result.snapshot.query_label}</strong>：{result.snapshot.query_value}</p>}
       {result.snapshot?.can_forward && <Button size="large" icon={<SendOutlined />} onClick={() => setForwardOpen(true)}>转发到飞书</Button>}
       {result.forward_unavailable && <Alert type="warning" showIcon message={result.forward_unavailable} />}
       {result.snapshot && <small>快照有效至 {new Date(result.snapshot.expires_at).toLocaleString('zh-CN')}{!result.snapshot.can_forward ? ' · 仅查询发起人可转发' : ''}</small>}
      </div>}
      {busy ? <Skeleton active paragraph={{ rows: 4 }} /> : error ? <Alert showIcon type="error" message="本次操作未确认成功" description={error} /> : result ? query ? <BusinessResultView data={result.data} /> : <Result status="success" title="操作成功" subTitle={result.message} /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={query ? '填写条件后，查询结果将在这里显示' : '完成确认后，这里会显示处理结果'} />}
     </section>
    </main>
    <aside className="business-app__aside">
     <div className="business-app__panel"><h2><InfoCircleOutlined /> 使用提示</h2><p>{definition.hint}</p><ul><li>结果来自业务系统，不经AI生成或识别。</li><li>接口暂不可用时，请联系服务台处理。</li>{!query && <li>请求超时不代表操作未执行，请先核实再提交。</li>}</ul></div>
     <div className="business-app__trust"><CheckCircleOutlined /><div><strong>{query ? '无需上传图片' : '凭据仅在服务端使用'}</strong><p>{query ? '直接输入信息，查询更明确。' : '敏感操作留痕，密码不记录。'}</p></div></div>
    </aside>
   </div>
   {query && result?.snapshot?.can_forward && <FeishuForwardModal key={result.snapshot.token} open={forwardOpen} shareToken={result.snapshot.token} onClose={() => setForwardOpen(false)} send={sendQuery} reauthReturnTo={`${appPath(`/app/${application.slug}`)}?result=${encodeURIComponent(result.snapshot.token)}`} summary={<>
    <strong>{definition.title}</strong><p>{result.snapshot.query_label}：{result.snapshot.query_value}</p><p>查询时间：{new Date(result.snapshot.queried_at).toLocaleString('zh-CN')}</p>
    <p>所选用户／群组将在卡片中直接看到查询条件、时间和主要结果；无需先打开网页。卡片按钮用于进入查询应用，需登录并具备应用权限，快照24小时有效。</p><p>同一快照对同一目标不重复发送；明确失败可重试，发送状态不确定时请先在飞书核实。</p>
   </>} />}
   <Modal open={!!pending} title="请确认本次账号操作" okText="确认提交" cancelText="返回修改" onCancel={clearSensitive} onOk={() => pending && void execute(pending)} okButtonProps={{danger: appKey === 'tpm-account' && action === 'lock', style: { minHeight: 44 }}} cancelButtonProps={{ style: { minHeight: 44 } }} destroyOnHidden>
    <p>应用：<strong>{definition.title}</strong></p><p>目标工号：<strong>{pending?.usercode}</strong></p>{action && <p>操作：<strong>{operation}</strong></p>}
    {appKey === 'oa-phone' && <p>新手机号：{pending?.mobile?.replace(/^(\d{3})\d{4}(\d{4})$/, '$1****$2')}</p>}
    <Alert showIcon type="warning" message={appKey === 'tpm-account' && action === 'reset' ? '重置密码将同时解锁该账号。' : '提交后将直接更新业务系统，请确认账号和信息无误。'} />
   </Modal>
  </div>
 );
};
export default BusinessApp;
