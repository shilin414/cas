/**
 * Enterprise 路由定义与导航派生测试（Architecture 2.0 §46/§十二）。
 *
 * 验证 enterpriseRoutes（单一事实来源）与 enterpriseNav（派生 UI helper）：
 *   - 每条子路由定义都有 ANY permission 与 mobileTitle
 *   - ENTERPRISE_SECTIONS 的 key 集与定义完全一致（不两地漂移）
 *   - filterEnterpriseSections 沿用 ANY 语义
 */
import { describe, expect, it } from 'vitest';

import { ENTERPRISE_ROUTE_DEFINITIONS, ENTERPRISE_SECTION_ORDER } from '@/pages/Enterprise/enterpriseRoutes';
import { ENTERPRISE_SECTIONS, filterEnterpriseSections } from '@/pages/Enterprise/enterpriseNav';

describe('enterprise route definitions (§46)', () => {
  it('covers every console child route with ANY permission + mobile title', () => {
    const keys = ENTERPRISE_ROUTE_DEFINITIONS.map((d) => d.key);
    expect(keys).toEqual(expect.arrayContaining([
      'resources/agents', 'resources/apps',
      'access/agents', 'access/apps', 'access/groups', 'access/diagnosis',
      'admins', 'directory', 'directory/sync',
      'ai-models', 'operations', 'providers', 'audit',
    ]));
    // id 全局唯一且与 key 一一对应（route handle 使用）。
    const ids = ENTERPRISE_ROUTE_DEFINITIONS.map((d) => d.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(new Set(ids).size).toBe(ENTERPRISE_ROUTE_DEFINITIONS.length);
    for (const definition of ENTERPRISE_ROUTE_DEFINITIONS) {
      expect(definition.permission.type).toBe('any');
      expect(definition.mobileTitle.length).toBeGreaterThan(0);
      expect(ENTERPRISE_SECTION_ORDER).toContain(definition.section);
    }
  });

  it('derives nav sections from route definitions with identical keys', () => {
    const navKeys = ENTERPRISE_SECTIONS.flatMap((section) => section.items.map((item) => item.key));
    expect(new Set(navKeys)).toEqual(new Set(ENTERPRISE_ROUTE_DEFINITIONS.map((d) => d.key)));
  });

  it('keeps ANY semantics when filtering visible sections', () => {
    // admins 需要 admin.user.read OR admin.role.read：只持有其中一个也可见。
    const visibleWithRoleRead = filterEnterpriseSections(new Set(['admin.role.read']), false)
      .flatMap((section) => section.items.map((item) => item.key));
    expect(visibleWithRoleRead).toContain('admins');

    const visibleWithNeither = filterEnterpriseSections(new Set(['audit.read']), false)
      .flatMap((section) => section.items.map((item) => item.key));
    expect(visibleWithNeither).not.toContain('admins');
    // ai-models 三个权限任一可见（保持现有 ANY 语义）。
    expect(visibleWithNeither).toContain('audit');
    expect(visibleWithNeither).not.toContain('ai-models');
    const visibleWithModelTest = filterEnterpriseSections(new Set(['ai.model.test']), false)
      .flatMap((section) => section.items.map((item) => item.key));
    expect(visibleWithModelTest).toContain('ai-models');
  });

  it('super admin sees every section', () => {
    const visible = filterEnterpriseSections(new Set(), true)
      .flatMap((section) => section.items.map((item) => item.key));
    expect(visible).toEqual(ENTERPRISE_ROUTE_DEFINITIONS.map((d) => d.key));
  });
});
