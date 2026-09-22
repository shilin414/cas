// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
const mocks = vi.hoisted(() => ({ tasks:vi.fn(), schedules:vi.fn(), artifacts:vi.fn(), enable:vi.fn(), disable:vi.fn(), navigate:vi.fn() }));
vi.mock('@/services/taskApi',()=>({fetchTasks:mocks.tasks}));
vi.mock('@/services/scheduleApi',()=>({fetchSchedules:mocks.schedules,enableSchedule:mocks.enable,disableSchedule:mocks.disable}));
vi.mock('@/services/workspaceArtifactApi',()=>({fetchWorkspaceArtifacts:mocks.artifacts}));
vi.mock('react-router-dom',()=>({useNavigate:()=>mocks.navigate}));
vi.mock('@/components/Schedules/ScheduleEditorModal',()=>({ScheduleEditorModal:({presetApplicationId}: {presetApplicationId:number})=><div>编辑器智能体 {presetApplicationId}</div>}));
import AgentWorkspaceCollections from '../home/AgentWorkspaceCollections';
import {fetchAgentCollection} from '../home/agentWorkspaceData';
(globalThis as unknown as {IS_REACT_ACT_ENVIRONMENT:boolean}).IS_REACT_ACT_ENVIRONMENT=true;
let root:Root; let host:HTMLDivElement;
const settle=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,30));});
const click=async(label:string)=>{const button=Array.from(host.querySelectorAll('button')).find(b=>b.textContent?.replace(/\s/g,'')===label.replace(/\s/g,'')); expect(button).toBeTruthy(); await act(async()=>button!.click()); await settle();};
beforeEach(()=>{vi.clearAllMocks(); mocks.tasks.mockResolvedValue({items:[],nextCursor:''}); mocks.artifacts.mockResolvedValue({items:[],next_cursor:''}); mocks.schedules.mockResolvedValue([]); host=document.createElement('div'); document.body.append(host); root=createRoot(host);});
afterEach(async()=>{await act(async()=>root.unmount());host.remove();});
const mount=async(id=7)=>{await act(async()=>root.render(<AgentWorkspaceCollections key={id} applicationId={id} slug="agent-seven"/>));await settle();};
const task={id:'88',title:'销售日报',preview:'摘要',updatedAt:'2026-09-20T12:00:00Z',executionState:'idle'};

describe('agent workspace data and interactions',()=>{
 it('scopes tasks on the server and preserves cursor and query',async()=>{await fetchAgentCollection(7,'tasks','收入','cursor'); expect(mocks.tasks).toHaveBeenCalledWith({applicationId:7,q:'收入',cursor:'cursor',limit:20});});
 it('scopes automations before pagination and uses the last DISPLAYED record for the next page',async()=>{mocks.schedules.mockResolvedValue(Array.from({length:21},(_,i)=>({id:100-i,application_id:7}))); const page=await fetchAgentCollection(7,'automations','日报','120'); expect(mocks.schedules).toHaveBeenCalledWith('all',120,21,'日报',7);expect(page.rows).toHaveLength(20);expect(page.next).toBe('81');});
 it('uses generated artifact API, not task uploads',async()=>{await fetchAgentCollection(7,'resources','pdf','next');expect(mocks.artifacts).toHaveBeenCalledWith(7,'pdf','next');expect(mocks.tasks).not.toHaveBeenCalled();});
 it('resumes a task in its owning agent conversation',async()=>{mocks.tasks.mockResolvedValue({items:[task],nextCursor:''});await mount();await click('销售日报摘要');expect(mocks.navigate).toHaveBeenCalledWith('/chat/agent-seven?conversation=88');});
 it('shows failures honestly rather than a fake empty state and supports retry',async()=>{mocks.artifacts.mockRejectedValueOnce(new Error('offline'));await mount();await click('资源');expect(host.textContent).toContain('内容加载失败');expect(host.textContent).not.toContain('还没有生成');await click('重试');expect(host.textContent).toContain('还没有生成的资源');});
 it('discards a late task result after changing tabs',async()=>{let resolve!:(v:unknown)=>void;mocks.tasks.mockReturnValue(new Promise(r=>{resolve=r;}));await mount();await click('资源');await act(async()=>resolve({items:[task],nextCursor:''}));expect(host.textContent).not.toContain('销售日报');expect(host.textContent).toContain('还没有生成的资源');});
 it('discards old agent data when selection changes',async()=>{let resolve!:(v:unknown)=>void;mocks.tasks.mockReturnValueOnce(new Promise(r=>{resolve=r;}));await mount(7);await mount(8);await act(async()=>resolve({items:[task],nextCursor:''}));expect(host.textContent).not.toContain('销售日报');expect(mocks.tasks).toHaveBeenLastCalledWith(expect.objectContaining({applicationId:8}));});
 it('retains the first page after load-more failure',async()=>{mocks.tasks.mockResolvedValueOnce({items:[task],nextCursor:'next'}).mockRejectedValueOnce(new Error('offline'));await mount();await click('加载更多');expect(host.textContent).toContain('销售日报');expect(host.textContent).toContain('加载更多失败');});
 it('opens a generated artifact through authenticated resolver and links its source task',async()=>{mocks.artifacts.mockResolvedValue({items:[{id:'uuid',name:'日报.pdf',normalized_type:'file',created_at:'2026-09-20T12:00:00Z',conversation_id:'88',task_title:'销售日报'}],next_cursor:''});await mount();await click('资源');expect(host.querySelector('a')?.getAttribute('href')).toMatch(/\/v2\/artifacts\/uuid\/open$/);await click('来自：销售日报');expect(mocks.navigate).toHaveBeenCalledWith('/chat/agent-seven?conversation=88');});
 it('presets new automation to the selected agent',async()=>{await mount();await click('自动化');const add=host.querySelector('[aria-label="新建自动化"]') as HTMLButtonElement;await act(async()=>add.click());expect(host.textContent).toContain('编辑器智能体 7');});
 it('does not flip enabled state when the server rejects a toggle',async()=>{mocks.schedules.mockResolvedValue([{id:5,name:'日报',application_id:7,enabled:false,schedule_type:'daily',trigger:{time:'15:00'},timezone:'Asia/Shanghai'}]);mocks.enable.mockRejectedValue(new Error('denied'));await mount();await click('自动化');await act(async()=>(host.querySelector('[role="switch"]') as HTMLButtonElement).click());expect(mocks.enable).toHaveBeenCalledWith(5);expect(host.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('false');expect(host.textContent).toContain('未改变原状态');});
});
