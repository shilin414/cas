import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Checkbox, Form, Input, Modal, Select, Space, Switch, Table, Tabs, Tag, Typography, message } from 'antd';
import { enterpriseApi, type Administrator, type AdminPermission, type AdminRole } from '../enterpriseApi';
import { useDirectoryUsers } from '../hooks/useDirectoryUsers';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { groupAdminPermissions } from '../permissionCatalog';

export default function AdminsPage() {
  const canReadUsers = useAdminPermissionStore((state) => state.can('admin.user.read'));
  const canReadRoles = useAdminPermissionStore((state) => state.can('admin.role.read'));
  const canManageUsers = useAdminPermissionStore((state) => state.can('admin.user.manage'));
  const canManageRoles = useAdminPermissionStore((state) => state.can('admin.role.manage'));
  const [roles, setRoles] = useState<AdminRole[]>([]);
  const [permissions, setPermissions] = useState<AdminPermission[]>([]);
  const [admins, setAdmins] = useState<Administrator[]>([]);
  const [loading, setLoading] = useState(true);
  const [roleOpen, setRoleOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<AdminRole | null>(null);
  const [userQuery, setUserQuery] = useState('');
  const [roleForm] = Form.useForm();
  const [adminForm] = Form.useForm();
  const directoryUsers = useDirectoryUsers({ query: userQuery, enabled: adminOpen && canManageUsers, includeInactive: false, sessionKey: adminOpen ? 'admin-picker' : null });

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [roleResult, permissionResult, adminResult] = await Promise.allSettled([
        canReadRoles ? enterpriseApi.roles() : Promise.resolve([]),
        canReadRoles ? enterpriseApi.permissions() : Promise.resolve([]),
        canReadUsers ? enterpriseApi.administrators() : Promise.resolve([]),
      ]);
      setRoles(roleResult.status === 'fulfilled' ? roleResult.value : []);
      setPermissions(permissionResult.status === 'fulfilled' ? permissionResult.value : []);
      setAdmins(adminResult.status === 'fulfilled' ? adminResult.value : []);
    } finally { setLoading(false); }
  }, [canReadRoles, canReadUsers]);
  useEffect(() => { void load(); }, [load]);
  const permissionGroups = useMemo(() => groupAdminPermissions(permissions), [permissions]);
  const saveRole = async () => { if (!canManageRoles) return; const values = await roleForm.validateFields(); if (editingRole) await enterpriseApi.updateRole(editingRole.id, values); else await enterpriseApi.createRole(values); message.success('角色已保存'); setRoleOpen(false); void load(); };
  const saveAdmin = async () => { if (!canManageUsers) return; const values = await adminForm.validateFields(); await enterpriseApi.updateAdministrator(values.user_id, values.role_ids); message.success('管理员角色已更新'); setAdminOpen(false); void load(); };
  const openAdmin = (admin?: Administrator) => { setUserQuery(''); adminForm.setFieldsValue({ user_id: admin?.user_id, role_ids: admin?.assignments.map((assignment) => assignment.role_id) ?? [] }); setAdminOpen(true); };
  const openRole = (role?: AdminRole) => { setEditingRole(role ?? null); roleForm.setFieldsValue({ code: role?.code ?? '', name: role?.name ?? '', description: role?.description ?? '', enabled: role?.enabled ?? true, permission_codes: role?.permissions.map((permission) => permission.code) ?? [] }); setRoleOpen(true); };

  return <section className="enterprise-section">
    <div className="enterprise-section__head"><div><h2>管理员与角色</h2><p>后台管理权限与资源使用权限相互独立；可为同一管理员分配多个角色。</p></div></div>
    <Tabs items={[
      ...(canReadUsers ? [{ key: 'admins', label: '管理员', children: <Card extra={canManageUsers ? <Button type="primary" onClick={() => openAdmin()}>添加管理员</Button> : undefined}><Table loading={loading} rowKey="user_id" dataSource={admins} pagination={false} columns={[
        { title: '管理员', render: (_: unknown, row: Administrator) => <><strong>{row.display_name || row.username}</strong><br/><Typography.Text type="secondary">{row.username}</Typography.Text></> },
        { title: '身份', render: (_: unknown, row: Administrator) => row.is_staff ? <Tag color="red">超级管理员</Tag> : <Tag>角色授权</Tag> },
        { title: '角色', render: (_: unknown, row: Administrator) => <Space wrap>{row.assignments.map((assignment) => <Tag key={assignment.id}>{assignment.role_name}</Tag>)}</Space> },
        { title: '状态', render: (_: unknown, row: Administrator) => <Tag color={row.is_active ? 'green' : 'default'}>{row.is_active ? '有效' : '停用'}</Tag> },
        { title: '操作', render: (_: unknown, row: Administrator) => row.is_staff ? '系统超级管理员' : canManageUsers ? <Button size="small" onClick={() => openAdmin(row)}>编辑角色</Button> : '只读' },
      ]}/></Card> }] : []),
      ...(canReadRoles ? [{ key: 'roles', label: '角色', children: <Card extra={canManageRoles ? <Button type="primary" onClick={() => openRole()}>新建自定义角色</Button> : undefined}><Table loading={loading} rowKey="id" dataSource={roles} pagination={false} columns={[
        { title: '角色', render: (_: unknown, row: AdminRole) => <><strong>{row.name}</strong><br/><Typography.Text type="secondary">{row.description || row.code}</Typography.Text></> },
        { title: '类型', render: (_: unknown, row: AdminRole) => <Tag color={row.is_system ? 'blue' : 'gold'}>{row.is_system ? '系统角色' : '自定义'}</Tag> },
        { title: '权限', render: (_: unknown, row: AdminRole) => `${row.permissions.length} 项` },
        { title: '状态', render: (_: unknown, row: AdminRole) => row.enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag> },
        { title: '操作', render: (_: unknown, row: AdminRole) => <Button size="small" onClick={() => openRole(row)}>{row.is_system || !canManageRoles ? '查看' : '编辑'}</Button> },
      ]}/></Card> }] : []),
    ]}/>
    <Modal width={860} title={editingRole ? '角色详情' : '新建自定义角色'} open={roleOpen} onCancel={() => setRoleOpen(false)} onOk={editingRole?.is_system || !canManageRoles ? () => setRoleOpen(false) : () => void saveRole()} okText={editingRole?.is_system || !canManageRoles ? '关闭' : '保存'} cancelButtonProps={{ style: editingRole?.is_system || !canManageRoles ? { display: 'none' } : undefined }}>
      <Form layout="vertical" form={roleForm} disabled={Boolean(editingRole?.is_system || !canManageRoles)}>
        <Alert type="info" showIcon message="按工作职责选择最小必要权限" description="查看类权限只允许读取；管理、测试、导出等权限会执行实际操作。建议优先从少量权限开始。" style={{ marginBottom: 16 }}/>
        <div className="enterprise-role-fields"><Form.Item name="code" label="角色代码" rules={[{ required: true }]}><Input disabled={Boolean(editingRole)} placeholder="例如 finance_auditor" /></Form.Item><Form.Item name="name" label="角色名称" rules={[{ required: true }]}><Input placeholder="例如 财务审计员" /></Form.Item></div>
        <Form.Item name="description" label="职责说明"><Input.TextArea placeholder="说明这个角色适合谁、负责什么" /></Form.Item><Form.Item name="enabled" label="启用角色" valuePropName="checked"><Switch /></Form.Item>
        <Form.Item name="permission_codes" label="权限范围"><Checkbox.Group className="enterprise-permission-groups">{permissionGroups.map((group) => <Card key={group.key} size="small" className="enterprise-permission-group" title={<div><strong>{group.label}</strong><Typography.Text type="secondary" className="enterprise-permission-group__desc">{group.description}</Typography.Text></div>}><div className="enterprise-permission-group__items">{group.permissions.map((permission) => <Checkbox key={permission.code} value={permission.code}><span className="enterprise-permission-item"><strong>{permission.name}</strong><small>{permission.description}</small></span></Checkbox>)}</div></Card>)}</Checkbox.Group></Form.Item>
      </Form>
    </Modal>
    <Modal title="分配管理员角色" open={adminOpen} onCancel={() => setAdminOpen(false)} onOk={() => void saveAdmin()}>
      <Form layout="vertical" form={adminForm}><Form.Item name="user_id" label="企业用户" rules={[{ required: true }]}><Select showSearch filterOption={false} searchValue={userQuery} onSearch={setUserQuery} loading={directoryUsers.loading} onPopupScroll={(event) => { const { scrollTop, scrollHeight, clientHeight } = event.currentTarget; if (directoryUsers.hasMore && !directoryUsers.loadingMore && scrollHeight - scrollTop - clientHeight < 24) void directoryUsers.loadMore(); }} options={directoryUsers.items.filter((user) => user.local_user_id).map((user) => ({ value: user.local_user_id!, label: `${user.name} · ${user.open_id}` }))}/></Form.Item><Form.Item name="role_ids" label="角色" rules={[{ required: true }]}><Select mode="multiple" options={roles.filter((role) => role.enabled).map((role) => ({ value: role.id, label: role.name }))}/></Form.Item></Form>
    </Modal>
  </section>;
}
