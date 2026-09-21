import {
  enterpriseApi,
  type EnterpriseReadOptions,
  type SyncConfig,
  type SyncJob,
  type SyncJobStatus,
  type SyncRun,
  type SyncScheduleConfig,
  type SyncTarget,
  type SyncTargetConfig,
} from './enterpriseApi';

export interface SyncTargetView {
  code: string;
  displayName: string;
  description: string;
  dependencies: string[];
  metricKeys: string[];
  config: SyncTargetConfig;
  legacyFallback: boolean;
}

export interface SyncJobView {
  id: number | string;
  targetCode: string;
  batchId?: number | string | null;
  triggerType: string;
  status: SyncJobStatus;
  metrics: Record<string, number>;
  warnings: string[];
  targetVersion: number;
  startedAt?: string | null;
  finishedAt?: string | null;
  errorCode: string;
  errorMessage: string;
  createdAt: string;
  legacyFallback: boolean;
}

const TARGET_METADATA: Record<string, { displayName: string; description: string; dependencies: string[]; metricKeys: string[] }> = {
  directory: {
    displayName: '组织通讯录',
    description: '同步部门、用户和用户所属部门关系，并以完整快照原子发布。',
    dependencies: [],
    metricKeys: ['departments', 'users', 'active_users', 'memberships', 'active_memberships'],
  },
  user_groups: {
    displayName: '飞书用户组',
    description: '同步普通用户组、动态用户组及成员；失败不会影响已发布的通讯录。',
    dependencies: ['directory'],
    metricKeys: ['groups', 'group_members', 'unknown_users'],
  },
};

const METRIC_LABELS: Record<string, string> = {
  departments: '部门',
  users: '用户',
  active_users: '有效用户',
  memberships: '所属部门关系',
  active_memberships: '有效所属关系',
  groups: '用户组',
  group_members: '用户组成员',
  unknown_users: '未匹配用户',
};

const DEFAULT_CONFIG: Omit<SyncTargetConfig, 'target_code'> = {
  enabled: false,
  schedule_type: 'interval',
  interval_minutes: 360,
  daily_time: '02:00',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: null,
  last_success_at: null,
  target_version: 0,
  last_error_code: '',
  last_error_message: '',
  updated_at: '',
};

function asRecord(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function asString(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback;
}

function asNullableString(value: unknown): string | null | undefined {
  return value === null || typeof value === 'string' ? value : undefined;
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

function numberRecordFrom(value: unknown): Record<string, number> {
  return Object.entries(asRecord(value)).reduce<Record<string, number>>((metrics, [key, item]) => {
    if (typeof item === 'number' && Number.isFinite(item)) metrics[key] = item;
    return metrics;
  }, {});
}

function warningsFrom(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => {
    if (typeof item === 'string') return item;
    const record = asRecord(item);
    const message = asString(record.message || record.code, '同步告警');
    return typeof record.count === 'number' && record.count > 0
      ? `${message}（${record.count}）`
      : message;
  });
}

export function normalizeSyncTargetConfig(value: unknown, fallbackCode = ''): SyncTargetConfig {
  const record = asRecord(value);
  const code = asString(record.target_code, fallbackCode);
  return {
    ...DEFAULT_CONFIG,
    target_code: code,
    enabled: typeof record.enabled === 'boolean' ? record.enabled : DEFAULT_CONFIG.enabled,
    schedule_type: record.schedule_type === 'daily' ? 'daily' : 'interval',
    interval_minutes: asNumber(record.interval_minutes, DEFAULT_CONFIG.interval_minutes),
    daily_time: asString(record.daily_time, DEFAULT_CONFIG.daily_time),
    timezone: asString(record.timezone, DEFAULT_CONFIG.timezone),
    next_run_at: asNullableString(record.next_run_at) ?? null,
    last_run_at: asNullableString(record.last_run_at) ?? null,
    last_success_at: asNullableString(record.last_success_at) ?? null,
    target_version: asNumber(record.target_version, 0),
    last_error_code: asString(record.last_error_code),
    last_error_message: asString(record.last_error_message),
    updated_at: asString(record.updated_at),
  };
}

function normalizeTarget(value: SyncTarget | unknown, legacyFallback = false): SyncTargetView | null {
  const record = asRecord(value);
  const code = asString(record.code);
  if (!code) return null;
  const fallback = TARGET_METADATA[code];
  const dependencies = Array.isArray(record.dependencies)
    ? asStringArray(record.dependencies)
    : fallback?.dependencies || [];
  const metricKeys = Array.isArray(record.metrics_schema)
    ? asStringArray(record.metrics_schema)
    : fallback?.metricKeys || [];
  return {
    code,
    displayName: asString(record.display_name, fallback?.displayName || code),
    description: asString(record.description, fallback?.description || '企业数据同步目标'),
    dependencies,
    metricKeys,
    config: normalizeSyncTargetConfig(record.config, code),
    legacyFallback,
  };
}

function targetOrder(code: string): number {
  if (code === 'directory') return 0;
  if (code === 'user_groups') return 1;
  return 100;
}

export function normalizeSyncTargets(response: SyncTarget[] | unknown): SyncTargetView[] {
  if (!Array.isArray(response)) return [];
  return response
    .map((target) => normalizeTarget(target))
    .filter((target): target is SyncTargetView => target !== null)
    .sort((a, b) => targetOrder(a.code) - targetOrder(b.code) || a.displayName.localeCompare(b.displayName));
}

function normalizeJob(value: SyncJob | unknown, legacyFallback = false): SyncJobView | null {
  const record = asRecord(value);
  const targetCode = asString(record.target_code);
  if (!targetCode) return null;
  const rawStatus = asString(record.status, 'pending');
  const status: SyncJobStatus = ['pending', 'blocked', 'running', 'success', 'failed'].includes(rawStatus)
    ? rawStatus as SyncJobStatus
    : 'pending';
  return {
    id: typeof record.id === 'number' || typeof record.id === 'string'
      ? record.id
      : `${targetCode}:${asString(record.created_at)}`,
    targetCode,
    batchId: typeof record.batch_id === 'number' || typeof record.batch_id === 'string' || record.batch_id === null
      ? record.batch_id
      : undefined,
    triggerType: asString(record.trigger_type, 'manual'),
    status,
    metrics: numberRecordFrom(record.metrics),
    warnings: warningsFrom(record.warnings),
    targetVersion: asNumber(record.target_version, 0),
    startedAt: asNullableString(record.started_at),
    finishedAt: asNullableString(record.finished_at),
    errorCode: asString(record.error_code),
    errorMessage: asString(record.error_message),
    createdAt: asString(record.created_at),
    legacyFallback,
  };
}

export function normalizeSyncJobs(response: SyncJob[] | unknown): SyncJobView[] {
  if (!Array.isArray(response)) return [];
  return response
    .map((job) => normalizeJob(job))
    .filter((job): job is SyncJobView => job !== null);
}

export function legacyTargetsFrom(config: SyncConfig): SyncTargetView[] {
  return ['directory', 'user_groups'].map((code) => {
    const metadata = TARGET_METADATA[code];
    return {
      code,
      displayName: metadata.displayName,
      description: `${metadata.description}（旧接口仅支持与另一目标组合配置和执行）`,
      dependencies: metadata.dependencies,
      metricKeys: metadata.metricKeys,
      config: normalizeSyncTargetConfig({
        ...config,
        target_code: code,
        target_version: config.directory_version,
      }, code),
      legacyFallback: true,
    };
  });
}

export function legacyJobsFrom(runs: SyncRun[]): SyncJobView[] {
  return runs.flatMap((run) => {
    const common = {
      batchId: `legacy:${run.id}`,
      triggerType: run.trigger_type,
      status: run.status,
      targetVersion: 0,
      startedAt: run.started_at,
      finishedAt: run.finished_at,
      errorCode: run.error_code,
      errorMessage: run.error_message,
      createdAt: run.created_at,
      legacyFallback: true,
    };
    const directoryJob: SyncJobView = {
      ...common,
      id: `${run.id}:directory`,
      targetCode: 'directory',
      metrics: {
        departments: run.departments_count,
        users: run.users_count,
        active_users: run.active_users_count,
        memberships: run.memberships_count,
        active_memberships: run.active_memberships_count,
      },
      warnings: [],
    };
    const userGroupsJob: SyncJobView = {
      ...common,
      id: `${run.id}:user_groups`,
      targetCode: 'user_groups',
      metrics: {
        groups: run.groups_count,
        group_members: run.group_members_count,
        unknown_users: run.unknown_users_count,
      },
      warnings: run.group_fetch_warnings > 0 ? ['旧同步记录包含用户组获取告警'] : [],
    };
    return [directoryJob, userGroupsJob];
  });
}

export function isUnsupportedSyncApiError(error: unknown): boolean {
  const status = asRecord(asRecord(error).response).status;
  return status === 404 || status === 405 || status === 501;
}

export async function loadSyncTargets(options?: EnterpriseReadOptions): Promise<SyncTargetView[]> {
  try {
    return normalizeSyncTargets(await enterpriseApi.syncTargets({ ...options, silentError: true }));
  } catch (error) {
    if (!isUnsupportedSyncApiError(error)) throw error;
    return legacyTargetsFrom(await enterpriseApi.syncConfig({ fresh: options?.fresh }));
  }
}

export async function loadSyncTargetConfig(
  code: string,
  options?: EnterpriseReadOptions,
): Promise<SyncTargetConfig> {
  return normalizeSyncTargetConfig(await enterpriseApi.syncTargetConfig(code, options), code);
}

export async function loadSyncJobs(
  limit = 50,
  options?: EnterpriseReadOptions,
): Promise<SyncJobView[]> {
  try {
    return normalizeSyncJobs(await enterpriseApi.syncJobs(limit, { ...options, silentError: true }));
  } catch (error) {
    if (!isUnsupportedSyncApiError(error)) throw error;
    return legacyJobsFrom(await enterpriseApi.syncRuns(limit, { fresh: options?.fresh }));
  }
}

export async function saveSyncTargetConfig(
  code: string,
  config: Partial<SyncScheduleConfig>,
): Promise<SyncTargetConfig> {
  const updated = normalizeSyncTargetConfig(
    await enterpriseApi.updateSyncTargetConfig(code, config),
    code,
  );
  try {
    return await loadSyncTargetConfig(code, { fresh: true });
  } catch {
    return updated;
  }
}

export async function triggerOneSyncTarget(code: string): Promise<void> {
  await enterpriseApi.triggerSyncTarget(code);
}

export async function triggerAllSyncTargets(targets: string[]): Promise<void> {
  await enterpriseApi.triggerSyncBatch(targets);
}

export function formatMetricLabel(key: string): string {
  return METRIC_LABELS[key] || key.replace(/_/g, ' ');
}

export function syncTriggerLabel(triggerType: string): string {
  if (triggerType === 'manual') return '手动';
  if (triggerType === 'scheduled') return '自动';
  if (triggerType === 'batch') return '批次';
  return triggerType || '—';
}
