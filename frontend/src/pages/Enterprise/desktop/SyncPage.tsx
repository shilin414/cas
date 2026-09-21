/**
 * SyncPage — extensible enterprise sync center (desktop).
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  InputNumber,
  Select,
  Skeleton,
  Space,
  Switch,
  Table,
  Tag,
  TimePicker,
  message,
} from "antd";
import { fmt } from "../enterpriseNav";
import { formatSyncDuration } from "../syncDuration";
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
} from "../syncManagement";
import {
  reconcileSyncTargetForm,
  syncTargetControlId,
  syncTargetFormName,
  syncTargetFormValues,
  type SyncTargetFormValues,
} from "../syncTargetForm";
import type { SyncTargetConfig } from "../enterpriseApi";
import { useAdminPermissionStore } from "@/stores/useAdminPermissionStore";

const STATUS_TAG: Record<SyncJobView["status"], { color: string; label: string }> = {
  pending: { color: "default", label: "等待中" },
  blocked: { color: "warning", label: "等待依赖" },
  running: { color: "processing", label: "进行中" },
  success: { color: "success", label: "成功" },
  failed: { color: "error", label: "失败" },
};

function TargetScheduleCard({
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
  const formName = syncTargetFormName(target.code, "desktop");

  useEffect(() => {
    // A manual/global refresh may update target metadata while an admin is
    // editing this card. Never replace touched values with server data.
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
    || (latestJob?.status === "failed" ? latestJob.errorMessage : "");

  return (
    <Card
      className="enterprise-sync-target"
      title={(
        <Space wrap>
          <span>{target.displayName}</span>
          <Tag>{target.code}</Tag>
          {target.legacyFallback && <Tag color="warning">组合只读</Tag>}
          {latestJob && (
            <Tag color={STATUS_TAG[latestJob.status].color}>
              {STATUS_TAG[latestJob.status].label}
            </Tag>
          )}
        </Space>
      )}
      extra={(
        <Button
          type="primary"
          disabled={!canMutate}
          loading={syncing}
          aria-label={`立即同步${target.displayName}`}
          onClick={() => void trigger()}
        >
          立即同步
        </Button>
      )}
    >
      <p className="enterprise-sync-target__description">{target.description}</p>
      <div className="enterprise-sync-target__facts">
        <span>版本：{target.config.target_version}</span>
        <span>最近成功：{fmt(target.config.last_success_at)}</span>
        <span>下次执行：{fmt(target.config.next_run_at)}</span>
        {target.dependencies.length > 0 && <span>依赖：{target.dependencies.join(" → ")}</span>}
      </div>
      {targetError && (
        <Alert
          type="error"
          showIcon
          message={targetError}
          description={target.config.last_error_code || undefined}
          style={{ marginTop: 12 }}
        />
      )}
      <Form<SyncTargetFormValues>
        id={`${formName}-form`}
        name={formName}
        className="enterprise-sync-target__form"
        disabled={!canMutate}
        form={form}
        layout="vertical"
        initialValues={syncTargetFormValues(target.config)}
      >
        <Form.Item
          name="enabled"
          valuePropName="checked"
          label="启用自动同步"
          htmlFor={syncTargetControlId(target.code, "desktop", "enabled")}
        >
          <Switch
            id={syncTargetControlId(target.code, "desktop", "enabled")}
            aria-label={`${target.displayName}启用自动同步`}
          />
        </Form.Item>
        <Form.Item
          name="schedule_type"
          label="同步方式"
          htmlFor={syncTargetControlId(target.code, "desktop", "schedule_type")}
        >
          <Select
            id={syncTargetControlId(target.code, "desktop", "schedule_type")}
            aria-label={`${target.displayName}同步方式`}
            options={[
              { value: "interval", label: "按间隔" },
              { value: "daily", label: "每天" },
            ]}
          />
        </Form.Item>
        <Form.Item noStyle shouldUpdate>
          {({ getFieldValue }) => getFieldValue("schedule_type") === "daily" ? (
            <Form.Item
              name="daily_time"
              label="执行时间"
              htmlFor={syncTargetControlId(target.code, "desktop", "daily_time")}
            >
              <TimePicker
                id={syncTargetControlId(target.code, "desktop", "daily_time")}
                aria-label={`${target.displayName}执行时间`}
                format="HH:mm"
                style={{ width: "100%" }}
              />
            </Form.Item>
          ) : (
            <Form.Item
              name="interval_minutes"
              label="间隔（分钟）"
              htmlFor={syncTargetControlId(target.code, "desktop", "interval_minutes")}
            >
              <InputNumber
                id={syncTargetControlId(target.code, "desktop", "interval_minutes")}
                aria-label={`${target.displayName}同步间隔分钟数`}
                min={15}
                max={10080}
                style={{ width: "100%" }}
              />
            </Form.Item>
          )}
        </Form.Item>
        <Form.Item
          name="timezone"
          label="时区"
          htmlFor={syncTargetControlId(target.code, "desktop", "timezone")}
        >
          <Input
            id={syncTargetControlId(target.code, "desktop", "timezone")}
            aria-label={`${target.displayName}同步时区`}
          />
        </Form.Item>
        <Form.Item label=" ">
          <Button
            type="primary"
            disabled={!canMutate}
            loading={saving}
            aria-label={`保存${target.displayName}设置`}
            onClick={() => void save()}
          >
            保存设置
          </Button>
        </Form.Item>
      </Form>
    </Card>
  );
}

function MetricTags({ job }: { job: SyncJobView }) {
  const entries = Object.entries(job.metrics);
  if (entries.length === 0) return <span>—</span>;
  return (
    <Space size={[4, 4]} wrap>
      {entries.map(([key, value]) => (
        <Tag key={key}>{formatMetricLabel(key)} {value}</Tag>
      ))}
    </Space>
  );
}

export default function SyncPage() {
  const canManage = useAdminPermissionStore((state) => Boolean(
    state.identity === null
    || state.identity?.is_super_admin
    || state.identity?.permissions.some((item) => item.code === "directory.sync.manage"),
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
    if (targetResult.status === "fulfilled") setTargets(targetResult.value);
    else setTargetsError("加载同步目标失败");
    if (jobsSeq === jobsSeqRef.current) {
      if (jobResult.status === "fulfilled") setJobs(jobResult.value);
      else setJobsError("加载同步记录失败");
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
      if (seq === jobsSeqRef.current) setJobsError("刷新同步记录失败");
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
        daily_time: values.daily_time.format("HH:mm"),
        timezone: values.timezone,
      });
      setTargets((current) => current.map((item) => item.code === target.code
        ? { ...item, config }
        : item));
      message.success(`${target.displayName}设置已保存`);
      return config;
    } catch (error) {
      message.error("保存同步设置失败");
      throw error;
    }
  };

  const triggerTarget = async (target: SyncTargetView) => {
    try {
      await triggerOneSyncTarget(target.code);
      message.success(`${target.displayName}同步任务已进入队列`);
      await refreshJobs();
    } catch (error) {
      message.error("发起同步失败");
      throw error;
    }
  };

  const triggerAll = async () => {
    if (!canManage || targets.length === 0 || legacyFallback) return;
    setSyncingAll(true);
    try {
      await triggerAllSyncTargets(targets.map((target) => target.code));
      message.success("同步全部任务已进入队列");
      await refreshJobs();
    } catch {
      message.error("发起同步全部失败");
    } finally {
      setSyncingAll(false);
    }
  };

  return (
    <section className="enterprise-section">
      <div className="enterprise-section__head">
        <div>
          <h2>同步管理</h2>
          <p>每类企业数据独立配置、运行和观察；同步全部会按依赖顺序创建批次任务。</p>
        </div>
        <Space>
          <Button
            loading={refreshing}
            aria-label="刷新同步管理"
            onClick={() => void load(true)}
          >
            刷新
          </Button>
          <Button
            type="primary"
            disabled={!canManage || targets.length === 0 || legacyFallback}
            loading={syncingAll}
            aria-label="同步全部目标"
            onClick={() => void triggerAll()}
          >
            同步全部
          </Button>
        </Space>
      </div>

      {legacyFallback && (
        <Alert
          type="warning"
          showIcon
          message="兼容只读模式：旧接口只支持组织通讯录与用户组组合配置、组合执行"
          description="为避免单目标操作误触发组合任务，保存、立即同步和同步全部已停用。升级后端同步目标 API 后可恢复独立操作。"
        />
      )}

      {targetsError && targets.length > 0 && (
        <Alert type="warning" showIcon message="刷新失败，当前显示的是上次已加载配置；未保存的表单内容已保留" />
      )}

      {targets.length === 0 ? (
        targetsError ? (
          <Card>
            <Empty description={targetsError}>
              <Button loading={refreshing} onClick={() => void load(true)}>重试</Button>
            </Empty>
          </Card>
        ) : (
          <Card><Skeleton active /></Card>
        )
      ) : (
        <div className="enterprise-sync-target-grid">
          {targets.map((target) => (
            <TargetScheduleCard
              key={target.code}
              target={target}
              latestJob={latestJobs[target.code]}
              canManage={canManage && !legacyFallback}
              onSaved={saveTarget}
              onTriggered={triggerTarget}
            />
          ))}
        </div>
      )}

      <Card title="同步任务记录">
        {jobsError && (
          <Alert
            type="warning"
            showIcon
            message={jobs.length > 0 ? "刷新记录失败，当前显示的是上次已加载数据" : jobsError}
            action={<Button size="small" onClick={() => void refreshJobs()}>重试</Button>}
            style={{ marginBottom: 16 }}
          />
        )}
        <Table<SyncJobView>
          rowKey={(row) => String(row.id)}
          dataSource={jobs}
          pagination={false}
          locale={{ emptyText: jobsError ? "同步记录加载失败" : "还没有同步任务" }}
          scroll={{ x: 1120 }}
          columns={[
            { title: "入队时间", dataIndex: "createdAt", width: 180, render: fmt },
            {
              title: "同步目标",
              dataIndex: "targetCode",
              width: 160,
              render: (code: string) => (
                <Space size={4} wrap>
                  <span>{targetsByCode[code]?.displayName || code}</span>
                  <Tag>{code}</Tag>
                </Space>
              ),
            },
            {
              title: "状态",
              dataIndex: "status",
              width: 100,
              render: (status: SyncJobView["status"]) => (
                <Tag color={STATUS_TAG[status].color}>{STATUS_TAG[status].label}</Tag>
              ),
            },
            { title: "触发", dataIndex: "triggerType", width: 90, render: syncTriggerLabel },
            { title: "版本", dataIndex: "targetVersion", width: 80 },
            {
              title: "耗时",
              width: 110,
              render: (_, row) => formatSyncDuration(row.startedAt, row.finishedAt),
            },
            { title: "指标", render: (_, row) => <MetricTags job={row} /> },
            {
              title: "告警 / 错误",
              width: 260,
              render: (_, row) => {
                const details = [...row.warnings, row.errorMessage].filter(Boolean);
                return details.length > 0 ? details.join("；") : "—";
              },
            },
            {
              title: "批次",
              dataIndex: "batchId",
              width: 120,
              render: (batchId) => batchId ? String(batchId) : "—",
            },
          ]}
        />
      </Card>
    </section>
  );
}
