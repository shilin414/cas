/**
 * enterpriseRoutes — 企业控制台 route / nav / permission 的单一事实来源
 * （Architecture 2.0 §29–§31）。
 *
 * enterpriseNav.tsx 的 ENTERPRISE_SECTIONS 由此派生；key / path / label /
 * icon / permission 不再两地维护。权限语义保持现状：
 *   - 多权限 = ANY（任一满足）
 *   - is_staff / is_super_admin bypass（在 permissionDecision 层实现）
 */
import React from 'react';
import {
  ApartmentOutlined,
  AppstoreOutlined,
  AuditOutlined,
  CloudSyncOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
  UserSwitchOutlined,
  ClusterOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { anyPermission, type PermissionRule } from '@/router/permissions';

export interface EnterpriseRouteDefinition {
  id: string;
  /** URL path segment under /enterprise/。 */
  path: string;
  /** 与旧 enterpriseNav key 完全一致（URL 兼容）。 */
  key: string;
  label: string;
  section: string;
  icon: React.ReactNode;
  permission: PermissionRule;
  mobileTitle: string;
}

export const ENTERPRISE_ROUTE_DEFINITIONS: EnterpriseRouteDefinition[] = [
  {
    id: 'enterprise-resource-agents',
    key: 'resources/agents',
    path: 'resources/agents',
    label: '智能体管理',
    section: '资源管理',
    icon: <RobotOutlined />,
    permission: anyPermission('resource.agent.read'),
    mobileTitle: '智能体管理',
  },
  {
    id: 'enterprise-resource-apps',
    key: 'resources/apps',
    path: 'resources/apps',
    label: '应用管理',
    section: '资源管理',
    icon: <AppstoreOutlined />,
    permission: anyPermission('resource.app.read'),
    mobileTitle: '应用管理',
  },
  {
    id: 'enterprise-access-agents',
    key: 'access/agents',
    path: 'access/agents',
    label: '智能体授权',
    section: '权限管理',
    icon: <SafetyCertificateOutlined />,
    permission: anyPermission('access.policy.read'),
    mobileTitle: '智能体授权',
  },
  {
    id: 'enterprise-access-apps',
    key: 'access/apps',
    path: 'access/apps',
    label: '应用授权',
    section: '权限管理',
    icon: <SafetyCertificateOutlined />,
    permission: anyPermission('access.policy.read'),
    mobileTitle: '应用授权',
  },
  {
    id: 'enterprise-access-groups',
    key: 'access/groups',
    path: 'access/groups',
    label: '企业用户组',
    section: '权限管理',
    icon: <ClusterOutlined />,
    permission: anyPermission('access.group.read'),
    mobileTitle: '企业用户组',
  },
  {
    id: 'enterprise-access-diagnosis',
    key: 'access/diagnosis',
    path: 'access/diagnosis',
    label: '权限诊断',
    section: '权限管理',
    icon: <SearchOutlined />,
    permission: anyPermission('access.diagnosis.read'),
    mobileTitle: '权限诊断',
  },
  {
    id: 'enterprise-admins',
    key: 'admins',
    path: 'admins',
    label: '管理员与角色',
    section: '权限管理',
    icon: <UserSwitchOutlined />,
    // 保持现有 ANY 语义：admin.user.read OR admin.role.read。
    permission: anyPermission('admin.user.read', 'admin.role.read'),
    mobileTitle: '管理员与角色',
  },
  {
    id: 'enterprise-directory',
    key: 'directory',
    path: 'directory',
    label: '部门与人员',
    section: '组织架构',
    icon: <TeamOutlined />,
    permission: anyPermission('directory.read'),
    mobileTitle: '部门与人员',
  },
  {
    id: 'enterprise-directory-sync',
    key: 'directory/sync',
    path: 'directory/sync',
    label: '同步管理',
    section: '组织架构',
    icon: <CloudSyncOutlined />,
    permission: anyPermission('directory.sync.read'),
    mobileTitle: '同步管理',
  },
  {
    id: 'enterprise-ai-models',
    key: 'ai-models',
    path: 'ai-models',
    label: 'AI 模型管理',
    section: '平台管理',
    icon: <RobotOutlined />,
    // 保持现有 ANY 语义（ai.model.read / test / log.read 任一）。
    permission: anyPermission('ai.model.read', 'ai.model.test', 'ai.model.log.read'),
    mobileTitle: 'AI 模型管理',
  },
  {
    id: 'enterprise-operations',
    key: 'operations',
    path: 'operations',
    label: '运行中心',
    section: '平台管理',
    icon: <ClusterOutlined />,
    permission: anyPermission('run.monitor.read'),
    mobileTitle: '运行中心',
  },
  {
    id: 'enterprise-providers',
    key: 'providers',
    path: 'providers',
    label: 'Provider',
    section: '平台管理',
    icon: <ApartmentOutlined />,
    permission: anyPermission('provider.read'),
    mobileTitle: 'Provider',
  },
  {
    id: 'enterprise-audit',
    key: 'audit',
    path: 'audit',
    label: '审计日志',
    section: '平台管理',
    icon: <AuditOutlined />,
    permission: anyPermission('audit.read'),
    mobileTitle: '审计日志',
  },
];

export const ENTERPRISE_SECTION_ORDER = [
  '资源管理',
  '权限管理',
  '组织架构',
  '平台管理',
] as const;
