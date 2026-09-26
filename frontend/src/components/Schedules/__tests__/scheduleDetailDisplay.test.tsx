// @vitest-environment jsdom
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ScheduleDetailDrawer } from '../ScheduleDetailDrawer';
import { MobileScheduleDetailPage } from '../MobileScheduleDetailPage';
import { formatLocalDateTime } from '@/lib/scheduleFormat';
import type { Schedule } from '@/types/schedule';
const mock = vi.hoisted(() => ({ schedule: null as Schedule | null }));
vi.mock('../useScheduleDetail', () => ({ useScheduleDetail: () => ({ schedule: mock.schedule, loading: false, error: null, occurrences: [], occurrencesLoading: false, occurrenceError: null }) }));
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true,
  matchMedia: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} }),
});
let root: Root, host: HTMLElement;
beforeEach(() => {
  host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
  mock.schedule = {
    id: 7, name: '自动化详情', schedule_type: 'daily', timezone: 'America/New_York', enabled: true,
    trigger: { time: '09:00', starts_at: '2099-01-01T01:02:03Z', ends_at: '2099-02-01T04:05:06Z' },
    next_run_at: '2099-01-02T14:00:00Z', run_at: null, prompt: 'Respond',
    deliveries: [
      { id: 1, target_type: 'chat', target_id: 'c', target_name: '群', content_mode: 'summary', enabled: true, condition: { operator: 'contains', text: ' <Alert>\nCPU > 80% ' } },
      { id: 2, target_type: 'user', target_id: 'u', target_name: '人', content_mode: 'summary', enabled: true, condition: { operator: 'not_contains', text: '正常' } },
      { id: 3, target_type: 'user', target_id: 'old', target_name: '', content_mode: 'summary', enabled: true },
    ],
  } as Schedule;
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
describe.each([['desktop', ScheduleDetailDrawer], ['mobile', MobileScheduleDetailPage]] as const)('%s detail', (_name, Detail) => {
  it('shows effective dates, explicit timezones and all per-target predicates', async () => {
    await act(async () => root.render(<Detail open={true as never} scheduleId={7} onClose={() => {}} />));
    const text = document.body.textContent;
    expect(text).toContain('计划时区：America/New_York');
    expect(text).toContain('生效开始：'); expect(text).toContain('生效结束：');
    expect(text).toContain(formatLocalDateTime(mock.schedule!.trigger.starts_at));
    expect(text).toContain(formatLocalDateTime(mock.schedule!.trigger.ends_at));
    expect(text).toContain(formatLocalDateTime(mock.schedule!.next_run_at));
    const rows = document.querySelectorAll('[data-delivery-id]');
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toContain('最终回复包含「 <Alert>\nCPU > 80% 」（区分大小写，按原文匹配）');
    expect(rows[1].textContent).toContain('最终回复不包含「正常」');
    expect(rows[2].textContent).toContain('old'); expect(rows[2].textContent).toContain('每次执行完成后');
    expect(document.querySelector('alert')).toBeNull();
  });
  it('shows unbounded defaults and labels a once time as device-local rather than recurrence timezone', async () => {
    mock.schedule = { ...mock.schedule!, schedule_type: 'once', run_at: '2099-01-01T09:00:00Z', trigger: {}, deliveries: [] };
    await act(async () => root.render(<Detail open={true as never} scheduleId={7} onClose={() => {}} />));
    expect(document.body.textContent).toContain('立即生效'); expect(document.body.textContent).toContain('永不结束');
    expect(document.body.textContent).toContain(formatLocalDateTime(mock.schedule.run_at));
    expect(document.body.textContent).not.toContain('计划时区：America/New_York');
  });
});
