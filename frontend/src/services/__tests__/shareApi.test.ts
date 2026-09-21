// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
  loadForwardHistory,
  saveForwardHistory,
  type FeishuForwardTarget,
} from '@/services/shareApi';

const target = (
  id: string,
  name: string,
  targetType: 'user' | 'chat',
): FeishuForwardTarget => ({
  id,
  name,
  avatar_url: '',
  target_type: targetType,
});

describe('Feishu forward history', () => {
  beforeEach(() => localStorage.clear());

  it('keeps user and chat targets with the same raw id as separate recent entries', () => {
    saveForwardHistory([target('same-id', '同名群', 'chat')]);
    saveForwardHistory([target('same-id', '同名用户', 'user')]);

    expect(loadForwardHistory()).toEqual([
      target('same-id', '同名用户', 'user'),
      target('same-id', '同名群', 'chat'),
    ]);
  });
});
