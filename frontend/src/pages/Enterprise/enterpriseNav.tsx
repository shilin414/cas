/**
 * enterpriseNav — 纯 UI helper（Architecture 2.0 §31）。
 *
 * 单一事实来源已迁至 enterpriseRoutes.tsx；本文件的 ENTERPRISE_SECTIONS /
 * 权限字段全部由 route definitions 派生，不再独立维护。
 * key 与 URL 保持与桌面 Sider / 移动首页完全一致。
 */
import React from 'react';
import { ControlOutlined } from '@ant-design/icons';
import { ENTERPRISE_ROUTE_DEFINITIONS, ENTERPRISE_SECTION_ORDER } from './enterpriseRoutes';
import type { PermissionRule } from '@/router/permissions';

export interface EnterpriseNavItem {
  /** URL path segment under /enterprise/ (may carry a query, e.g. access?app=). */
  key: string;
  label: string;
  icon: React.ReactNode;
  /** @deprecated 派生自 route definition；仅兼容旧调用方。 */
  requiredPermission: string;
  /** @deprecated 派生自 route definition；仅兼容旧调用方。 */
  requiredAnyPermissions?: string[];
}

export interface EnterpriseNavSection {
  title: string;
  items: EnterpriseNavItem[];
}

/** 从权限规则提取展示用的 code 列表（兼容旧字段）。 */
function permissionCodes(rule: PermissionRule): string[] {
  return rule.type === 'any' || rule.type === 'all' ? [...rule.permissions] : [];
}

export const ENTERPRISE_SECTIONS: EnterpriseNavSection[] = ENTERPRISE_SECTION_ORDER
  .map((title) => ({
    title,
    items: ENTERPRISE_ROUTE_DEFINITIONS
      .filter((definition) => definition.section === title)
      .map(({ key, label, icon, permission }) => {
        const codes = permissionCodes(permission);
        return {
          key,
          label,
          icon,
          requiredPermission: codes[0] ?? '',
          ...(codes.length > 1 ? { requiredAnyPermissions: codes } : {}),
        };
      }),
  }))
  .filter((section) => section.items.length > 0);

/** 概览不是子路由 definition，单独提供菜单项。 */
export const ENTERPRISE_OVERVIEW_ITEM = {
  key: 'overview',
  icon: <ControlOutlined />,
  label: '概览',
};

export const pagePath = (key: string) => `/enterprise/${key}`;

export const currentKey = (pathname: string) =>
  pathname.replace(/^\/enterprise\/?/, '') || 'overview';

export const fmt = (v?: string | null) => (v ? new Date(v).toLocaleString() : '—');

/** 规则匹配沿用 route definition 的 ANY 语义。 */
function ruleAllows(rule: PermissionRule, permissions: Set<string>, isSuperAdmin: boolean): boolean {
  if (isSuperAdmin) return true;
  if (rule.type === 'any' || rule.type === 'all') {
    return rule.permissions.some((code) => permissions.has(code));
  }
  // enterprise / none 规则不在此判定（入口资格由 Route Boundary 负责）。
  return false;
}

export function filterEnterpriseSections(permissions: Set<string>, isSuperAdmin: boolean) {
  return ENTERPRISE_SECTIONS.map((section) => ({
    ...section,
    items: section.items.filter((item) => {
      const definition = ENTERPRISE_ROUTE_DEFINITIONS.find((entry) => entry.key === item.key);
      if (!definition) {
        // 兼容旧数据：退回 requiredAnyPermissions/requiredPermission 语义。
        return isSuperAdmin
          || (item.requiredAnyPermissions
            ? item.requiredAnyPermissions.some((code) => permissions.has(code))
            : permissions.has(item.requiredPermission));
      }
      return ruleAllows(definition.permission, permissions, isSuperAdmin);
    }),
  })).filter((section) => section.items.length > 0);
}
