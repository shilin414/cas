/**
 * MobileDirectoryPage — 移动端部门与人员（开发执行报告 §44–§46，二次复审 P1-4）。
 *
 * Segmented 切换 部门/人员，各自带独立搜索词；只读快照（飞书 Directory），
 * 部门详情第一版不做。
 *
 * 二次复审 P1-4 修复：
 *   · 人员走 useDirectoryUsers（cursor 分页 + 服务端搜索）——员工 >100
 *     可继续「加载更多」；
 *   · 部门/人员搜索词彻底拆分：部门是全量快照只做本地过滤（搜索部门不再
 *     顺带请求人员 API），切 Tab 不再继承上一个 Tab 的搜索词；
 *   · 人数标题不再把「当前已加载数」显示成企业总人数（无 total 时只显示
 *     「人员」）；
 *   · 请求失败 ≠ 没有匹配：部门/人员各有独立错误态与重试。
 */
import React, { useState } from 'react';
import { Avatar, Button, Segmented, Skeleton, Tag } from 'antd';
import { useDirectoryDepartments } from '../hooks/useDirectoryDepartments';
import { useDirectoryUsers } from '../hooks/useDirectoryUsers';
import { MobileEmptyState, MobilePage, MobileSearchBar } from '@/components/MobileConsole';
import '../EnterpriseMobile.css';

type DirectoryTab = 'departments' | 'users';

export default function MobileDirectoryPage() {
  const [tab, setTab] = useState<DirectoryTab>('departments');
  const [departmentQuery, setDepartmentQuery] = useState('');
  const [userQuery, setUserQuery] = useState('');

  const departments = useDirectoryDepartments({
    query: departmentQuery,
    enabled: tab === 'departments',
    includeInactive: true,
    limit: 50,
  });
  const users = useDirectoryUsers({
    query: userQuery,
    enabled: tab === 'users',
    includeInactive: true,
    limit: 50,
  });

  const query = tab === 'departments' ? departmentQuery : userQuery;
  const setQuery = tab === 'departments' ? setDepartmentQuery : setUserQuery;
  const retryDepartments = () => (departments.hasMore ? void departments.loadMore() : void departments.refresh());
  const retryUsers = () => (users.hasMore ? void users.loadMore() : void users.refresh());

  return (
    <MobilePage>
      <div className="mobile-console-page__sticky">
        <Segmented block value={tab} onChange={(v) => setTab(v as DirectoryTab)} options={[
          { value: 'departments', label: '部门' },
          { value: 'users', label: '人员' },
        ]} />
        <MobileSearchBar
          placeholder={tab === 'departments' ? '搜索部门' : '搜索姓名'}
          value={query}
          onChange={setQuery}
        />
      </div>

      {tab === 'departments' ? (
        departments.error && departments.items.length === 0 ? (
          <MobileEmptyState title="加载部门失败" action={<Button onClick={() => void departments.refresh()}>重试</Button>} />
        ) : departments.loading && departments.items.length === 0 ? (
          <div style={{ padding: '12px 0' }}><Skeleton active paragraph={{ rows: 1 }} /><Skeleton active paragraph={{ rows: 1 }} /></div>
        ) : departments.items.length === 0 ? (
          <MobileEmptyState title="没有匹配的部门" />
        ) : (
          <>
            <div className="mobile-console-section__rows" style={{ marginTop: 12 }}>
              {departments.items.map((dep) => (
                <div key={dep.id} className="mobile-console-row" style={{ cursor: 'default' }}>
                  <span className="mobile-console-row__body"><span className="mobile-console-row__title"><span>{dep.name}</span></span></span>
                  <Tag color={dep.is_active ? 'green' : 'default'}>{dep.is_active ? '有效' : '停用'}</Tag>
                </div>
              ))}
            </div>
            {(departments.hasMore || (departments.error && departments.items.length > 0)) && (
              <button type="button" className="mobile-console-more" onClick={retryDepartments}>
                {departments.loadingMore ? '加载中…' : departments.error ? '加载失败，点击重试' : '加载更多'}
              </button>
            )}
          </>
        )
      ) : (
        users.error && users.items.length === 0 ? (
          <MobileEmptyState title="加载人员失败" action={<Button onClick={() => void users.refresh()}>重试</Button>} />
        ) : users.loading && users.items.length === 0 ? (
          <div style={{ padding: '12px 0' }}><Skeleton active avatar paragraph={{ rows: 1 }} /><Skeleton active avatar paragraph={{ rows: 1 }} /></div>
        ) : users.items.length === 0 ? (
          <MobileEmptyState title="没有匹配的人员" />
        ) : (
          <>
            <div className="mobile-console-section__rows" style={{ marginTop: 12 }}>
              {users.items.map((user) => (
                <div key={user.id} className="mobile-console-row" style={{ cursor: 'default' }}>
                  <Avatar src={user.avatar_url} size={44}>{user.name.slice(0, 1)}</Avatar>
                  <span className="mobile-console-row__body">
                    <span className="mobile-console-row__title"><span>{user.name}</span></span>
                    <span className="mobile-console-row__meta">{user.departments.map((d) => d.name).join(' / ') || '—'}</span>
                  </span>
                  <span style={{ display: 'flex', flex: 'none', flexDirection: 'column', gap: 4, alignItems: 'flex-end' }}>
                    <Tag color={user.is_active ? 'green' : 'red'}>{user.is_active ? '在职' : '离职'}</Tag>
                    {user.local_user_id ? <Tag color="blue">小安工作助手 已关联</Tag> : <Tag>未登录</Tag>}
                  </span>
                </div>
              ))}
            </div>
            {(users.hasMore || (users.error && users.items.length > 0)) && (
              <button type="button" className="mobile-console-more" onClick={retryUsers}>
                {users.loadingMore ? '加载中…' : users.error ? '加载失败，点击重试' : '加载更多'}
              </button>
            )}
          </>
        )
      )}
    </MobilePage>
  );
}
