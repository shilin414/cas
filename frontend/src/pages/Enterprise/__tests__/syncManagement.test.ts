import { afterEach, describe, expect, it, vi } from 'vitest';
import { enterpriseApi, type SyncConfig, type SyncRun, type SyncTargetConfig } from '../enterpriseApi';
import {
  formatMetricLabel,
  legacyJobsFrom,
  loadSyncTargets,
  normalizeSyncJobs,
  normalizeSyncTargets,
  saveSyncTargetConfig,
  triggerAllSyncTargets,
  triggerOneSyncTarget,
} from '../syncManagement';

const TARGET_CONFIG: SyncTargetConfig = {
  target_code: 'directory',
  enabled: true,
  schedule_type: 'interval',
  interval_minutes: 120,
  daily_time: '02:00',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: '2026-09-20T01:00:00Z',
  last_success_at: '2026-09-20T01:00:00Z',
  target_version: 7,
  last_error_code: '',
  last_error_message: '',
  updated_at: '2026-09-20T02:00:00Z',
};

const LEGACY_CONFIG: SyncConfig = {
  enabled: true,
  schedule_type: 'interval',
  interval_minutes: 120,
  daily_time: '02:00',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: null,
  last_success_at: '2026-09-20T02:00:00Z',
  directory_version: 7,
  updated_at: '2026-09-20T02:00:00Z',
};

const LEGACY_RUN: SyncRun = {
  id: 9,
  trigger_type: 'scheduled',
  status: 'success',
  departments_count: 10,
  users_count: 20,
  active_users_count: 18,
  memberships_count: 30,
  active_memberships_count: 29,
  groups_count: 4,
  group_members_count: 12,
  group_fetch_warnings: 1,
  member_mapping_errors: 2,
  unknown_users_count: 3,
  started_at: '2026-09-20T02:00:00Z',
  finished_at: '2026-09-20T02:01:00Z',
  error_code: '',
  error_message: '',
  created_at: '2026-09-20T02:00:00Z',
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('real enterprise sync API contract normalization', () => {
  it('normalizes target_version, target errors, and string[] metrics_schema', () => {
    const targets = normalizeSyncTargets([
      {
        code: 'directory',
        display_name: 'Directory',
        description: 'Departments, users, and department memberships',
        dependencies: [],
        metrics_schema: ['departments', 'users', 'active_users', 'memberships', 'active_memberships'],
        config: {
          ...TARGET_CONFIG,
          last_error_code: 'previous_failure',
          last_error_message: 'Previous directory sync failed',
        },
      },
      {
        code: 'user_groups',
        display_name: 'User groups',
        description: 'Feishu normal/dynamic groups and mapped members',
        dependencies: ['directory'],
        metrics_schema: ['groups', 'group_members', 'unknown_users'],
        config: {
          ...TARGET_CONFIG,
          target_code: 'user_groups',
          target_version: 3,
        },
      },
    ]);

    expect(targets[0]).toMatchObject({
      code: 'directory',
      displayName: 'Directory',
      metricKeys: ['departments', 'users', 'active_users', 'memberships', 'active_memberships'],
      config: {
        target_version: 7,
        last_error_code: 'previous_failure',
        last_error_message: 'Previous directory sync failed',
      },
    });
    expect(targets[1]).toMatchObject({
      code: 'user_groups',
      dependencies: ['directory'],
      metricKeys: ['groups', 'group_members', 'unknown_users'],
      config: { target_version: 3 },
    });
  });

  it('normalizes real job metrics and target_version without legacy count names', () => {
    const jobs = normalizeSyncJobs([{
      id: 42,
      target_code: 'directory',
      batch_id: 8,
      trigger_type: 'manual',
      status: 'success',
      metrics: { departments: 10, users: 20, active_users: 18 },
      warnings: [{ code: 'partial', message: 'Some users were skipped', count: 2 }],
      target_version: 9,
      started_at: '2026-09-20T03:00:00Z',
      finished_at: '2026-09-20T03:01:00Z',
      error_code: '',
      error_message: '',
      created_at: '2026-09-20T03:00:00Z',
    }]);

    expect(jobs[0]).toMatchObject({
      targetCode: 'directory',
      batchId: 8,
      targetVersion: 9,
      metrics: { departments: 10, users: 20, active_users: 18 },
      warnings: ['Some users were skipped（2）'],
    });
    expect(formatMetricLabel('departments')).toBe('部门');
    expect(formatMetricLabel('group_members')).toBe('用户组成员');
  });
});

describe('safe rollout behavior', () => {
  it('uses legacy endpoints only for explicit read-only capability detection', async () => {
    vi.spyOn(enterpriseApi, 'syncTargets').mockRejectedValue({ response: { status: 404 } });
    vi.spyOn(enterpriseApi, 'syncConfig').mockResolvedValue(LEGACY_CONFIG);

    const targets = await loadSyncTargets({ fresh: true });

    expect(enterpriseApi.syncTargets).toHaveBeenCalledWith({ fresh: true, silentError: true });
    expect(targets.map((target) => target.code)).toEqual(['directory', 'user_groups']);
    expect(targets.every((target) => target.legacyFallback)).toBe(true);
    expect(targets[0].description).toContain('组合配置和执行');
  });

  it('maps legacy history to the current metric keys', () => {
    const jobs = legacyJobsFrom([LEGACY_RUN]);

    expect(jobs[0]).toMatchObject({
      targetCode: 'directory',
      metrics: { departments: 10, users: 20, active_users: 18 },
    });
    expect(jobs[1]).toMatchObject({
      targetCode: 'user_groups',
      metrics: { groups: 4, group_members: 12, unknown_users: 3 },
    });
  });

  it('never turns a target-specific mutation into a legacy combined mutation', async () => {
    vi.spyOn(enterpriseApi, 'triggerSyncTarget').mockRejectedValue({ response: { status: 404 } });
    const legacyTrigger = vi.spyOn(enterpriseApi, 'triggerSync');
    await expect(triggerOneSyncTarget('directory')).rejects.toMatchObject({ response: { status: 404 } });
    expect(legacyTrigger).not.toHaveBeenCalled();

    vi.spyOn(enterpriseApi, 'triggerSyncBatch').mockRejectedValue({ response: { status: 405 } });
    await expect(triggerAllSyncTargets(['directory', 'user_groups'])).rejects.toMatchObject({ response: { status: 405 } });
    expect(legacyTrigger).not.toHaveBeenCalled();
  });

  it('refreshes the saved target config with a fresh read and no legacy write fallback', async () => {
    vi.spyOn(enterpriseApi, 'updateSyncTargetConfig').mockResolvedValue(TARGET_CONFIG);
    const freshConfig = { ...TARGET_CONFIG, target_version: 8, updated_at: '2026-09-20T04:00:00Z' };
    const read = vi.spyOn(enterpriseApi, 'syncTargetConfig').mockResolvedValue(freshConfig);
    const legacyUpdate = vi.spyOn(enterpriseApi, 'updateSyncConfig');

    await expect(saveSyncTargetConfig('directory', { enabled: false })).resolves.toMatchObject({
      target_version: 8,
    });
    expect(read).toHaveBeenCalledWith('directory', { fresh: true });
    expect(legacyUpdate).not.toHaveBeenCalled();
  });

  it('does not hide a real target-list failure behind legacy fallback', async () => {
    vi.spyOn(enterpriseApi, 'syncTargets').mockRejectedValue({ response: { status: 500 } });
    const legacy = vi.spyOn(enterpriseApi, 'syncConfig');

    await expect(loadSyncTargets()).rejects.toMatchObject({ response: { status: 500 } });
    expect(legacy).not.toHaveBeenCalled();
  });
});
