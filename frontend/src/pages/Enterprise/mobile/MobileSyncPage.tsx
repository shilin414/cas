/**
 * MobileSyncPage — extensible enterprise sync center for small screens.
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Form,
  Input,
  InputNumber,
  Select,
  Skeleton,
  Switch,
  Tag,
  TimePicker,
  message,
} from 'antd';
import type { SyncTargetConfig } from '../enterpriseApi';
import { fmt } from '../enterpriseNav';
import { formatSyncDuration } from '../syncDuration';
import {
  formatMetricLabel,
  loadSyncJobs,
  loadSyncTargets,
  saveSyncTargetConfig,
  syncTriggerLabel,
  triggerAllSyncTargets,
  triggerOneSyncTarget,
  type SyncJobView,
  type SyncTargetView,
} from '../syncManagement';
import {
  reconcileSyncTargetForm,
  syncTargetControlId,
  syncTargetFormName,
  syncTargetFormValues,
  type SyncTargetFormValues,
} from '../syncTargetForm';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { MobilePage, MobileSection } from '@/components/MobileConsole';
import '../EnterpriseMobile.css';

const STATUS_TAG: Record<SyncJobView['status'], { color: string; label: string }> = {
  pending: { color: 'default', label: '等待中' },
  blocked: { color: 'warning', label: '等待依赖' },
  running: { color: 'processing', label: '进行中' },
  success: { color: 'success', label: '成功' },
  failed: { color: 'error', label: '失败' },
};

function MobileTargetSection({
  target,
  latestJob,
  canManage,
  onSaved,
  onTriggered,
}: {
  target: SyncTargetView;
  latestJob?: SyncJobView;
  canManage: boolean;
  onSaved: (target: SyncTargetView, values: SyncTargetFormValues) => Promise<SyncTargetConfig>;
  onTriggered: (target: SyncTargetView) => Promise<void>;
}) {
  const [form] = Form.useForm<SyncTargetFormValues>();
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const canMutate = canManage && !target.legacyFallback;
  const formName = syncTargetFormName(target.code, 'mobile');

  useEffect(() => {
    reconcileSyncTargetForm(form, target.config);
  }, [form, target.config]);

  const save = async () => {
    if (!canMutate) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const config = await onSaved(target, values);
      reconcileSyncTargetForm(form, config, { force: true });
    } catch {
      // Parent owns user-visible mutation errors; keep this form dirty.
    } finally {
      setSaving(false);
    }
  };

  const trigger = async () => {
    if (!canMutate) return;
    setSyncing(true);
    try {
      await onTriggered(target);
    } catch {
      // Parent owns user-visible mutation errors.
    } finally {
      setSyncing(false);
    }
  };

  const targetError = target.config.last_error_message
    || (latestJob?.status === 'failed' ? latestJob.errorMessage : '');

  return (
    <MobileSection title={target.displayName}>
      <div className="mobile-sync__target-head">
        <Tag>{target.code}</Tag>
        {target.legacyFallback && <Tag color="warning">组合只读</Tag>}
        {latestJob && (
          <Tag color={STATUS_TAG[latestJob.status].color}>
            {STATUS_TAG[latestJob.status].label}
          </Tag>
        )}
      </div>
      <p className="mobile-sync__target-description">{target.description}</p>
      <div className="mobile-sync__status">
        <div className="mobile-sync__status-line mobile-sync__status-line--dim">
          版本：{target.config.target_version}
        </div>
        <div className="mobile-sync__status-line mobile-sync__status-line--dim">
          最近成功：{fmt(target.config.last_success_at)}
        </div>
        <div className="mobile-sync__status-line mobile-sync__status-line--dim">
          下一次：{fmt(target.config.next_run_at)}
        </div>
        {target.dependencies.length > 0 && (
          <div className="mobile-sync__status-line mobile-sync__status-line--dim">
            依赖：{target.dependencies.join(' → ')}
          </div>
        )}
        {targetError && (
          <div className="mobile-sync__status-line" style={{ color: 'var(--color-error)' }}>
            {targetError}
          </div>
        )}
      </div>
      <Button
        type="primary"
        block
        disabled={!canMutate}
        loading={syncing}
        className="mobile-sync__trigger"
        aria-label={`立即同步${target.displayName}`}
        onClick={() => void trigger()}
      >
        立即同步
      </Button>
      <Form<SyncTargetFormValues>
        id={`${formName}-form`}
        name={formName}
        className="mobile-sync__target-form"
        disabled={!canMutate}
        form={form}
        layout="vertical"
        initialValues={syncTargetFormValues(target.config)}
      >
        <Form.Item
          name="enabled"
          valuePropName="checked"
          label="启用自动同步"
          htmlFor={syncTargetControlId(target.code, 'mobile', 'enabled')}
        >
          <Switch
            id={syncTargetControlId(target.code, 'mobile', 'enabled')}
            aria-label={`${target.displayName}启用自动同步`}
          />
        </Form.Item>
        <Form.Item
          name="schedule_type"
          label="同步方式"
          htmlFor={syncTargetControlId(target.code, 'mobile', 'schedule_type')}
        >
          <Select
            id={syncTargetControlId(target.code, 'mobile', 'schedule_type')}
            aria-label={`${target.displayName}同步方式`}
            options={[
              { value: 'interval', label: '按间隔' },
              { value: 'daily', label: '每天' },
            ]}
          />
        </Form.Item>
        <Form.Item noStyle shouldUpdate>
          {({ getFieldValue }) => getFieldValue('schedule_type') === 'daily' ? (
            <Form.Item
              name="daily_time"
              label="执行时间"
              htmlFor={syncTargetControlId(target.code, 'mobile', 'daily_time')}
            >
              <TimePicker
                id={syncTargetControlId(target.code, 'mobile', 'daily_time')}
                aria-label={`${target.displayName}执行时间`}
                format="HH:mm"
                style={{ width: '100%' }}
              />
            </Form.Item>
          ) : (
            <Form.Item
              name="interval_minutes"
              label="间隔（分钟）"
              htmlFor={syncTargetControlId(target.code, 'mobile', 'interval_minutes')}
            >
              <InputNumber
                id={syncTargetControlId(target.code, 'mobile', 'interval_minutes')}
                aria-label={`${target.displayName}同步间隔分钟数`}
                min={15}
                max={10080}
                style={{ width: '100%' }}
              />
            </Form.Item>
          )}
        </Form.Item>
        <Form.Item
          name="timezone"
          label="时区"
          htmlFor={syncTargetControlId(target.code, 'mobile', 'timezone')}
        >
          <Input
            id={syncTargetControlId(target.code, 'mobile', 'timezone')}
            aria-label={`${target.displayName}同步时区`}
          />
        </Form.Item>
        <Button
          type="primary"
          block
          disabled={!canMutate}
          loading={saving}
          aria-label={`保存${target.displayName}设置`}
          onClick={() => void save()}
        >
          保存设置
        </Button>
      </Form>
    </MobileSection>
  );
}

export default function MobileSyncPage() {
  const canManage = useAdminPermissionStore((state) => Boolean(
    state.identity === null
    || state.identity?.is_super_admin
    || state.identity?.permissions.some((item) => item.code === 'directory.sync.manage'),
  ));
  const [targets, setTargets] = useState<SyncTargetView[]>([]);
  const [jobs, setJobs] = useState<SyncJobView[]>([]);
  const [targetsError, setTargetsError] = useState<string | null>(null);
  const [jobsError, setJobsError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [syncingAll, setSyncingAll] = useState(false);
  const loadSeqRef = useRef(0);
  const jobsSeqRef = useRef(0);

  const load = useCallback(async (fresh = false) => {
    const seq = ++loadSeqRef.current;
    const jobsSeq = ++jobsSeqRef.current;
    setRefreshing(true);
    setTargetsError(null);
    setJobsError(null);
    const options = fresh ? { fresh: true } : undefined;
    const [targetResult, jobResult] = await Promise.allSettled([
      loadSyncTargets(options),
      loadSyncJobs(50, options),
    ]);
    if (seq !== loadSeqRef.current) return;
    if (targetResult.status === 'fulfilled') setTargets(targetResult.value);
    else setTargetsError('加载同步目标失败');
    if (jobsSeq === jobsSeqRef.current) {
      if (jobResult.status === 'fulfilled') setJobs(jobResult.value);
      else setJobsError('加载同步记录失败');
    }
    setRefreshing(false);
  }, []);

  const refreshJobs = useCallback(async () => {
    const seq = ++jobsSeqRef.current;
    try {
      const freshJobs = await loadSyncJobs(50, { fresh: true });
      if (seq === jobsSeqRef.current) {
        setJobs(freshJobs);
        setJobsError(null);
      }
    } catch {
      if (seq === jobsSeqRef.current) setJobsError('刷新同步记录失败');
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const latestJobs = useMemo(() => jobs.reduce<Record<string, SyncJobView>>((latest, job) => {
    if (!latest[job.targetCode]) latest[job.targetCode] = job;
    return latest;
  }, {}), [jobs]);
  const targetsByCode = useMemo(() => Object.fromEntries(
    targets.map((target) => [target.code, target]),
  ), [targets]);
  const legacyFallback = targets.some((target) => target.legacyFallback)
    || jobs.some((job) => job.legacyFallback);

  const saveTarget = async (
    target: SyncTargetView,
    values: SyncTargetFormValues,
  ): Promise<SyncTargetConfig> => {
    // A full refresh started before this mutation must never overwrite the
    // target-specific config returned by the save.
    loadSeqRef.current += 1;
    setRefreshing(false);
    try {
      const config = await saveSyncTargetConfig(target.code, {
        enabled: values.enabled,
        schedule_type: values.schedule_type,
        interval_minutes: values.interval_minutes,
        daily_time: values.daily_time.format('HH:mm'),
        timezone: values.timezone,
      });
      setTargets((current) => current.map((item) => item.code === target.code
        ? { ...item, config }
        : item));
      message.success(`${target.displayName}设置已保存`);
      return config;
    } catch (error) {
      message.error('保存同步设置失败');
      throw error;
    }
  };

  const triggerTarget = async (target: SyncTargetView) => {
    try {
      await triggerOneSyncTarget(target.code);
      message.success(`${target.displayName}同步任务已进入队列`);
      await refreshJobs();
    } catch (error) {
      message.error('发起同步失败');
      throw error;
    }
  };

  const triggerAll = async () => {
    if (!canManage || targets.length === 0 || legacyFallback) return;
    setSyncingAll(true);
    try {
      await triggerAllSyncTargets(targets.map((target) => target.code));
      message.success('同步全部任务已进入队列');
      await refreshJobs();
    } catch {
      message.error('发起同步全部失败');
    } finally {
      setSyncingAll(false);
    }
  };

  return (
    <MobilePage>
      <MobileSection title="同步中心">
        <p className="mobile-sync__intro">
          每个同步目标独立配置和运行；同步全部会按依赖顺序创建任务。
        </p>
        <Button
          loading={refreshing}
          block
          aria-label="刷新同步管理"
          onClick={() => void load(true)}
        >
          刷新
        </Button>
        <Button
          type="primary"
          block
          loading={syncingAll}
          disabled={!canManage || targets.length === 0 || legacyFallback}
          className="mobile-sync__trigger"
          aria-label="同步全部目标"
          onClick={() => void triggerAll()}
        >
          同步全部
        </Button>
      </MobileSection>

      {legacyFallback && (
        <MobileSection title="兼容只读模式">
          <Alert
            type="warning"
            showIcon
            message="旧接口只支持通讯录与用户组组合配置、组合执行"
            description="为避免单目标按钮误触发组合任务，所有同步写操作已停用。"
          />
        </MobileSection>
      )}

      {targetsError && targets.length > 0 && (
        <MobileSection title="配置状态">
          <Alert
            type="warning"
            showIcon
            message="刷新失败，当前显示的是上次已加载数据；未保存的表单内容已保留"
            action={<Button size="small" onClick={() => void load(true)}>重试</Button>}
          />
        </MobileSection>
      )}

      {targets.length === 0 ? (
        <MobileSection title="同步目标">
          {targetsError ? (
            <div className="mobile-console-empty">
              <strong>无法加载同步配置</strong>
              {targetsError}
              <Button loading={refreshing} onClick={() => void load(true)}>重试</Button>
            </div>
          ) : (
            <Skeleton active />
          )}
        </MobileSection>
      ) : targets.map((target) => (
        <MobileTargetSection
          key={target.code}
          target={target}
          latestJob={latestJobs[target.code]}
          canManage={canManage && !legacyFallback}
          onSaved={saveTarget}
          onTriggered={triggerTarget}
        />
      ))}

      <MobileSection title="同步任务记录" flush>
        {jobsError && (
          <div className="mobile-console-empty">
            {jobs.length > 0 ? '刷新记录失败，以下为上次已加载数据' : jobsError}
            <Button size="small" onClick={() => void refreshJobs()}>重试</Button>
          </div>
        )}
        {jobs.length === 0 && !jobsError ? (
          <div className="mobile-console-empty">还没有同步任务</div>
        ) : jobs.map((job) => {
          const target = targetsByCode[job.targetCode];
          return (
            <div key={String(job.id)} className="mobile-sync__run">
              <div className="mobile-sync__run-head">
                <span className="mobile-sync__run-title">
                  <Tag color={STATUS_TAG[job.status].color}>{STATUS_TAG[job.status].label}</Tag>
                  <strong>{target?.displayName || job.targetCode}</strong>
                </span>
                <time dateTime={job.createdAt}>{fmt(job.createdAt)}</time>
              </div>
              <div className="mobile-sync__run-meta">
                <span>{syncTriggerLabel(job.triggerType)}</span>
                <span>版本 {job.targetVersion}</span>
                <span>耗时 {formatSyncDuration(job.startedAt, job.finishedAt)}</span>
                {job.batchId && <span>批次 {String(job.batchId)}</span>}
              </div>
              {Object.keys(job.metrics).length > 0 && (
                <div className="mobile-sync__metrics">
                  {Object.entries(job.metrics).map(([key, value]) => (
                    <span key={key}>{formatMetricLabel(key)} {value}</span>
                  ))}
                </div>
              )}
              {job.warnings.length > 0 && (
                <div className="mobile-sync__job-message mobile-sync__job-message--warning">
                  {job.warnings.join('；')}
                </div>
              )}
              {job.errorMessage && (
                <div className="mobile-sync__job-message mobile-sync__job-message--error">
                  {job.errorMessage}
                </div>
              )}
            </div>
          );
        })}
      </MobileSection>
    </MobilePage>
  );
}
