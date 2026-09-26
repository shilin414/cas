/**
 * MobileEnterpriseLayout — 移动企业控制台 Layout（Architecture 2.0 §42–§44）。
 *
 * 子页面 Header（← 标题）由子路由 meta 提供；这里只渲染容器 + Outlet。
 * 旧的 currentKey/SUBPAGE_TITLES/useMobileHeader(backTo) 全部删除。
 */
import React from 'react';
import { Outlet } from 'react-router-dom';
import '../EnterpriseMobile.css';

export default function MobileEnterpriseLayout() {
  return (
    <div className="enterprise-console-mobile">
      <Outlet />
    </div>
  );
}
