import { describe, expect, it, vi } from 'vitest';
import type { FormInstance } from 'antd';
import type { SyncTargetConfig } from '../enterpriseApi';
import {
  reconcileSyncTargetForm,
  syncTargetControlId,
  syncTargetFormName,
  type SyncTargetFormValues,
} from '../syncTargetForm';

const CONFIG: SyncTargetConfig = {
  target_code: 'user_groups',
  enabled: true,
  schedule_type: 'daily',
  interval_minutes: 360,
  daily_time: '03:30',
  timezone: 'Asia/Shanghai',
  next_run_at: null,
  last_run_at: null,
  last_success_at: null,
  target_version: 4,
  last_error_code: '',
  last_error_message: '',
  updated_at: '2026-09-20T03:00:00Z',
};

function fakeForm(touched: boolean) {
  return {
    isFieldsTouched: vi.fn(() => touched),
    setFields: vi.fn(),
  } as unknown as FormInstance<SyncTargetFormValues>;
}

describe('sync target form reconciliation', () => {
  it('preserves dirty values when another target or a global refresh updates server state', () => {
    const form = fakeForm(true);

    expect(reconcileSyncTargetForm(form, CONFIG)).toBe(false);
    expect(form.setFields).not.toHaveBeenCalled();
  });

  it('applies a saved config to that target only and clears touched state', () => {
    const form = fakeForm(true);

    expect(reconcileSyncTargetForm(form, CONFIG, { force: true })).toBe(true);
    expect(form.setFields).toHaveBeenCalledTimes(1);
    const fields = vi.mocked(form.setFields).mock.calls[0][0];
    expect(fields).toEqual(expect.arrayContaining([
      expect.objectContaining({ name: 'enabled', value: true, touched: false }),
      expect.objectContaining({ name: 'schedule_type', value: 'daily', touched: false }),
      expect.objectContaining({ name: 'timezone', value: 'Asia/Shanghai', touched: false }),
    ]));
  });

  it('generates unique form names and field IDs for target and surface', () => {
    expect(syncTargetFormName('directory', 'desktop')).toBe('sync-target-directory-desktop');
    expect(syncTargetFormName('directory', 'mobile')).toBe('sync-target-directory-mobile');
    expect(syncTargetControlId('user_groups', 'desktop', 'interval_minutes'))
      .toBe('sync-target-user_groups-desktop-interval-minutes');
  });
});
