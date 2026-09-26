/**
 * DesktopEnterpriseLayout — 桌面企业控制台 Layout（Architecture 2.0 §38–§41）。
 *
 * 职责只剩：Sider 菜单 + <Outlet/>。页面组件由子路由渲染，
 * 页面级 pathname switch 已删除。
 */
import React, { useMemo } from 'react';
import { Outlet } from 'react-router-dom';
import { Layout, Menu } from 'antd';
import { useLocation, useNavigate } from 'react-router-dom';
import { ENTERPRISE_OVERVIEW_ITEM, filterEnterpriseSections, currentKey, pagePath } from '../enterpriseNav';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import '../EnterprisePage.css';

const { Sider, Content } = Layout;

export default function DesktopEnterpriseLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  // 只用于菜单当前高亮，不用于决定页面组件（§40）。
  const key = currentKey(location.pathname);
  const identity = useAdminPermissionStore((state) => state.identity);
  const permissions = useMemo(() => new Set(identity?.permissions.map((item) => item.code) ?? []), [identity]);
  const sections = useMemo(() => filterEnterpriseSections(permissions, Boolean(identity?.is_super_admin)), [permissions, identity?.is_super_admin]);

  return (
    <Layout className="enterprise-console">
      <Sider width={220} theme="light" className="enterprise-sider">
        <div className="enterprise-brand">企业控制台</div>
        <Menu
          mode="inline"
          selectedKeys={[key.split('?')[0]]}
          items={[
            { key: ENTERPRISE_OVERVIEW_ITEM.key, icon: ENTERPRISE_OVERVIEW_ITEM.icon, label: ENTERPRISE_OVERVIEW_ITEM.label },
            ...sections.map((section) => ({
              type: 'group' as const,
              label: section.title,
              children: section.items.map(({ key: k, icon, label }) => ({
                key: k,
                icon,
                label,
              })),
            })),
          ]}
          // PC 企业内部导航属于正常页面导航（§41）。
          onClick={({ key: k }) => navigate(pagePath(k))}
        />
      </Sider>
      <Content className="enterprise-content"><Outlet /></Content>
    </Layout>
  );
}
