/** One mounted Form shared by desktop columns and mobile views. */
import React, { useEffect, useState } from 'react';
import { Alert, Button, Checkbox, Form, Input, InputNumber, Radio, Select, Switch } from 'antd';
import { ClockCircleOutlined, RightOutlined, RobotOutlined, SendOutlined } from '@ant-design/icons';
import { WEEKDAY_LABELS } from '@/lib/scheduleFormat';
import type { ScheduleType } from '@/types/schedule';
import type { ScheduleEditorState } from './useScheduleEditor';
import { ScheduleBoundaryFields } from './ScheduleBoundaryFields';
import './ScheduleEditor.css';

export type ScheduleEditorView = 'compose' | 'agent' | 'trigger' | 'delivery';
interface Props {
  state: ScheduleEditorState;
  mobile?: boolean;
  view?: ScheduleEditorView;
  onNavigate?: (view: ScheduleEditorView) => void;
  onPreview?: () => void | Promise<void>;
}

export function ScheduleEditorFields({ state, mobile = false, view = 'compose', onNavigate, onPreview }: Props) {
  const {
    hydrationReady, hydrationLoading, hydrationError, retryHydration,
    form, apps, appsLoading, appsHasMore, appsLoadingMore, loadMoreApps,
    appsError, refreshApps, appQuery, setAppQuery,
    appResolution, resolutionApplies, retryResolveApp,
    scheduleType, setScheduleType, setPreview, deliveryOn, setDeliveryOn,
    targets, targetsLoading, targetsHasMore, targetsLoadingMore, loadMoreTargets,
    targetsError, targetsPartialFailed, refreshTargets, targetsLoadMoreError,
    targetQuery, setTargetQuery, setSelectedTarget, preview, previewing, refreshPreview,
  } = state;
  const [advancedOpen, setAdvancedOpen] = useState(false);
  useEffect(() => {
    if (!state.saving && !previewing && form.getFieldsError().some((field) => field.errors.length && ['starts_at_local', 'ends_at_local', 'timezone', 'execution_window_seconds'].includes(String(field.name[0])))) setAdvancedOpen(true);
  }, [form, state.saving, previewing]);
  const appId = Form.useWatch('application_id', form);
  const time = Form.useWatch(['trigger', 'time'], form);
  const condition = Form.useWatch(['deliveries', 0, 'condition', 'operator'], form) ?? 'always';
  const watchedDeliveries = Form.useWatch('deliveries', { form, preserve: true });
  const deliveryCount = watchedDeliveries?.length ?? 0;
  const selectedApp = apps.find((app) => app.id === appId);
  const agentSummary = resolutionApplies && appResolution === 'transient-error' ? '加载失败，点此重试' : resolutionApplies && appResolution === 'unavailable' ? '请重新选择智能体' : selectedApp?.name || '选择智能体';
  const triggerSummary = `${{ once: '单次执行', daily: '每天', weekly: '每周', monthly: '每月' }[scheduleType]}${scheduleType === 'once' ? '' : ` ${time || '09:00'}`}`;
  const deliverySummary = deliveryOn ? (watchedDeliveries?.[0]?.target_name || '选择用户或群聊') : '不推送';

  return (
    <Form form={form} disabled={!hydrationReady} layout="vertical" preserve className={`automation-editor automation-editor--${mobile ? 'mobile' : 'desktop'}`} initialValues={{ trigger: { time: '09:00' }, start_mode: 'immediate', end_mode: 'never' }}
      onFieldsChange={(changed) => {
        if (changed.some((field) => field.errors?.length && ['start_mode', 'end_mode', 'starts_at_local', 'ends_at_local', 'timezone', 'execution_window_seconds'].includes(String(field.name[0])))) setAdvancedOpen(true);
      }}>
      {(hydrationLoading || hydrationError) && (
        <Alert
          style={{ gridColumn: '1 / -1' }}
          type={hydrationError ? 'error' : 'info'}
          showIcon
          message={hydrationError ? '加载自动化详情失败' : '正在加载完整自动化配置'}
          description={hydrationError || '完整投递配置加载完成后才能编辑、保存或预览，现有收件人不会被清除。'}
          action={hydrationError ? <Button disabled={false} size="small" onClick={retryHydration}>重试加载详情</Button> : undefined}
        />
      )}
      <div className="automation-editor__composer" hidden={mobile && view !== 'compose'}>
        <div className="automation-editor__intro"><span className="automation-editor__eyebrow">自动化</span><h2>把重复的事，交给智能体</h2><p>写下任务描述，按你的节奏自动执行。</p></div>
        <Form.Item name="name" label="自动化名称" rules={[{ required: true, whitespace: true, message: '请输入自动化名称' }, { max: 200, message: '名称过长' }]}>
          <Input placeholder="例如：每日销售日报" maxLength={200} />
        </Form.Item>
        <Form.Item name="prompt" label="任务描述" className="automation-editor__prompt" rules={[{ required: true, whitespace: true, message: '请输入每次执行要发送的指令' }]} extra="每次执行都会把这段描述作为新消息发送给智能体。">
          <Input.TextArea rows={mobile ? 7 : 10} placeholder="希望智能体帮你做什么？描述任务、关注的信息，以及期望的输出…" />
        </Form.Item>
        {mobile && <nav aria-label="自动化配置" className="automation-editor__navigation">
          <button type="button" className="automation-editor__row" aria-label="配置智能体" onClick={() => onNavigate?.('agent')}><RobotOutlined /><span>智能体</span><span className="automation-editor__row-value">{agentSummary}<RightOutlined /></span></button>
          <button type="button" className="automation-editor__row" aria-label="配置触发器" onClick={() => onNavigate?.('trigger')}><ClockCircleOutlined /><span>触发器</span><span className="automation-editor__row-value">{triggerSummary}<RightOutlined /></span></button>
          <button type="button" className="automation-editor__row" aria-label="配置推送" onClick={() => onNavigate?.('delivery')}><SendOutlined /><span>推送配置</span><span className="automation-editor__row-value">{deliverySummary}<RightOutlined /></span></button>
        </nav>}
      </div>
      <section className="automation-editor__agent" aria-label="智能体配置" hidden={mobile && view !== 'agent'}>
        <Form.Item
          name="application_id"
          label="选择智能体"
          rules={[{ required: true, message: '请选择一个智能体' }]}
        >
          {/* 服务端搜索 + 分页（二次复审 P1-2）：filterOption=false 让搜索词
              直发后端（覆盖全部智能体，而不只是已加载页）；下拉滚到底再拉
              下一页，>50 个可调度智能体也不会被截断。notFoundContent 区分
              loading / empty / error（三次复审 §39）。searchValue 受控
              （五次复审 §40）：编辑器会话切换清空 appQuery 时，Select 内部
              搜索文本同步清空。 */}
          <Select
            loading={appsLoading}
            placeholder="选择要定时执行的智能体"
            showSearch
            filterOption={false}
            searchValue={appQuery}
            onSearch={setAppQuery}
            onPopupScroll={(e) => {
              const { scrollTop, scrollHeight, clientHeight } = e.currentTarget;
              if (appsHasMore
                && !appsLoadingMore
                && scrollHeight - scrollTop - clientHeight < 24) {
                void loadMoreApps();
              }
            }}
            notFoundContent={
              appsError
                ? '加载智能体失败'
                : appsLoading
                  ? '搜索中…'
                  : '没有匹配的智能体'
            }
            options={apps.map((a) => ({
              value: a.id,
              label: a.name,
            }))}
          />
        </Form.Item>
        {/* 列表请求失败 ≠ 没有智能体（三次复审 §38–§39）：错误可见 + 重试。 */}
        {appsError && (
          <Alert
            type="error"
            showIcon
            message="加载智能体失败"
            description={appsError}
            action={<Button size="small" onClick={() => void refreshApps()}>重试</Button>}
            style={{ marginBottom: 16 }}
          />
        )}
        {/* 回填状态（§40–§42）：404 = 原智能体不可用，必须换一个；
            5xx / 网络 = 临时故障，给重试而不是伪装成「智能体 #id」。
            告警只对「当前仍选中的原智能体」生效（四次复审 P2-4）—— 用户
            已换成新 Agent 后，旧 Agent 的告警立即隐藏。 */}
        {resolutionApplies && appResolution === 'unavailable' && (
          <Alert
            type="warning"
            showIcon
            message="原智能体当前不可用，请选择新的智能体"
            style={{ marginBottom: 16 }}
          />
        )}
        {resolutionApplies && appResolution === 'transient-error' && (
          <Alert
            type="error"
            showIcon
            message="无法加载智能体信息"
            action={<Button size="small" onClick={retryResolveApp}>重试</Button>}
            style={{ marginBottom: 16 }}
          />
        )}
      </section>
      <div className="automation-editor__configuration" hidden={mobile && view !== 'trigger' && view !== 'delivery'}>
        <section className="automation-editor__section" aria-label="触发器配置" hidden={mobile && view !== 'trigger'}>
          <h3><ClockCircleOutlined />触发配置</h3>
        <Form.Item name="schedule_type" label="频率" rules={[{ required: true }]}>
          <Radio.Group
            onChange={(e) => {
              setScheduleType(e.target.value as ScheduleType);
              setPreview([]);
            }}
            options={[
              { value: 'once', label: '单次' },
              { value: 'daily', label: '每天' },
              { value: 'weekly', label: '每周' },
              { value: 'monthly', label: '每月' },
            ]}
            optionType="button"
            buttonStyle="solid"
          />
        </Form.Item>

        {scheduleType === 'once' ? (
          <Form.Item
            name="run_at_local"
            label="执行时间"
            rules={[{ required: true, message: '请选择执行时间' }]}
          >
            <Input type="datetime-local" step={60} aria-label="执行时间" />
          </Form.Item>
        ) : (
          <>
            <Form.Item
              name={['trigger', 'time']}
              label="执行时刻"
              rules={[{ required: true, message: '请选择执行时刻' }]}
            >
              <Input type="time" step={60} aria-label="执行时刻" />
            </Form.Item>
            {scheduleType === 'weekly' && (
              <Form.Item name={['trigger', 'days_of_week']} label="星期" rules={[{ required: true, message: '至少选择一天' }]}>
                <Checkbox.Group options={WEEKDAY_LABELS.map((label, idx) => ({ value: idx, label }))} />
              </Form.Item>
            )}
            {scheduleType === 'monthly' && (
              <Form.Item
                name={['trigger', 'day_of_month']}
                label="日期"
                rules={[{ required: true, message: '请选择日期' }]}
                extra="遇短月自动顺延到当月最后一天"
              >
                <InputNumber min={1} max={31} style={{ width: 120 }} />
              </Form.Item>
            )}
          </>
        )}

        <button
          type="button"
          className="schedule-preview-btn"
          onClick={onPreview ?? refreshPreview}
          disabled={previewing}
        >
          {previewing ? '计算中…' : '预览未来执行时间'}
        </button>
        {preview.length > 0 && (
          <ul className="schedule-preview-list" aria-live="polite">
            {preview.map((t) => (
              <li key={t}><time dateTime={t}>{new Date(t).toLocaleString()}</time></li>
            ))}
          </ul>
        )}
          {mobile && <ScheduleBoundaryFields form={form} mobile active />}
          <details className="automation-editor__advanced" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
            <summary>高级配置</summary>
            <div className="automation-editor__advanced-content">
              {!mobile && <ScheduleBoundaryFields form={form} active />}
        <Form.Item name="timezone" label="时区" rules={[{ required: true }]}>
          <Select
            options={['Asia/Shanghai', 'UTC', 'Asia/Tokyo', 'Asia/Singapore', 'America/New_York', 'Europe/London'].map((tz) => ({ value: tz, label: tz }))}
          />
        </Form.Item>

        <Form.Item name="conversation_policy" label="会话方式" extra="建议每次新建会话，避免上下文无限累积">
          <Radio.Group
            options={[
              { value: 'new_each_run', label: '每次新建会话' },
              { value: 'reuse', label: '沿用首次会话' },
            ]}
          />
        </Form.Item>
        <Form.Item name="overlap_policy" label="上一轮未完成时">
          <Radio.Group
            options={[
              { value: 'queue', label: '排队等待' },
              { value: 'skip', label: '跳过本轮' },
            ]}
          />
        </Form.Item>
              <Form.Item name="misfire_policy" label="错过执行时间时"><Radio.Group options={[{ value: 'fire_once', label: '补执行一次' }, { value: 'skip', label: '跳过' }]} /></Form.Item>
              <Form.Item name="execution_window_seconds" label="允许延迟执行的时间（秒）" extra="0 表示不限制延迟时间" rules={[{ type: 'number', min: 0, message: '请输入不小于 0 的秒数' }]}><InputNumber min={0} precision={0} /></Form.Item>
              <Form.Item name="deadline_policy" label="超过允许延迟时"><Radio.Group options={[{ value: 'execute_anyway', label: '仍然执行' }, { value: 'skip', label: '跳过' }]} /></Form.Item>
            </div>
          </details>
        </section>
        <section className="automation-editor__section" aria-label="推送配置" hidden={mobile && view !== 'delivery'}>
          <h3><SendOutlined />推送配置</h3>
        <Form.Item label="执行完成后发送飞书消息">
          <Switch checked={deliveryOn} onChange={setDeliveryOn} aria-label="开启飞书投递" />
        </Form.Item>
        <div hidden={!deliveryOn}>
            <Form.Item
              name={['deliveries', 0, 'target_id']}
              label="投递目标"
              rules={[{ required: deliveryOn, message: '请选择投递目标' }]}
              extra="输入姓名搜索联系人；群聊列表自动展示"
            >
              {/* 远程搜索（五次复审 P1-3）：不再预拉「前 20 个联系人 + 前
                  100 个群聊」后本地过滤 —— 第 21 个人/第 101 个群聊曾永远
                  选不到。现在 user 搜索词直发后端目录搜索 + cursor 续拉，
                  chat 一个会话只拉一次全量、搜索词在本地完整数据集上过
                  滤；已选目标始终注入 options，
                  搜索词变化后 Select 不显示裸 id。
                  联系人 cursor 分页（六次复审 P1-3）：滚到底续拉下一页 ——
                  匹配的第 51+ 人不再被第一页截断（chat 恒全量、无续拉）。 */}
              <Select
                loading={targetsLoading}
                showSearch
                filterOption={false}
                searchValue={targetQuery}
                onSearch={setTargetQuery}
                onPopupScroll={(e) => {
                  const { scrollTop, scrollHeight, clientHeight } = e.currentTarget;
                  if (targetsHasMore
                    && !targetsLoadingMore
                    && scrollHeight - scrollTop - clientHeight < 24) {
                    void loadMoreTargets();
                  }
                }}
                placeholder="搜索并选择飞书用户或群聊"
                notFoundContent={
                  targetsError
                    ? '加载投递目标失败'
                    : targetsLoading
                      ? '搜索中…'
                      : targetQuery.trim()
                        ? '没有匹配的目标'
                        : '输入姓名可搜索联系人'
                }
                options={targets.map((t) => ({
                  value: t.id,
                  label: `${t.target_type === 'chat' ? '[群聊] ' : ''}${t.name}`,
                }))}
                onSelect={(value) => {
                  const target = targets.find((t) => t.id === value);
                  if (target) {
                    setSelectedTarget(target);
                    // Update only the first target; preserve conditions and all other targets.
                    form.setFieldValue(['deliveries', 0, 'target_id'], target.id);
                    form.setFieldValue(['deliveries', 0, 'target_name'], target.name);
                    form.setFieldValue(['deliveries', 0, 'target_type'], target.target_type);
                  }
                  // 选中后清空搜索词：受控 searchValue 下必须显式清，
                  // 同时让候选回到默认群聊列表。
                  setTargetQuery('');
                }}
              />
            </Form.Item>
            {/* 投递目标加载失败 ≠ 没有目标（五次复审 §28，ERROR ≠ EMPTY）：
                全部失败给 error + 重试；部分失败（如联系人接口挂了但群聊
                正常）仍展示可用部分，给 warning + 重试。 */}
            {(targetsError || targetsPartialFailed) && (
              <Alert
                type={targetsError ? 'error' : 'warning'}
                showIcon
                message={targetsError ? '加载飞书投递目标失败' : '部分飞书目标加载失败，仅显示可用部分'}
                description={targetsError ?? undefined}
                action={<Button size="small" onClick={() => void refreshTargets()}>重试</Button>}
                style={{ marginBottom: 16 }}
              />
            )}
            {/* 联系人续拉失败 ≠ 数据源失败（七次复审 P1-2）：已加载的前几页
                仍然有效、候选仍然可选 —— 只提示「更多加载失败」，重试从断点
                续拉（loadMoreTargets），不回第一页丢掉已加载进度。 */}
            {targetsLoadMoreError && (
              <Alert
                type="warning"
                showIcon
                message="更多联系人加载失败，已加载的目标仍可选择"
                description={targetsLoadMoreError}
                action={<Button size="small" onClick={() => void loadMoreTargets()}>重试</Button>}
                style={{ marginBottom: 16 }}
              />
            )}
            <Form.Item name={['deliveries', 0, 'condition', 'operator']} label="推送条件" initialValue="always">
              <Select options={[
                { value: 'always', label: '始终推送' },
                { value: 'contains', label: '回复包含指定文本' },
                { value: 'not_contains', label: '回复不包含指定文本' },
              ]} />
            </Form.Item>
            <Form.Item name={['deliveries', 0, 'condition', 'text']} label="指定文本" hidden={condition === 'always'} rules={[{ required: deliveryOn && condition !== 'always', whitespace: true, message: '请输入用于匹配的指定文本' }]} extra="匹配最终回复中的完整文本，区分大小写。">
              <Input placeholder="例如：发现需要关注的异常" />
            </Form.Item>
            {deliveryCount > 1 && <Alert type="info" showIcon message={`已保留全部 ${deliveryCount} 个推送目标；此处仅修改第一个目标及其条件。`} />}
            <Alert
              type="info"
              showIcon
              message="消息将以你的身份发送，内容为本次执行的最终回复。"
            />
          </div>
        </section>
      </div>
    </Form>
  );
}
