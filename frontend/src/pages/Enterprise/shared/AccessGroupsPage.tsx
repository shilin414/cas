import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Card, Empty, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tabs, Tag, TreeSelect, Typography, message } from 'antd';
import { enterpriseApi, type AccessGroup, type DirectoryDepartment } from '../enterpriseApi';
import { useDirectoryUsers } from '../hooks/useDirectoryUsers';
import { useAdminPermissionStore } from '@/stores/useAdminPermissionStore';
import { useIsMobile } from '@/shell/useIsMobile';
import { buildDepartmentTree } from '../departmentTree';

type DepartmentGrant = { department_id: number; name: string; include_children: boolean; covered_users: number };
type UserGrant = { directory_user_id: number; name: string; avatar_url: string };

export function mergeDepartmentGrants(ids: number[], current: DepartmentGrant[], departments: DirectoryDepartment[]): DepartmentGrant[] {
  return ids.map((id) => current.find((item) => item.department_id === id) ?? { department_id: id, name: departments.find((item) => item.id === id)?.name ?? '', include_children: false, covered_users: 0 });
}

export default function AccessGroupsPage() {
  const isMobile = useIsMobile();
  const canManage = useAdminPermissionStore((state) => state.can('access.group.manage'));
  const [groups, setGroups] = useState<AccessGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<AccessGroup | null>(null);
  const [departmentGrants, setDepartmentGrants] = useState<DepartmentGrant[]>([]);
  const [departments, setDepartments] = useState<DirectoryDepartment[]>([]);
  const [departmentsLoading, setDepartmentsLoading] = useState(false);
  const [userGrants, setUserGrants] = useState<UserGrant[]>([]);
  const [userQuery, setUserQuery] = useState('');
  const editRequest = useRef(0);
  const [form] = Form.useForm();
  const sessionKey = editing?.id ?? (open ? 'new' : null);
  const directoryUsers = useDirectoryUsers({ query: userQuery, enabled: open && canManage && editing?.source_type !== 'feishu', includeInactive: false, sessionKey });

  const load = useCallback(async () => {
    setLoading(true);
    try { setGroups(await enterpriseApi.accessGroups()); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    if (!open || editing?.source_type === 'feishu') return;
    let current = true;
    setDepartmentsLoading(true);
    void enterpriseApi.departments().then((items) => {
      if (current) setDepartments(items);
    }).catch(() => {
      if (current) message.error('加载部门树失败');
    }).finally(() => {
      if (current) setDepartmentsLoading(false);
    });
    return () => { current = false; };
  }, [open, editing?.source_type]);

  const openNew = () => {
    editRequest.current += 1; setEditing(null); setDepartmentGrants([]); setUserGrants([]); setUserQuery('');
    form.setFieldsValue({ code: '', name: '', description: '', enabled: true }); setOpen(true);
  };
  const edit = async (group: AccessGroup) => {
    const request = ++editRequest.current;
    const detail = await enterpriseApi.accessGroup(group.id);
    if (request !== editRequest.current) return;
    setEditing(detail); setDepartmentGrants(detail.departments ?? []); setUserGrants(detail.users ?? []); setUserQuery('');
    form.setFieldsValue({ code: detail.code, name: detail.name, description: detail.description, enabled: detail.enabled }); setOpen(true);
  };
  const close = () => { editRequest.current += 1; setOpen(false); };
  const save = async () => {
    if (!canManage) return;
    const values = await form.validateFields();
    const payload = { code: values.code, name: values.name, description: values.description ?? '', enabled: values.enabled, department_grants: departmentGrants.map(({ department_id, include_children }) => ({ department_id, include_children })), user_grants: userGrants.map((item) => item.directory_user_id) };
    if (editing) await enterpriseApi.updateAccessGroup(editing.id, payload); else await enterpriseApi.createAccessGroup(payload);
    message.success('权限组已保存'); close(); void load();
  };
  const departmentOptions = useMemo<DirectoryDepartment[]>(() => {
    const byId = new Map(departments.map((item) => [item.id, item]));
    departmentGrants.forEach((grant) => {
      if (!byId.has(grant.department_id)) byId.set(grant.department_id, { id: grant.department_id, name: grant.name, open_department_id: '', parent_open_department_id: '', order_weight: '', is_active: true });
    });
    return Array.from(byId.values());
  }, [departments, departmentGrants]);
  const departmentTree = useMemo(() => buildDepartmentTree(departmentOptions), [departmentOptions]);
  const setDepartmentIds = (ids: number[]) => setDepartmentGrants(mergeDepartmentGrants(ids, departmentGrants, departmentOptions));
  const userOptions = useMemo(() => {
    const map = new Map<number, UserGrant>();
    userGrants.forEach((item) => map.set(item.directory_user_id, item));
    directoryUsers.items.forEach((item) => map.set(item.id, { directory_user_id: item.id, name: item.name, avatar_url: item.avatar_url }));
    return Array.from(map.values());
  }, [directoryUsers.items, userGrants]);
  const setUserIds = (ids: number[]) => setUserGrants(ids.map((id) => userOptions.find((item) => item.directory_user_id === id)!).filter(Boolean));

  const columns: any[] = [
    { title: '名称', render: (_: unknown, group: AccessGroup) => <><strong>{group.name}</strong><br/><Typography.Text type="secondary">{group.description || group.code}</Typography.Text></> },
    { title: '来源', render: (_: unknown, group: AccessGroup) => <Tag color={group.source_type === 'feishu' ? 'blue' : 'gold'}>{group.source_type === 'feishu' ? (group.external_group_type === 'dynamic' ? '飞书·动态' : '飞书') : '本地'}</Tag> },
    { title: '成员/覆盖', render: (_: unknown, group: AccessGroup) => `${group.member_count} / ${group.covered_users}` },
    { title: '已授权资源', dataIndex: 'application_count' },
    { title: '状态', render: (_: unknown, group: AccessGroup) => <Tag color={group.enabled ? 'green' : 'default'}>{group.enabled ? group.sync_status || '启用' : '停用'}</Tag> },
    { title: '操作', render: (_: unknown, group: AccessGroup) => <Space><Button size="small" onClick={() => void edit(group)}>{group.source_type === 'feishu' || !canManage ? '查看' : '编辑'}</Button>{canManage && group.source_type === 'local' && <Popconfirm title="确认删除该权限组？" onConfirm={() => enterpriseApi.deleteAccessGroup(group.id).then(() => { message.success('已删除'); void load(); })}><Button danger size="small">删除</Button></Popconfirm>}</Space> },
  ];
  const renderGroups = (items: AccessGroup[], source?: 'feishu') => {
    if (!loading && items.length === 0) return source === 'feishu' ? <Space direction="vertical" style={{ width: '100%' }}><Alert type="warning" showIcon message="本次同步没有获取到飞书用户组" description="请在飞书开放平台为应用开通“获取用户组信息（contact:group:readonly）”，并将通讯录权限范围设为全部员工，然后到“同步管理”手动同步。"/><Empty description="暂无可见的飞书用户组"/></Space> : <Empty description="暂无权限组"/>;
    return isMobile ? <Space direction="vertical" style={{ width: '100%' }}>{items.map((group) => <Card key={group.id} size="small" title={group.name} extra={<Tag>{group.source_type === 'feishu' ? '飞书' : '本地'}</Tag>}><p>{group.description || group.code}</p><p>覆盖 {group.covered_users} 人 · 已授权 {group.application_count} 个资源</p><Button block onClick={() => void edit(group)}>{group.source_type === 'feishu' || !canManage ? '查看详情' : '编辑'}</Button></Card>)}</Space> : <Table rowKey="id" loading={loading} dataSource={items} columns={columns} pagination={false}/>;
  };

  return <section className="enterprise-section">
    <div className="enterprise-section__head"><div><h2>企业用户组</h2><p>飞书组只读同步，本地组可组合部门与人员；权限组不支持嵌套与拒绝规则。</p></div>{canManage && <Button type="primary" onClick={openNew}>新建本地权限组</Button>}</div>
    <Card loading={loading}><Tabs items={[{ key: 'all', label: '全部', children: renderGroups(groups) }, { key: 'feishu', label: '飞书', children: renderGroups(groups.filter((group) => group.source_type === 'feishu'), 'feishu') }, { key: 'local', label: '本地', children: renderGroups(groups.filter((group) => group.source_type === 'local')) }]} /></Card>
    <Modal width={isMobile ? 'calc(100vw - 24px)' : 680} title={editing?.source_type === 'feishu' ? '飞书用户组详情' : editing ? '编辑本地权限组' : '新建本地权限组'} open={open} onCancel={close} onOk={!canManage ? close : () => void save()} okText={!canManage ? '关闭' : '保存'} cancelButtonProps={{ style: !canManage ? { display: 'none' } : undefined }}>
      <Form layout="vertical" form={form} disabled={!canManage}>
        <Form.Item name="code" label="代码" rules={[{ required: true }]}><Input disabled={Boolean(editing)} /></Form.Item><Form.Item name="name" label="名称" rules={[{ required: true }]}><Input disabled={editing?.source_type === 'feishu'} /></Form.Item><Form.Item name="description" label="说明"><Input.TextArea disabled={editing?.source_type === 'feishu'} /></Form.Item><Form.Item name="enabled" label="参与访问控制" valuePropName="checked"><Switch /></Form.Item>
        {editing?.source_type !== 'feishu' && <>
        <Form.Item label="部门" extra="子部门会显示在所属上级部门下；是否连同子部门授权可在下方逐项设置。"><TreeSelect treeData={departmentTree} multiple showSearch treeNodeFilterProp="title" loading={departmentsLoading} value={departmentGrants.map((item) => item.department_id)} onChange={(ids) => setDepartmentIds(ids as number[])} placeholder="按组织层级选择部门" maxTagCount="responsive" /></Form.Item>
        <Space direction="vertical" style={{ width: '100%', marginBottom: 16 }}>{departmentGrants.map((grant) => <Card key={grant.department_id} size="small"><Space style={{ width: '100%', justifyContent: 'space-between' }}><span>{grant.name}</span><Switch checked={grant.include_children} checkedChildren="含子部门" unCheckedChildren="仅本部门" onChange={(checked) => setDepartmentGrants((items) => items.map((item) => item.department_id === grant.department_id ? { ...item, include_children: checked } : item))} /></Space></Card>)}</Space>
        <Form.Item label="人员"><Select mode="multiple" showSearch filterOption={false} searchValue={userQuery} onSearch={setUserQuery} value={userGrants.map((item) => item.directory_user_id)} onChange={setUserIds} onPopupScroll={(event) => { const { scrollTop, scrollHeight, clientHeight } = event.currentTarget; if (directoryUsers.hasMore && !directoryUsers.loadingMore && scrollHeight - scrollTop - clientHeight < 24) void directoryUsers.loadMore(); }} options={userOptions.map((item) => ({ value: item.directory_user_id, label: item.name }))} /></Form.Item>
        </>}
      </Form>
    </Modal>
  </section>;
}
