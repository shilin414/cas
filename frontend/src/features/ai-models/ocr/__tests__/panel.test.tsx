// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OcrTestPanel from '../OcrTestPanel'
import type { AIModel } from '../../../../services/aiModels'
;(globalThis as unknown as {IS_REACT_ACT_ENVIRONMENT:boolean}).IS_REACT_ACT_ENVIRONMENT=true
const model: AIModel={id:'test',model_id:'PP-OCRv6_tiny',name:'test',capability_kind:'ocr',execution_location:'browser_local',enabled:true,version:1,created_at:'',updated_at:'',capabilities:{image:true,video:false,pdf:false,streaming:false},default_parameters:{},browser_manifest:{adapter:'paddleocr_tiny',version:'v1',resources:['PP-OCRv6_tiny_det','PP-OCRv6_tiny_rec'].map(name=>({name,url:`/ocr-assets/v1/${name}.tar`,sha256:'a'.repeat(64),size_bytes:100}))}}
let root:Root
let host:HTMLDivElement
async function render(m=model){host=document.createElement('div');document.body.append(host);root=createRoot(host);await act(async()=>{root.render(<OcrTestPanel model={m}/>)})}
afterEach(async()=>{await act(async()=>root?.unmount());document.body.innerHTML='';vi.unstubAllGlobals()})
describe('OCR panel privacy and manual entry',()=>{
  it('mounting a valid panel makes no fetch and constructs no worker',async()=>{
    const fetch=vi.fn(),worker=vi.fn();vi.stubGlobal('fetch',fetch);vi.stubGlobal('Worker',worker)
    await render();expect(fetch).not.toHaveBeenCalled();expect(worker).not.toHaveBeenCalled()
    expect((host.querySelector('[data-testid=ocr-start]') as HTMLButtonElement).disabled).toBe(true)
  })
  it('rejects multiline paste before native text input silently joins it',async()=>{
    await render();const event=new Event('paste',{bubbles:true,cancelable:true})
    Object.defineProperty(event,'clipboardData',{value:{getData:()=> '0123456789\n0123456789'}})
    await act(async()=>host.querySelector('[data-testid=ocr-manual-code]')!.dispatchEvent(event))
    expect(event.defaultPrevented).toBe(true);expect(host.textContent).toContain('不允许跨行粘贴')
  })
  it('does not silently replace unknown models with tiny',async()=>{
    await render({...model,model_id:'another-model'});expect(host.textContent).toContain('不会自动替换模型')
    expect((host.querySelector('[data-testid=ocr-start]') as HTMLButtonElement).disabled).toBe(true)
  })
})
