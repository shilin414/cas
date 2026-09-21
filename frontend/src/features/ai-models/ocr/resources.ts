import type { BrowserManifest, BrowserResource } from '../../../services/aiModels'
const CACHE_PREFIX = 'cas-ocr-verified-v1-'
export async function checkedBytes(buffer: ArrayBuffer, resource: BrowserResource): Promise<ArrayBuffer> {
  if (buffer.byteLength !== resource.size_bytes) throw new Error(`${resource.name} 资源大小校验失败`)
  const digest = await crypto.subtle.digest('SHA-256', buffer)
  const hash = [...new Uint8Array(digest)].map(n => n.toString(16).padStart(2, '0')).join('')
  if (hash !== resource.sha256) throw new Error(`${resource.name} SHA-256 校验失败；拒绝运行`)
  return buffer
}
export async function readBounded(response: Response, limit: number): Promise<ArrayBuffer> {
  const reader = response.body?.getReader()
  if (!reader) throw new Error('浏览器不支持流式资源加载')
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    for (let read = await reader.read(); !read.done; read = await reader.read()) {
      const value = read.value
      size += value.byteLength
      if (size > limit) throw new Error('资源大小超过 manifest 声明值')
      chunks.push(value)
    }
  } catch (error) { await reader.cancel(); throw error }
  finally { reader.releaseLock() }
  const result = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength }
  return result.buffer
}
export async function loadResources(manifest: BrowserManifest, signal: AbortSignal, progress: (message: string) => void): Promise<Record<string, ArrayBuffer>> {
  const result: Record<string, ArrayBuffer> = {}
  // Version + all content digests isolate upgrades even if an admin reuses a version label.
  const identity = manifest.resources.map(r => `${r.name}-${r.sha256}`).sort().join('-')
  let cache: Cache | undefined
  try { if (typeof caches !== 'undefined') cache = await caches.open(`${CACHE_PREFIX}${manifest.version}-${identity}`) } catch { progress('缓存不可用，改用内存校验（不会持久化图片）') }
  for (const resource of manifest.resources) {
    signal.throwIfAborted()
    let buffer: ArrayBuffer | undefined
    if (cache) {
      try {
        const stored = await cache.match(resource.url)
        if (stored) buffer = await checkedBytes(await readBounded(stored, resource.size_bytes), resource)
      } catch { await cache.delete(resource.url).catch(() => false) }
    }
    if (!buffer) {
      progress(`加载并校验 ${resource.name} (${(resource.size_bytes / 1048576).toFixed(1)} MiB)`)
      const response = await fetch(resource.url, {signal, credentials:'omit', redirect:'error', mode:'same-origin', cache:'no-store', referrerPolicy:'no-referrer'})
      if (!response.ok) throw new Error(`${resource.name} 下载失败：HTTP ${response.status}`)
      buffer = await checkedBytes(await readBounded(response, resource.size_bytes), resource)
      signal.throwIfAborted()
      // Only verified PUBLIC MODEL bytes, never user images or codes.
      if (cache) await cache.put(resource.url, new Response(buffer.slice(0), {headers:{'Content-Type':'application/octet-stream'}})).catch(() => progress('缓存写入失败，本次仍可识别'))
    } else progress(`${resource.name} 缓存已重新校验`)
    signal.throwIfAborted()
    result[resource.name] = buffer
  }
  return result
}
export async function clearModelCache(): Promise<void> {
  if (typeof caches === 'undefined') return
  for (const key of await caches.keys()) if (key.startsWith(CACHE_PREFIX)) await caches.delete(key)
}
