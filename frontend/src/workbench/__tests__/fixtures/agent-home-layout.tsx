/** Browser layout fixture: real component + real CSS, fake read-only data, no authentication. */
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import AgentWorkspaceCollections from '../../home/AgentWorkspaceCollections';
import WorkbenchHome from '../../home/WorkbenchHome';
import { api } from '@/services/api';
import '@/styles/global.css';
import '@/components/Chat/chatSurface.css';
import '../../home/workbench-home.css';

const longText = '这是一段非常长的任务标题以及生成内容摘要，需要在列表内容列内正确截断而不是覆盖右侧时间和状态。'.repeat(12);
const longToken = 'LongUnbrokenGeneratedResourceName'.repeat(16) + '.xlsx';
api.get = async <T,>(url: string): Promise<T> => {
  if (url === '/v2/tasks') return {items:[{id:'1',title:longText,preview:longText,application_id:7,updated_at:'2026-09-22T01:00:00Z',created_at:'2026-09-22T01:00:00Z',execution_state:'idle'}],next_cursor:''} as T;
  if (url === '/v2/schedules') return [{id:1,name:longText,enabled:false,schedule_type:'daily',trigger:{time:'15:00'},timezone:'Asia/Shanghai',application_id:7}] as T;
  if (url === '/v2/workspace/artifacts') return {items:[{id:'00000000-0000-0000-0000-000000000001',name:longToken,task_title:longText,conversation_id:'1',normalized_type:'file',created_at:'2026-09-22T01:00:00Z'}],next_cursor:''} as T;
  throw new Error(`Unexpected fixture request: ${url}`);
};

function verifyLayout() {
  const failures:string[]=[];
  const identity = document.querySelector<HTMLElement>('.agent-home-identity');
  const avatar = identity?.querySelector<HTMLElement>('.agent-avatar');
  const body = identity?.querySelector<HTMLElement>('.agent-home-identity__body');
  if (!identity || !avatar || !body) {
    failures.push('Desktop identity missing');
  } else {
    const face = avatar.getBoundingClientRect();
    const copy = body.getBoundingClientRect();
    if (Math.abs(face.top - copy.top) > 1) failures.push(`Avatar shifted down ${Math.round(face.top-copy.top)}px`);
    if (Math.abs(face.width-60)>1 || Math.abs(face.height-60)>1) failures.push('Avatar size changed');
    if (body.scrollWidth > body.clientWidth+1) failures.push('Description overflows its column');
    const paragraph = body.querySelector('p')!;
    if (getComputedStyle(paragraph).textAlign !== 'left') failures.push('Description inherited centered chat styles');
    if (paragraph.scrollWidth > paragraph.clientWidth+1) failures.push('Description text overflows paragraph');
    if (avatar.getBoundingClientRect().right > copy.left) failures.push('Avatar overlaps text');
  }
  const rows=Array.from(document.querySelectorAll<HTMLElement>('.agent-home-row'));
  if (!rows.length) failures.push('No rows rendered');
  rows.forEach(row=>{
    const main=row.querySelector<HTMLElement>('.agent-home-row__main')!;
    const bound=main.getBoundingClientRect();
    for(const child of Array.from(main.children)) {
      const box=child.getBoundingClientRect();
      if(box.right>bound.right+1 || box.left<bound.left-1) failures.push(`${child.tagName} escapes main column by ${Math.round(box.right-bound.right)}px`);
      const css=getComputedStyle(child);
      if(css.textOverflow!=='ellipsis' || css.overflowX!=='hidden') failures.push(`${child.tagName} lacks truncation`);
    }
    const rowBox=row.getBoundingClientRect();
    if(row.scrollWidth>row.clientWidth+1) failures.push(`row scrollWidth ${row.scrollWidth} exceeds ${row.clientWidth}`);
    for (const child of Array.from(row.children)) {if(child.getBoundingClientRect().right>rowBox.right+1) failures.push('Row child escapes collection');}
  });
  const output=document.querySelector('output')!;
  output.textContent=failures.length ? `FAIL: ${failures.join('; ')}` : 'PASS: avatar top-aligned at 60px; description and rows contained';
}
// Also stress non-stretched flex children: clipping must not depend on alignment.
function Fixture() {
  const [intrinsic, setIntrinsic] = useState(false);
  const [description, setDescription] = useState('long');
  const descriptions: Record<string, string> = {
    short: '协助你处理日常 IT 工作。',
    long: '大家好，我是 IT 小安！我能解答 IT 问题，并协助处理权限申请、软件开通、电脑和网络故障。'.repeat(5),
    token: 'https://example.invalid/' + 'UnbrokenDescription'.repeat(45),
  };
  return <MemoryRouter>
    <style>{`*{box-sizing:border-box}body{margin:0;font:14px Arial}main{margin-left:272px;padding:32px;max-width:1336px}nav{padding:12px}output{display:block;padding:12px;overflow-wrap:anywhere}.fixture-intrinsic .agent-home-row__main{align-items:flex-start}`}</style>
    <nav><label>描述长度<select aria-label="描述长度" value={description} onChange={event => setDescription(event.target.value)}><option value="short">短描述</option><option value="long">多行描述</option><option value="token">连续长字符</option></select></label><label><input type="checkbox" checked={intrinsic} onChange={event => setIntrinsic(event.target.checked)} />非拉伸布局压力测试</label><button onClick={verifyLayout}>运行布局断言</button><output>等待列表加载</output></nav>
    <main className={`chat-container agent-home-surface${intrinsic ? ' fixture-intrinsic' : ''}`}><div className="chat-empty"><WorkbenchHome current={{id:7,slug:'fixture',name:'IT小安',description:descriptions[description],kind:'chat',icon:'🛠️',color:'#2864dc'}} onOpen={() => undefined} /></div><AgentWorkspaceCollections applicationId={7} slug="fixture" /></main>
  </MemoryRouter>;
}
createRoot(document.getElementById('root')!).render(<Fixture />);
