/**
 * navigationHistory — 导航模型回归基线（Architecture 2.0 §三十五）。
 *
 * 目标不是测 React Router 本身，而是固定项目的导航语义期望：
 *
 *   一级导航 → Root Switch（Mobile 用 replace，不产生连续返回栈）
 *   详情页面 → PUSH
 *   返回页面 → POP
 *   Direct Link 返回 → parent fallback（replace）
 *
 * 这些期望将随 Commit 04/05 的 useAppNavigation / Root Replace 逐条实现；
 * 未实现的先以 it.todo 占位，禁止为了让 CI 通过写错误断言。
 */
import { describe, expect, it } from 'vitest';

/**
 * 一级（root）页面集合：Mobile 上互相切换必须 replace（Commit 05）。
 * 详情 / 编辑页面集合：从列表进入必须 PUSH（Commit 15–17）。
 */
export const ROOT_PATHS = ['/', '/agents', '/apps', '/schedules'] as const;

export const DETAIL_PUSH_CASES = [
  { from: '/schedules', to: '/schedules/1' },
  { from: '/enterprise', to: '/enterprise/resources/agents' },
  { from: '/apps', to: '/app/foo' },
] as const;

/** 判断一次跳转期望的 history 语义：root↔root = replace，其余 = push。 */
export function expectedNavigationAction(from: string, to: string): 'replace' | 'push' {
  const isRoot = (path: string) =>
    ROOT_PATHS.includes(path as (typeof ROOT_PATHS)[number]);
  return isRoot(from) && isRoot(to) ? 'replace' : 'push';
}

describe('navigation history semantics (baseline)', () => {
  it('classifies root-to-root switches as replace', () => {
    expect(expectedNavigationAction('/', '/agents')).toBe('replace');
    expect(expectedNavigationAction('/agents', '/apps')).toBe('replace');
    expect(expectedNavigationAction('/apps', '/schedules')).toBe('replace');
  });

  it('classifies root-to-detail as push', () => {
    expect(expectedNavigationAction('/schedules', '/schedules/1')).toBe('push');
    expect(expectedNavigationAction('/enterprise', '/enterprise/resources/agents')).toBe('push');
    expect(expectedNavigationAction('/apps', '/app/foo')).toBe('push');
  });

  describe('Mobile root navigation must replace browser history (Commit 05)', () => {
    it.todo('/ → /agents → /apps uses replace, leaving no stacked back entries');
    it.todo('/agents → /enterprise root switch replaces instead of pushing');
  });

  describe('Detail navigation must push real history (Commit 15–17)', () => {
    it.todo('/schedules → /schedules/1 pushes, so browser back returns to the list');
    it.todo('/enterprise → /enterprise/resources/agents pushes a real entry');
  });

  describe('Back must POP app-pushed entries (Commit 04)', () => {
    it.todo('back() after a pushPage() performs history POP (navigate(-1))');
    it.todo('back() on a direct-link detail replaces to the parent route');
  });
});
