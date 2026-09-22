// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
const mocks=vi.hoisted(()=>({ensure:vi.fn(), load:vi.fn().mockResolvedValue(undefined)}));
const defaultAgent={id:1,slug:'default',name:'默认智能体',kind:'chat'};
vi.mock('@/stores/useWorkspaceBootstrapStore',()=>({useWorkspaceBootstrapStore:(selector:(s:unknown)=>unknown)=>selector({isLoading:false,load:mocks.load,defaultApplication:defaultAgent})}));
vi.mock('@/stores/useApplicationEntityStore',()=>({useApplicationEntityStore:(selector:(s:unknown)=>unknown)=>selector({ensure:mocks.ensure})}));
vi.mock('@/shell/useIsMobile',()=>({useIsMobile:()=>false}));
vi.mock('@/workbench/home/WorkbenchHome',()=>({default:()=>null}));
vi.mock('@/workbench/home/AgentWorkspaceCollections',()=>({default:({applicationId}:{applicationId:number})=><div>列表智能体 {applicationId}</div>}));
vi.mock('@/components/Chat',()=>({RunChatPanel:({application,conversationId,afterComposer,onConversationCreated}:{application?:{name:string};conversationId:number|null;afterComposer:React.ReactNode;onConversationCreated:(id:number)=>void})=><div data-testid="composer" data-conversation={String(conversationId)}>{application?.name}{afterComposer}<button onClick={()=>onConversationCreated(901)}>模拟新任务创建</button></div>}));
import HomeWorkspace from '../HomeWorkspace';
import { useRunChatStore } from '@/stores/useRunChatStore';
(globalThis as {IS_REACT_ACT_ENVIRONMENT?:boolean}).IS_REACT_ACT_ENVIRONMENT=true;
let host:HTMLDivElement;let root:Root;
const agent={id:7,slug:'it-agent',name:'IT小安',kind:'chat',description:'IT支持'};
function Probe(){const location=useLocation();const navigate=useNavigate();return <><output>{location.pathname}{location.search}</output><button onClick={()=>navigate('/',{state:{newTaskApplicationId:8}})}>转到另一个智能体的新任务</button><button onClick={()=>navigate('/',{state:null})}>普通首页</button></>;}
async function mount(state:unknown=null){await act(async()=>root.render(<MemoryRouter initialEntries={[{pathname:'/',state}]}><Probe/><HomeWorkspace/></MemoryRouter>));}
beforeEach(()=>{vi.clearAllMocks();mocks.ensure.mockResolvedValue(agent);useRunChatStore.setState({activeConversationId:null});host=document.createElement('div');document.body.append(host);root=createRoot(host);});
afterEach(async()=>{await act(async()=>root.unmount());host.remove();});
it('keeps ordinary home using the configured default',async()=>{await mount();expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('默认智能体');expect(mocks.ensure).not.toHaveBeenCalled();});
it('uses the originating agent for the composer and all collections without restoring its conversation',async()=>{useRunChatStore.setState({activeConversationId:123});await mount({newTaskApplicationId:7});expect(mocks.ensure).toHaveBeenCalledWith(7,expect.anything());expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('IT小安');expect(host.textContent).toContain('列表智能体 7');expect(host.querySelector('[data-testid="composer"]')?.getAttribute('data-conversation')).toBe('null');expect(useRunChatStore.getState().activeConversationId).toBeNull();});
it('does not briefly expose a default-agent composer while resolving the intended agent',async()=>{let resolve!:(value:unknown)=>void;mocks.ensure.mockReturnValue(new Promise(r=>{resolve=r;}));await mount({newTaskApplicationId:7});expect(host.querySelector('[data-testid="composer"]')).toBeNull();await act(async()=>resolve(agent));expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('IT小安');});
it('surfaces handoff failures instead of silently sending through the default agent',async()=>{mocks.ensure.mockRejectedValue(new Error('offline'));await mount({newTaskApplicationId:7});expect(host.querySelector('[data-testid="composer"]')).toBeNull();expect(host.textContent).toContain('智能体加载失败');});
it('does not render a send surface for an unavailable originating agent',async()=>{mocks.ensure.mockResolvedValue(undefined);await mount({newTaskApplicationId:7});expect(host.querySelector('[data-testid="composer"]')).toBeNull();expect(host.textContent).toContain('智能体暂不可用');});
it('routes the first newly created task to the carried agent',async()=>{await mount({newTaskApplicationId:7});await act(async()=>(Array.from(host.querySelectorAll('button')).find(b=>b.textContent==='模拟新任务创建')!).click());expect(host.querySelector('output')?.textContent).toBe('/chat/it-agent?conversation=901');});
it('discards an old handoff resolution after a second navigation',async()=>{let resolve!:(value:unknown)=>void;mocks.ensure.mockReturnValueOnce(new Promise(r=>{resolve=r;})).mockResolvedValueOnce({...agent,id:8,name:'另一个智能体'});await mount({newTaskApplicationId:7});await act(async()=>(Array.from(host.querySelectorAll('button')).find(b=>b.textContent==='转到另一个智能体的新任务')!).click());await act(async()=>resolve(agent));expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('另一个智能体');expect(host.querySelector('[data-testid="composer"]')?.textContent).not.toContain('IT小安');});
it('ignores malformed navigation state',async()=>{await mount({newTaskApplicationId:'7'});expect(mocks.ensure).not.toHaveBeenCalled();expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('默认智能体');});

it('returns to the configured default when explicitly navigating to ordinary home',async()=>{
 await mount({newTaskApplicationId:7});
 await act(async()=>(Array.from(host.querySelectorAll('button')).find(b=>b.textContent==='普通首页')!).click());
 expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('默认智能体');
 expect(host.querySelector('[data-testid="composer"]')?.textContent).not.toContain('IT小安');
});
it('retries an unavailable handoff without changing the intended agent',async()=>{
 mocks.ensure.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(agent);
 await mount({newTaskApplicationId:7});
 await act(async()=>(Array.from(host.querySelectorAll('button')).find(b=>b.textContent?.replace(/\s/g,'')==='重新加载')!).click());
 expect(host.querySelector('[data-testid="composer"]')?.textContent).toContain('IT小安');
 expect(mocks.ensure).toHaveBeenLastCalledWith(7,{maxAgeMs:0,bypassBackoff:true});
});
