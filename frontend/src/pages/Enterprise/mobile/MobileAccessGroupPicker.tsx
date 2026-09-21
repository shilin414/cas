import React, { useEffect, useMemo, useState } from 'react';
import { Drawer, Input, Tag } from 'antd';
import { CheckOutlined, SearchOutlined } from '@ant-design/icons';
import type { AccessGroup } from '../enterpriseApi';
import '../EnterpriseMobile.css';

export default function MobileAccessGroupPicker({ open, groups, selectedIds, onClose, onDone }: { open: boolean; groups: AccessGroup[]; selectedIds: number[]; onClose: () => void; onDone: (ids: number[]) => void }) {
  const [query, setQuery] = useState(''); const [picked, setPicked] = useState<number[]>(selectedIds);
  useEffect(() => { if (open) { setPicked(selectedIds); setQuery(''); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const filtered = useMemo(() => { const q=query.trim().toLowerCase(); return groups.filter((group) => group.enabled && (!q || group.name.toLowerCase().includes(q))); }, [groups,query]);
  const toggle=(id:number)=>setPicked((items)=>items.includes(id)?items.filter((item)=>item!==id):[...items,id]);
  return <Drawer placement="bottom" open={open} onClose={onClose} height="72dvh" closable={false} title={null} rootClassName="mobile-bottom-sheet" styles={{content:{borderRadius:'24px 24px 0 0'},body:{padding:0,display:'flex',flexDirection:'column',minHeight:0}}}><div className="mobile-sheet"><div className="mobile-sheet__handle"/><div className="mobile-sheet__header"><span className="mobile-sheet__title">选择权限组</span></div><div className="mobile-picker"><div className="mobile-picker__search"><Input allowClear value={query} placeholder="搜索权限组" prefix={<SearchOutlined/>} onChange={(event)=>setQuery(event.target.value)}/></div><div className="mobile-picker__body">{filtered.map((group)=>{const checked=picked.includes(group.id);return <button key={group.id} type="button" className="mobile-picker__row" aria-pressed={checked} onClick={()=>toggle(group.id)}><span className="mobile-picker__row-body"><span className="mobile-picker__row-name">{group.name}</span><span className="mobile-picker__row-meta"><Tag>{group.source_type==='feishu'?(group.external_group_type==='dynamic'?'飞书·动态':'飞书'):'本地'}</Tag>覆盖 {group.covered_users} 人</span></span><span className={`mobile-picker__check${checked?' mobile-picker__check--on':''}`}><CheckOutlined/></span></button>})}</div><div className="mobile-picker__footer"><span className="mobile-picker__footer-count">已选择 {picked.length} 个</span><button type="button" className="mobile-picker__done" onClick={()=>onDone(picked)}>完成</button></div></div></div></Drawer>;
}
