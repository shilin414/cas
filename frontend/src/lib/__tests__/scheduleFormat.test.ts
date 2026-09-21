/**
 * scheduleFormat 纯函数单测：预设转换、字段清理、时区展示。
 */
import { describe, expect, it } from 'vitest';
import {
  WEEKDAY_LABELS,
  cleanTrigger,
  describeTrigger,
  describeSchedulePlan,
  formatLocalDateTime,
  describeDeliveryCondition,
  formToPayload,
  scheduleToForm,
  toLocalInput,
  type ScheduleFormValues,
} from '../scheduleFormat';
import type { Schedule } from '@/types/schedule';

const baseForm: ScheduleFormValues = {
  name: '日报',
  application_id: 7,
  prompt: '总结今天',
  schedule_type: 'daily',
  timezone: 'Asia/Shanghai',
  start_mode: 'immediate',
  end_mode: 'never',
  trigger: { time: '09:00', days_of_week: [1, 2], day_of_month: 1 },
  conversation_policy: 'new_each_run',
  overlap_policy: 'queue',
  misfire_policy: 'fire_once',
  deadline_policy: 'execute_anyway',
  execution_window_seconds: 0,
  deliveries: [],
};

describe('formToPayload', () => {
  it('keeps only trigger fields relevant to the schedule type', () => {
    const p = formToPayload({ ...baseForm, schedule_type: 'weekly', trigger: { time: '08:30', days_of_week: [3, 1, 5], day_of_month: 31 } });
    expect(p.trigger).toEqual({ time: '08:30', days_of_week: [1, 3, 5] });
  });

  it('monthly keeps day_of_month and drops days_of_week', () => {
    const p = formToPayload({ ...baseForm, schedule_type: 'monthly' });
    expect(p.trigger).toEqual({ time: '09:00', day_of_month: 1 });
  });

  it('once uses run_at and explicitly clears inactive bounds', () => {
    const p = formToPayload({ ...baseForm, schedule_type: 'once', run_at_local: '2026-09-14T09:00' });
    expect(p.trigger).toEqual({});
    expect(p.run_at).toBeTruthy();
  });

  it('empty deliveries become undefined (no delivery)', () => {
    expect(formToPayload(baseForm).deliveries).toBeUndefined();
  });

  it('deliveries map to summary content mode', () => {
    const p = formToPayload({
      ...baseForm,
      deliveries: [{ target_type: 'chat', target_id: 'oc_1', target_name: '群' }],
    });
    expect(p.deliveries).toEqual([
      { target_type: 'chat', target_id: 'oc_1', target_name: '群', content_mode: 'summary', condition: { operator: 'always' } },
    ]);
  });
});

describe('describeTrigger', () => {
  it('formats daily/weekly/monthly in Chinese', () => {
    expect(describeTrigger('daily', { time: '09:00' })).toBe('每天 09:00');
    expect(describeTrigger('weekly', { time: '09:00', days_of_week: [1, 5] })).toContain('周一');
    expect(describeTrigger('monthly', { time: '09:00', day_of_month: 31 })).toContain('31 号');
  });

  it('WEEKDAY_LABELS start on Sunday (0)', () => {
    expect(WEEKDAY_LABELS[0]).toBe('周日');
    expect(WEEKDAY_LABELS[1]).toBe('周一');
  });
});

describe('scheduleToForm roundtrip', () => {
  it('hydrates the editor form from a Schedule', () => {
    const schedule = {
      id: 1,
      name: '周报',
      description: '',
      application_id: 3,
      prompt: '写周报',
      schedule_type: 'weekly' as const,
      cron_expression: '0 9 * * 1',
      timezone: 'UTC',
      run_at: null,
      trigger: { time: '09:00', days_of_week: [1] },
      enabled: true,
      conversation_policy: 'new_each_run' as const,
      overlap_policy: 'queue' as const,
      misfire_policy: 'fire_once' as const,
      deadline_policy: 'execute_anyway' as const,
      execution_window_seconds: 0,
      next_run_at: null,
      last_run_at: null,
      created_at: '2026-09-13T00:00:00Z',
      updated_at: '2026-09-13T00:00:00Z',
      deliveries: [],
    } as Schedule;
    const form = scheduleToForm(schedule);
    expect(form.schedule_type).toBe('weekly');
    expect(form.trigger.days_of_week).toEqual([1]);
    expect(form.timezone).toBe('UTC');
  });
});

describe('toLocalInput', () => {
  it('renders a datetime-local value', () => {
    const out = toLocalInput('2026-09-13T01:02:00Z');
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
  });

  it('returns empty for invalid input', () => {
    expect(toLocalInput('not-a-date')).toBe('');
  });
});

describe('cleanTrigger (exported helper)', () => {
  it('strips irrelevant fields for daily', () => {
    expect(cleanTrigger('daily', { time: '09:00', days_of_week: [1], day_of_month: 2 }))
      .toEqual({ time: '09:00' });
  });
});


describe('automation effective boundaries and literal delivery conditions', () => {
  const now = new Date('2098-01-01T00:00:00Z').getTime();
  const bounded: ScheduleFormValues = {
    ...baseForm, start_mode: 'specified', end_mode: 'specified',
    starts_at_local: '2099-01-01T08:00', ends_at_local: '2099-02-01T08:00',
  };
  it.each(['once', 'daily', 'weekly', 'monthly'] as const)('preserves bounds for %s', (schedule_type) => {
    const payload = formToPayload({ ...bounded, schedule_type, run_at_local: '2099-01-15T09:00' }, { now });
    expect(payload.trigger).toMatchObject({
      starts_at: new Date(bounded.starts_at_local!).toISOString(),
      ends_at: new Date(bounded.ends_at_local!).toISOString(),
    });
    if (schedule_type === 'once') expect(payload.trigger).not.toHaveProperty('time');
  });
  it('clears inactive bounds including stale trigger metadata', () => {
    const payload = formToPayload({ ...bounded, start_mode: 'immediate', end_mode: 'never', trigger: { starts_at: '2099-01-01T00:00:00Z', ends_at: '2099-02-01T00:00:00Z', time: '09:00' } }, { now });
    expect(payload.trigger).toEqual({ time: '09:00' });
  });
  it.each([
    { starts_at_local: undefined },
    { starts_at_local: 'not-a-date' },
    { starts_at_local: '2099-02-30T08:00' },
    { starts_at_local: '2099-02-01T25:00' },
    { starts_at_local: '2097-01-01T08:00' },
    { ends_at_local: undefined },
    { ends_at_local: 'invalid' },
    { ends_at_local: '2097-01-01T08:00' },
    { ends_at_local: '2099-01-01T07:00' },
  ])('rejects missing/invalid/past/reversed boundary %j', (change) => {
    expect(() => formToPayload({ ...bounded, ...change }, { now })).toThrow();
  });
  it('accepts equal inclusive bounds and a once run at that instant', () => {
    const payload = formToPayload({ ...bounded, schedule_type: 'once', ends_at_local: bounded.starts_at_local, run_at_local: bounded.starts_at_local }, { now });
    expect(payload.run_at).toBe(payload.trigger?.starts_at);
    expect(payload.run_at).toBe(payload.trigger?.ends_at);
  });
  it('allows the unchanged past start of an existing automation', () => {
    const starts_at = new Date('2097-01-01T08:00').toISOString();
    expect(formToPayload({ ...bounded, starts_at_local: toLocalInput(starts_at) }, {
      now, original: { trigger: { starts_at } },
    }).trigger?.starts_at).toBe(starts_at);
  });
  it.each(['2098-12-31T09:00', '2099-02-02T09:00'])('rejects once run outside bounds: %s', (run_at_local) => {
    expect(() => formToPayload({ ...bounded, schedule_type: 'once', run_at_local }, { now })).toThrow();
  });
  it('preserves literal text, separate predicates and all targets', () => {
    const deliveries = [
      { target_type: 'chat' as const, target_id: 'c', target_name: '群', condition: { operator: 'contains' as const, text: '  Alert\nCPU > 80%  ' } },
      { target_type: 'user' as const, target_id: 'u', target_name: '人', condition: { operator: 'not_contains' as const, text: '正常' } },
    ];
    const payload = formToPayload({ ...baseForm, deliveries }, { now });
    expect(payload.deliveries?.map(d => d.condition)).toEqual(deliveries.map(d => d.condition));
  });
  it.each(['contains', 'not_contains'] as const)('rejects empty %s text', (operator) => {
    expect(() => formToPayload({ ...baseForm, deliveries: [{ target_type: 'chat', target_id: 'c', target_name: '群', condition: { operator, text: '  ' } }] }, { now })).toThrow();
  });
  it('defaults legacy predicates to always and discards inactive text', () => {
    const delivery = { target_type: 'chat' as const, target_id: 'c', target_name: '群' };
    expect(formToPayload({ ...baseForm, deliveries: [delivery] }).deliveries?.[0].condition).toEqual({ operator: 'always' });
    expect(formToPayload({ ...baseForm, deliveries: [{ ...delivery, condition: { operator: 'always', text: 'old' } }] }).deliveries?.[0].condition).toEqual({ operator: 'always' });
  });
  it('roundtrips bounds to millisecond precision and preserves predicates', () => {
    const schedule = {
      ...baseForm, id: 1, description: '', run_at: null,
      trigger: { time: '09:00', starts_at: '2099-01-01T01:02:03.456Z', ends_at: '2099-02-01T02:03:04.789Z' },
      deliveries: [{ id: 1, target_type: 'user', target_id: 'u', target_name: '人', content_mode: 'summary', enabled: true, condition: { operator: 'contains', text: ' A ' } }],
    } as unknown as Schedule;
    const form = scheduleToForm(schedule);
    expect(form.start_mode).toBe('specified');
    expect(form.end_mode).toBe('specified');
    expect(formToPayload(form, { now }).trigger).toEqual(schedule.trigger);
    expect(formToPayload(form, { now }).deliveries?.[0].condition).toEqual(schedule.deliveries?.[0].condition);
    form.deliveries[0].condition!.text = 'changed';
    expect(schedule.deliveries?.[0].condition?.text).toBe(' A ');
  });
});


describe('automation detail display', () => {
  it('labels absolute timestamps with device timezone and instant-specific UTC offset', () => {
    const iso = '2099-01-01T01:02:03.456Z';
    expect(formatLocalDateTime(iso)).toContain(toLocalInput(iso).replace('T', ' '));
    expect(formatLocalDateTime(iso)).toContain('设备本地时间');
    expect(formatLocalDateTime(iso)).toContain(Intl.DateTimeFormat().resolvedOptions().timeZone);
    expect(formatLocalDateTime(iso)).toMatch(/UTC[+-]\d{2}:\d{2}/);
    expect(formatLocalDateTime(null)).toBe('—');
    expect(formatLocalDateTime('invalid')).toBe('—');
  });
  it('distinguishes a recurring timezone from a local once timestamp', () => {
    const plan = { schedule_type: 'daily' as const, trigger: { time: '09:00' }, run_at: '2099-01-01T09:00:00Z', timezone: 'America/New_York' };
    expect(describeSchedulePlan(plan)).toBe('每天 09:00 · 计划时区：America/New_York');
    expect(describeSchedulePlan({ ...plan, schedule_type: 'once' })).toContain('设备本地时间');
    expect(describeSchedulePlan({ ...plan, schedule_type: 'once' })).not.toContain('计划时区');
  });
  it('shows each literal condition without trimming, case conversion or HTML interpretation', () => {
    expect(describeDeliveryCondition()).toBe('每次执行完成后');
    expect(describeDeliveryCondition({ operator: 'always', text: 'ignored' })).toBe('每次执行完成后');
    const text = '  <Alert>\nCPU > 80%  ';
    expect(describeDeliveryCondition({ operator: 'contains', text })).toBe('最终回复包含「' + text + '」（区分大小写，按原文匹配）');
    expect(describeDeliveryCondition({ operator: 'not_contains', text })).toBe('最终回复不包含「' + text + '」（区分大小写，按原文匹配）');
  });
});
