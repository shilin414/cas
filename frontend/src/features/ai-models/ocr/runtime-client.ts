import workerUrl from './runtime/ocr.worker.js?url'
import wasmUrl from './runtime/ort-wasm-simd-threaded.wasm?url'
import moduleUrl from './runtime/ort-wasm-simd-threaded.js?url'
import type { BrowserManifest } from '../../../services/aiModels'
import type { OcrLine } from './logic'
import { loadResources, readBounded } from './resources'
export interface OcrOutput { items: OcrLine[]; metrics: {detMs:number;recMs:number;totalMs:number}; elapsedMs:number }
export async function recognize(image: ImageData, manifest: BrowserManifest, signal: AbortSignal, progress: (message: string) => void): Promise<OcrOutput> {
  const started = performance.now()
  let worker: Worker | undefined
  signal.throwIfAborted()
  if (!isSecureContext || !crypto.subtle || typeof Worker === 'undefined') throw new Error('需要 HTTPS 或 localhost，以及 Worker、Web Crypto 和 WASM 支持')
  const resources = await loadResources(manifest, signal, progress)
  progress('加载应用内置 OCR WASM')
  const response = await fetch(wasmUrl, {signal,credentials:'omit',mode:'same-origin',redirect:'error'})
  if (!response.ok) throw new Error(`内置 WASM 加载失败：HTTP ${response.status}`)
  const wasm = await readBounded(response, 16 * 1024 * 1024)
  signal.throwIfAborted()
  return new Promise((resolve, reject) => {
    const finish = (error?: Error, result?: OcrOutput) => {
      signal.removeEventListener('abort', abort)
      clearTimeout(timer)
      worker?.terminate()
      worker = undefined
      if (error) reject(error); else resolve(result!)
    }
    const abort = () => finish(new DOMException('已取消本地 OCR', 'AbortError'))
    const timer = setTimeout(() => finish(new Error('本地 OCR 超过 120 秒，已终止；请缩小图片或选区后重试')),120_000)
    signal.addEventListener('abort',abort,{once:true})
    try {
      worker = new Worker(workerUrl,{type:'module',name:'cas-local-ocr'})
      worker.onmessage = ({data}) => {
        if (signal.aborted) return
        if (data.type === 'progress') progress(data.message)
        if (data.type === 'error') finish(new Error(data.message))
        if (data.type === 'result') finish(undefined,{items:data.items,metrics:data.metrics,elapsedMs:performance.now()-started})
      }
      worker.onerror = () => finish(new Error('OCR Worker 无法启动或运行；请检查部署资源、CSP 和浏览器 WASM 支持'))
      worker.onmessageerror = () => finish(new Error('OCR Worker 数据传递失败'))
      worker.postMessage({image,resources,wasm,moduleUrl:new URL(moduleUrl,location.href).href}, [image.data.buffer,wasm,...Object.values(resources)])
    } catch(error) { finish(error instanceof Error ? error : new Error('OCR Worker 创建失败')) }
  })
}
