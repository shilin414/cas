import type { FormInstance } from 'antd';
import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';
import type { SyncScheduleType, SyncTargetConfig } from './enterpriseApi';

export interface SyncTargetFormValues {
  enabled: boolean;
  schedule_type: SyncScheduleType;
  interval_minutes: number;
  daily_time: Dayjs;
  timezone: string;
}

const FORM_FIELDS: Array<keyof SyncTargetFormValues> = [
  'enabled',
  'schedule_type',
  'interval_minutes',
  'daily_time',
  'timezone',
];

export function syncTargetFormValues(config: SyncTargetConfig): SyncTargetFormValues {
  return {
    enabled: config.enabled,
    schedule_type: config.schedule_type,
    interval_minutes: config.interval_minutes,
    daily_time: dayjs(config.daily_time || '02:00', 'HH:mm'),
    timezone: config.timezone,
  };
}

export function reconcileSyncTargetForm(
  form: FormInstance<SyncTargetFormValues>,
  config: SyncTargetConfig,
  options?: { force?: boolean },
): boolean {
  if (!options?.force && form.isFieldsTouched()) return false;
  const values = syncTargetFormValues(config);
  form.setFields(FORM_FIELDS.map((name) => ({
    name,
    value: values[name],
    touched: false,
    errors: [],
    warnings: [],
  })));
  return true;
}

export function syncTargetFormName(code: string, surface: 'desktop' | 'mobile'): string {
  return `sync-target-${code}-${surface}`;
}

export function syncTargetControlId(
  code: string,
  surface: 'desktop' | 'mobile',
  field: keyof SyncTargetFormValues,
): string {
  return `${syncTargetFormName(code, surface)}-${field.replace(/_/g, '-')}`;
}
