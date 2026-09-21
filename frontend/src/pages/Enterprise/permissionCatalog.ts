import type { AdminPermission } from './enterpriseApi';

const CATEGORY_META: Record<string, { label: string; description: string; order: number }> = {
  overview: { label: '企业总览', description: '查看企业管理首页与关键指标', order: 10 },
  resource: { label: '智能体与应用', description: '管理智能体、固定应用及资源治理', order: 20 },
  access: { label: '访问权限', description: '配置资源可见范围、权限组与访问诊断', order: 30 },
  directory: { label: '组织与同步', description: '查看组织目录并管理飞书同步', order: 40 },
  rbac: { label: '管理员与角色', description: '管理后台管理员和自定义角色', order: 50 },
  ai: { label: 'AI 模型', description: '模型与连接管理、独立密钥写入、模型测试及调用元数据', order: 55 },
  provider: { label: 'Provider', description: '查看、配置和测试模型服务商', order: 60 },
  runtime: { label: '运行环境', description: '查看和维护 Runtime Binding', order: 70 },
  run: { label: '运行管理', description: '查看运行监控并执行管理动作', order: 80 },
  audit: { label: '审计', description: '查看和导出企业审计日志', order: 90 },
  settings: { label: '企业设置', description: '查看和维护企业级设置', order: 100 },
};

export interface PermissionGroup {
  key: string;
  label: string;
  description: string;
  permissions: AdminPermission[];
}

export function groupAdminPermissions(permissions: AdminPermission[]): PermissionGroup[] {
  const groups = new Map<string, AdminPermission[]>();
  permissions.forEach((permission) => groups.set(permission.category, [...(groups.get(permission.category) ?? []), permission]));
  return Array.from(groups.entries()).sort(([a], [b]) => (CATEGORY_META[a]?.order ?? 999) - (CATEGORY_META[b]?.order ?? 999) || a.localeCompare(b)).map(([key, items]) => ({
    key,
    label: CATEGORY_META[key]?.label ?? key,
    description: CATEGORY_META[key]?.description ?? '其他后台管理权限',
    permissions: [...items].sort((a, b) => a.code.localeCompare(b.code)),
  }));
}
