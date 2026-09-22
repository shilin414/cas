// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
vi.mock('@/shell/useIsMobile',()=>({useIsMobile:()=>false}));
vi.mock('@/stores/useAuthStore',()=>({useAuthStore:()=>({user:{id:1,username:'test'}})}));
import RunChatPanel from '../RunChatPanel';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import type { ComposerApplication } from '@/services/runApi';
(globalThis as unknown as {IS_REACT_ACT_ENVIRONMENT:boolean}).IS_REACT_ACT_ENVIRONMENT=true;
const app={id:7,slug:'agent-seven',name:'问数小安',kind:'chat',description:'查询数据',icon:'',color:'',capabilities:{attachment:true}} as ComposerApplication;
let host:HTMLDivElement;let root:Root;
const send=vi.fn();const created=vi.fn();
beforeEach(()=>{
 vi.clearAllMocks();send.mockResolvedValue(88);
 useRunChatStore.setState({conversations:{},activeConversationId:null,isLoading:false,error:null,sendMessage:send,loadConversation:vi.fn().mockResolvedValue(undefined)});
 useWorkspaceStore.setState({workspaces:{}});
 host=document.createElement('div');document.body.append(host);root=createRoot(host);
});
afterEach(async()=>{await act(async()=>root.unmount());host.remove();});
async function mount(draft='',conversationId:number|null=null){await act(async()=>root.render(<MemoryRouter><RunChatPanel applicationId={7} application={app} homeWorkspace conversationId={conversationId} draftText={draft} onConversationCreated={created} emptyState={<div>智能体标题</div>} afterComposer={<section>任务自动化资源</section>} /></MemoryRouter>));}
it('keeps the composer between identity and scoped collections, with an empty-send guard',async()=>{
 await mount();expect(host.querySelector('.agent-home-surface')).toBeTruthy();
 const sections=Array.from(host.querySelector('.agent-home-surface')!.children);
 expect(sections[0].textContent).toContain('智能体标题');expect(sections[1].querySelector('textarea')).toBeTruthy();expect(sections[2].textContent).toContain('任务自动化资源');
 expect((host.querySelector('[aria-label="发送任务"]') as HTMLButtonElement).disabled).toBe(true);
 expect(host.querySelector('[aria-label="添加附件"]')).toBeTruthy();expect(send).not.toHaveBeenCalled();
});
it('sends to the selected application via the original run path and hands off the new conversation',async()=>{
 await mount('分析销量');await act(async()=>(host.querySelector('[aria-label="发送任务"]') as HTMLButtonElement).click());
 expect(send).toHaveBeenCalledWith({applicationId:7,conversationId:null,content:'分析销量',attachments:[]});expect(created).toHaveBeenCalledWith(88);
});
it('does not insert the homepage layout into an existing conversation',async()=>{await mount('',42);expect(host.querySelector('.agent-home-surface')).toBeNull();expect(host.textContent).not.toContain('任务自动化资源');});
