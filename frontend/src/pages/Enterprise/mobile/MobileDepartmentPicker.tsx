import React, { useMemo, useState } from 'react';
import { Drawer, Input } from 'antd';
import { CheckOutlined, DownOutlined, RightOutlined, SearchOutlined } from '@ant-design/icons';
import type { DirectoryDepartment } from '../enterpriseApi';
import { flattenDepartmentTree } from '../departmentTree';
import '../EnterpriseMobile.css';

export interface MobileDepartmentPickerProps {
  open: boolean;
  departments: DirectoryDepartment[];
  selectedIds: number[];
  onClose: () => void;
  onDone: (ids: number[]) => void;
}

function pathOf(dep: DirectoryDepartment, byId: Map<number, DirectoryDepartment>): string {
  const parts: string[] = [];
  let node: DirectoryDepartment | undefined = dep;
  let guard = 0;
  while (node && guard++ < 20) {
    parts.unshift(node.name);
    node = node.parent_id ? byId.get(node.parent_id) : undefined;
  }
  return parts.join(' / ');
}

export default function MobileDepartmentPicker({ open, departments, selectedIds, onClose, onDone }: MobileDepartmentPickerProps) {
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<number[]>(selectedIds);
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());

  React.useEffect(() => {
    if (open) { setPicked(selectedIds); setQuery(''); setCollapsed(new Set()); }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const byId = useMemo(() => new Map(departments.map((department) => [department.id, department])), [departments]);
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return flattenDepartmentTree(departments, collapsed);
    const matches = new Set<number>();
    departments.forEach((department) => {
      if (!department.name.toLowerCase().includes(q)) return;
      let node: DirectoryDepartment | undefined = department;
      let guard = 0;
      while (node && guard++ < 20) {
        matches.add(node.id);
        node = node.parent_id ? byId.get(node.parent_id) : undefined;
      }
    });
    return flattenDepartmentTree(departments).filter((row) => matches.has(row.department.id));
  }, [departments, collapsed, query, byId]);

  const togglePicked = (id: number) => setPicked((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  const toggleCollapsed = (id: number) => setCollapsed((current) => {
    const next = new Set(current);
    if (next.has(id)) next.delete(id); else next.add(id);
    return next;
  });

  return <Drawer placement="bottom" open={open} onClose={onClose} height="72dvh" closable={false} title={null} rootClassName="mobile-bottom-sheet" styles={{ content: { borderRadius: '24px 24px 0 0' }, body: { padding: 0, display: 'flex', flexDirection: 'column', minHeight: 0 } }}>
    <div className="mobile-sheet"><div className="mobile-sheet__handle" aria-hidden /><div className="mobile-sheet__header"><span className="mobile-sheet__title">选择部门</span></div><div className="mobile-picker">
      <div className="mobile-picker__search"><Input allowClear value={query} placeholder="搜索部门名称" prefix={<SearchOutlined style={{ color: 'var(--color-text-dim)' }} />} onChange={(event) => setQuery(event.target.value)} /></div>
      <div className="mobile-picker__body">{rows.length === 0 ? <div className="mobile-console-empty">没有匹配的部门</div> : rows.map(({ department, depth, hasChildren }) => {
        const checked = picked.includes(department.id);
        const isCollapsed = collapsed.has(department.id);
        return <div key={department.id} className="mobile-picker__tree-row" style={{ paddingLeft: 12 + depth * 22 }}>
          <button type="button" className="mobile-picker__tree-toggle" aria-label={hasChildren ? `${isCollapsed ? '展开' : '收起'} ${department.name}` : undefined} disabled={!hasChildren || Boolean(query)} onClick={() => toggleCollapsed(department.id)}>{hasChildren ? (isCollapsed ? <RightOutlined /> : <DownOutlined />) : <span />}</button>
          <button type="button" className="mobile-picker__row mobile-picker__row--tree" aria-pressed={checked} onClick={() => togglePicked(department.id)}><span className="mobile-picker__row-body"><span className="mobile-picker__row-name">{department.name}</span><span className="mobile-picker__row-meta">{pathOf(department, byId)}</span></span><span className={`mobile-picker__check${checked ? ' mobile-picker__check--on' : ''}`}><CheckOutlined /></span></button>
        </div>;
      })}</div>
      <div className="mobile-picker__footer"><span className="mobile-picker__footer-count">已选择 {picked.length} 个</span><button type="button" className="mobile-picker__done" onClick={() => onDone(picked)}>完成</button></div>
    </div></div>
  </Drawer>;
}
