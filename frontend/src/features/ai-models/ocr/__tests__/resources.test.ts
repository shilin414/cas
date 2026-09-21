import { describe, expect, it, vi } from 'vitest'
import { checkedBytes, readBounded, loadResources } from '../resources'
import type { BrowserManifest } from '../../../../services/aiModels'
import { webcrypto } from 'node:crypto'

const bytes = new TextEncoder().encode('real bytes')
const hash = Buffer.from(await webcrypto.subtle.digest('SHA-256', bytes)).toString('hex')
const resource = {name: 'PP-OCRv6_tiny_det', url: '/ocr-assets/v1/PP-OCRv6_tiny_det.tar', sha256: hash, size_bytes: bytes.length}
describe('resource integrity before SDK sees bytes', () => {
  it('verifies length and actual hash', async () => {
    await expect(checkedBytes(bytes.buffer, resource)).resolves.toBeDefined()
    await expect(checkedBytes(bytes.buffer, {...resource,sha256:'a'.repeat(64)})).rejects.toThrow('SHA-256')
    await expect(checkedBytes(bytes.buffer, {...resource,size_bytes:1})).rejects.toThrow('大小')
  })
  it('rejects oversized streaming responses without reading unbounded body', async () => {
    await expect(readBounded(new Response(new Uint8Array(100)), 10)).rejects.toThrow('大小')
  })
  it('propagates cancellation', async () => {
    const controller = new AbortController(); controller.abort()
    const manifest = {adapter:'paddleocr_tiny',version:'v1',resources:[resource]} as BrowserManifest
    await expect(loadResources(manifest,controller.signal,()=>{})).rejects.toThrow()
  })
  it('rejects HTTP failure with a non-sensitive error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('no',{status:404})))
    const manifest = {adapter:'paddleocr_tiny',version:'v1',resources:[resource]} as BrowserManifest
    await expect(loadResources(manifest,new AbortController().signal,()=>{})).rejects.toThrow('404')
    vi.unstubAllGlobals()
  })
})

describe('verified model cache', () => {
  it('reuses verified bytes and never fetches on cache hit', async () => {
    const match=vi.fn().mockResolvedValue(new Response(bytes));const open=vi.fn().mockResolvedValue({match})
    const fetch=vi.fn();vi.stubGlobal('caches',{open});vi.stubGlobal('fetch',fetch)
    try {
      await loadResources({adapter:'paddleocr_tiny',version:'v1',resources:[resource]},new AbortController().signal,()=>{})
      expect(fetch).not.toHaveBeenCalled();expect(open.mock.calls[0][0]).toContain(hash)
    } finally {vi.unstubAllGlobals()}
  })
  it('evicts poisoned cache bytes, refetches and verifies before caching', async () => {
    const remove=vi.fn().mockResolvedValue(true);const put=vi.fn().mockResolvedValue(undefined)
    const fetch=vi.fn().mockResolvedValue(new Response(bytes))
    vi.stubGlobal('caches',{open:vi.fn().mockResolvedValue({match:vi.fn().mockResolvedValue(new Response('bad')),delete:remove,put})});vi.stubGlobal('fetch',fetch)
    try {
      await loadResources({adapter:'paddleocr_tiny',version:'v1',resources:[resource]},new AbortController().signal,()=>{})
      expect(remove).toHaveBeenCalledWith(resource.url);expect(put).toHaveBeenCalledOnce()
      expect(fetch.mock.calls[0][1]).toMatchObject({credentials:'omit',redirect:'error',mode:'same-origin',cache:'no-store'})
    } finally {vi.unstubAllGlobals()}
  })
  it('rejects hash-mismatched downloads without caching', async () => {
    const put=vi.fn();vi.stubGlobal('caches',{open:vi.fn().mockResolvedValue({match:vi.fn().mockResolvedValue(undefined),put})});vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(new Uint8Array(bytes.length))))
    try {
      await expect(loadResources({adapter:'paddleocr_tiny',version:'v1',resources:[resource]},new AbortController().signal,()=>{})).rejects.toThrow('SHA-256')
      expect(put).not.toHaveBeenCalled()
    } finally {vi.unstubAllGlobals()}
  })
  it('cache denied still permits verified in-memory execution', async () => {
    vi.stubGlobal('caches',{open:vi.fn().mockRejectedValue(new Error('denied'))});vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(bytes)))
    try {const result=await loadResources({adapter:'paddleocr_tiny',version:'v1',resources:[resource]},new AbortController().signal,()=>{});expect(result[resource.name].byteLength).toBe(bytes.length)}finally{vi.unstubAllGlobals()}
  })
})
