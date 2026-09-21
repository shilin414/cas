import { describe, expect, it } from 'vitest';
import { formatSyncDuration } from '../syncDuration';

describe('formatSyncDuration', () => {
  it('formats the successful 2026-09-18 directory sync from its actual execution bounds', () => {
    expect(formatSyncDuration(
      '2026-09-18T11:43:49.329Z',
      '2026-09-18T11:49:03.662Z',
    )).toBe('5分14秒');
  });

  it('formats seconds and hours without exposing raw milliseconds', () => {
    expect(formatSyncDuration('2026-09-18T00:00:00Z', '2026-09-18T00:00:42Z')).toBe('42秒');
    expect(formatSyncDuration('2026-09-18T00:00:00Z', '2026-09-18T01:02:03Z')).toBe('1小时2分3秒');
  });

  it('returns a placeholder for incomplete, invalid, or reversed timestamps', () => {
    expect(formatSyncDuration(null, '2026-09-18T00:00:42Z')).toBe('—');
    expect(formatSyncDuration('invalid', '2026-09-18T00:00:42Z')).toBe('—');
    expect(formatSyncDuration('2026-09-18T00:01:00Z', '2026-09-18T00:00:42Z')).toBe('—');
  });

  it('shows sub-second completed work as less than one second', () => {
    expect(formatSyncDuration('2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.333Z')).toBe('<1秒');
  });
});
