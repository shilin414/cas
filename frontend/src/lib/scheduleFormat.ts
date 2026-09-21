/**
 * 自动化展示格式化（纯函数，Vitest 直接覆盖）。
 * 与后端契约对齐：weekly 的 days_of_week 0=周日；monthly 短月顺延。
 */
import type { Schedule, ScheduleType, ScheduleTrigger, ScheduleUpsertPayload, ScheduleDeliveryCondition } from '@/types/schedule';

export const WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];

/** trigger → 人类可读计划摘要（无时区，时区单独展示）。 */
export function describeTrigger(scheduleType: ScheduleType, trigger?: ScheduleTrigger, runAt?: string | null): string {
  switch (scheduleType) {
    case 'once':
      return runAt ? formatDateTime(runAt) : '单次执行（未指定时间）';
    case 'daily':
      return `每天 ${trigger?.time ?? '--:--'}`;
    case 'weekly': {
      const days = (trigger?.days_of_week ?? []).slice().sort((a, b) => a - b);
      const label = days.map((d) => WEEKDAY_LABELS[d] ?? '?').join('、');
      return `每周 ${label || '—'} ${trigger?.time ?? '--:--'}`;
    }
    case 'monthly':
      return `每月 ${trigger?.day_of_month ?? '?'} 号 ${trigger?.time ?? '--:--'}（短月顺延到月末）`;
    default:
      return '未配置';
  }
}

/** 面向列表的完整摘要（带时区）。 */
export function describeSchedulePlan(schedule: Pick<Schedule, 'schedule_type' | 'trigger' | 'run_at' | 'timezone'>): string {
  if (schedule.schedule_type === 'once') {
    return schedule.run_at ? '单次 ' + formatLocalDateTime(schedule.run_at) : '单次执行（未指定时间）';
  }
  return `${describeTrigger(schedule.schedule_type, schedule.trigger, schedule.run_at)} · 计划时区：${schedule.timezone}`;
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** 绝对时间按设备本地时区展示，明确区别于重复计划的 timezone。 */
export function formatLocalDateTime(iso: string | null | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return '—';
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const offset = -d.getTimezoneOffset();
  const pad = (n: number) => String(n).padStart(2, '0');
  const zone = 'UTC' + (offset >= 0 ? '+' : '-') + pad(Math.floor(Math.abs(offset) / 60)) + ':' + pad(Math.abs(offset) % 60);
  return toLocalInput(iso).replace('T', ' ') + '（设备本地时间 · ' + timezone + ' · ' + zone + '）';
}

/** 原文显示条件，不裁剪、不做正则或大小写转换；由 React 作为文本渲染。 */
export function describeDeliveryCondition(condition?: ScheduleDeliveryCondition): string {
  if (!condition || condition.operator === 'always') return '每次执行完成后';
  return '最终回复' + (condition.operator === 'not_contains' ? '不包含' : '包含')
    + '「' + (condition.text ?? '') + '」（区分大小写，按原文匹配）';
}

export interface ScheduleFormValues {
  name: string;
  description?: string;
  application_id: number;
  prompt: string;
  schedule_type: ScheduleType;
  timezone: string;
  /** once 模式的本地时间输入值（datetime-local）。 */
  run_at_local?: string;
  start_mode: 'immediate' | 'specified';
  end_mode: 'never' | 'specified';
  /** 浏览器本地时间，保存时转 RFC3339；不使用重复计划的 timezone 解释。 */
  starts_at_local?: string;
  ends_at_local?: string;
  trigger: ScheduleTrigger;
  conversation_policy: 'new_each_run' | 'reuse';
  overlap_policy: 'skip' | 'queue';
  misfire_policy: 'fire_once' | 'skip';
  deadline_policy: 'skip' | 'execute_anyway';
  execution_window_seconds: number;
  /** 飞书投递目标（空 = 不投递）。 */
  deliveries: ScheduleFormDelivery[];
}

export interface ScheduleFormDelivery {
  target_type: 'user' | 'chat';
  target_id: string;
  target_name: string;
  condition?: ScheduleDeliveryCondition;
}

/** 表单值 → 创建/更新 payload（清理无关字段）。 */
export interface ScheduleMappingOptions {
  now?: number;
  /** 既有开始时间允许已过去：编辑名称/提示词不能破坏已生效自动化。 */
  original?: { trigger?: ScheduleTrigger };
}

type ScheduleValidationField = ['starts_at_local' | 'ends_at_local' | 'run_at_local']
  | ['deliveries', number, 'condition', 'operator' | 'text'];

export class ScheduleFormValidationError extends Error {
  constructor(public readonly field: ScheduleValidationField, message: string) {
    super(message);
    this.name = 'ScheduleFormValidationError';
  }
}

export function formToPayload(v: ScheduleFormValues, options: ScheduleMappingOptions = {}): ScheduleUpsertPayload {
  const bounds = effectiveBounds(v, options);
  const payload: ScheduleUpsertPayload = {
    name: v.name.trim(),
    description: v.description?.trim() || undefined,
    application_id: v.application_id,
    prompt: v.prompt,
    schedule_type: v.schedule_type,
    timezone: v.timezone,
    conversation_policy: v.conversation_policy,
    overlap_policy: v.overlap_policy,
    misfire_policy: v.misfire_policy,
    deadline_policy: v.deadline_policy,
    execution_window_seconds: v.execution_window_seconds,
    deliveries: v.deliveries.length > 0
      ? v.deliveries.map((d, index) => ({
          target_type: d.target_type,
          target_id: d.target_id,
          target_name: d.target_name,
          content_mode: 'summary' as const,
          condition: cleanCondition(d.condition, index),
        }))
      : undefined,
  };
  if (v.schedule_type === 'once') {
    payload.run_at = toISO(v.run_at_local);
    if (!payload.run_at) throw new ScheduleFormValidationError(['run_at_local'], '请选择有效的执行时间');
    if ((bounds.starts_at && payload.run_at < bounds.starts_at)
      || (bounds.ends_at && payload.run_at > bounds.ends_at)) {
      throw new ScheduleFormValidationError(['run_at_local'], '单次执行时间必须在生效时间范围内');
    }
    // once 同样有边界；显式 {} 可在 PATCH 时清除原有边界。
    payload.trigger = bounds;
  } else {
    const { starts_at: _start, ends_at: _end, ...frequency } = cleanTrigger(v.schedule_type, v.trigger);
    payload.trigger = { ...frequency, ...bounds };
  }
  return payload;
}

/** 只校验活跃字段；隐藏的历史输入不影响立即/永不结束。 */
function effectiveBounds(v: ScheduleFormValues, options: ScheduleMappingOptions): ScheduleTrigger {
  const now = options.now ?? Date.now();
  const starts_at = v.start_mode === 'specified' ? requiredLocalDate(v.starts_at_local, 'starts_at_local', '开始') : undefined;
  const ends_at = v.end_mode === 'specified' ? requiredLocalDate(v.ends_at_local, 'ends_at_local', '结束') : undefined;
  if (starts_at && new Date(starts_at).getTime() <= now
    && new Date(starts_at).getTime() !== new Date(options.original?.trigger?.starts_at ?? '').getTime()) {
    throw new ScheduleFormValidationError(['starts_at_local'], '开始时间必须晚于当前时间');
  }
  if (ends_at && new Date(ends_at).getTime() <= now) {
    throw new ScheduleFormValidationError(['ends_at_local'], '结束时间必须晚于当前时间');
  }
  if (starts_at && ends_at && starts_at > ends_at) {
    throw new ScheduleFormValidationError(['ends_at_local'], '结束时间不得早于开始时间');
  }
  return { ...(starts_at ? { starts_at } : {}), ...(ends_at ? { ends_at } : {}) };
}

/** 避免 Date 自动把 2 月 30 日或 DST 跳过的本地时间纠正成另一个时间。 */
function requiredLocalDate(value: string | undefined, field: 'starts_at_local' | 'ends_at_local' | 'run_at_local', label: string): string {
  const parts = value?.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/);
  const d = new Date(value ?? '');
  if (!parts || !Number.isFinite(d.getTime())
    || d.getFullYear() !== Number(parts[1]) || d.getMonth() + 1 !== Number(parts[2])
    || d.getDate() !== Number(parts[3]) || d.getHours() !== Number(parts[4])
    || d.getMinutes() !== Number(parts[5]) || d.getSeconds() !== Number(parts[6] ?? 0)) {
    throw new ScheduleFormValidationError([field], '请选择有效的' + label + '时间');
  }
  return d.toISOString();
}

function cleanCondition(condition: ScheduleDeliveryCondition | undefined, index: number): ScheduleDeliveryCondition {
  if (!condition || condition.operator === 'always') return { operator: 'always' };
  if (condition.operator !== 'contains' && condition.operator !== 'not_contains') {
    throw new ScheduleFormValidationError(['deliveries', index, 'condition', 'operator'], '请选择有效的投递条件');
  }
  if (!condition.text?.trim()) {
    throw new ScheduleFormValidationError(['deliveries', index, 'condition', 'text'], '请输入投递条件文本');
  }
  return { operator: condition.operator, text: condition.text };
}

/** datetime-local 字符串或 Dayjs（ duck-typed）→ ISO 字符串。 */
function toISO(v: unknown): string | undefined {
  if (!v) return undefined;
  if (typeof v === 'string') {
    const d = new Date(v);
    return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
  }
  const anyV = v as { toISOString?: () => string };
  if (typeof anyV.toISOString === 'function') return anyV.toISOString();
  return undefined;
}

/** 清理与 schedule_type 无关的字段，避免提交噪声。 */
export function cleanTrigger(type: ScheduleType, trigger: ScheduleTrigger): ScheduleTrigger {
  const out: ScheduleTrigger = {
    ...(trigger.starts_at ? { starts_at: trigger.starts_at } : {}),
    ...(trigger.ends_at ? { ends_at: trigger.ends_at } : {}),
  };
  if (type !== 'once') out.time = trigger.time;
  if (type === 'weekly') out.days_of_week = (trigger.days_of_week ?? []).slice().sort((a, b) => a - b);
  if (type === 'monthly') out.day_of_month = trigger.day_of_month;
  return out;
}

/** Schedule → 表单初值（编辑回填）。 */
export function scheduleToForm(s: Schedule): ScheduleFormValues {
  return {
    name: s.name,
    description: s.description || undefined,
    application_id: s.application_id,
    prompt: s.prompt,
    schedule_type: s.schedule_type,
    timezone: s.timezone,
    run_at_local: s.run_at ? toLocalInput(s.run_at) : undefined,
    start_mode: s.trigger?.starts_at ? 'specified' : 'immediate',
    end_mode: s.trigger?.ends_at ? 'specified' : 'never',
    starts_at_local: s.trigger?.starts_at ? toLocalInput(s.trigger.starts_at) : undefined,
    ends_at_local: s.trigger?.ends_at ? toLocalInput(s.trigger.ends_at) : undefined,
    trigger: {
      time: s.trigger?.time ?? '09:00',
      days_of_week: [...(s.trigger?.days_of_week ?? [1])],
      day_of_month: s.trigger?.day_of_month ?? 1,
    },
    conversation_policy: s.conversation_policy,
    overlap_policy: s.overlap_policy,
    misfire_policy: s.misfire_policy,
    deadline_policy: s.deadline_policy,
    execution_window_seconds: s.execution_window_seconds,
    deliveries: (s.deliveries ?? []).map((d) => ({
      target_type: d.target_type,
      target_id: d.target_id,
      target_name: d.target_name,
      condition: d.condition ? { ...d.condition } : { operator: 'always' },
    })),
  };
}

/** ISO → datetime-local 输入值（本地时区）。 */
export function toLocalInput(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  const seconds = d.getSeconds() || d.getMilliseconds() ? ':' + pad(d.getSeconds()) : '';
  const milliseconds = d.getMilliseconds() ? '.' + String(d.getMilliseconds()).padStart(3, '0') : '';
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}${seconds}${milliseconds}`;
}
