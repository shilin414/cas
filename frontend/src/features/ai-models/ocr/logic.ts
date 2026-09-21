import type { BrowserManifest } from '../../../services/aiModels'

export const MODEL_NAMES = ['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_rec'] as const
export const MAX_RESOURCE_BYTES = 64 * 1024 * 1024
export const MAX_IMAGE_BYTES = 15 * 1024 * 1024
export const MAX_IMAGE_PIXELS = 24_000_000
export interface Crop { x: number; y: number; width: number; height: number }
export interface OcrLine { text: string; score: number; poly: [number, number][] }
// Never remove vertical whitespace or convert the code to a number.
export const normalizeCode = (text: string) => text.replace(/[ \t\u00a0\u3000]/g, '')
export const isCode20 = (text: string) => /^[0-9]{20}$/.test(normalizeCode(text))
export function candidatesFromLines(lines: string[]): string[] {
  const candidates = new Set<string>()
  for (const line of lines) for (const physicalLine of line.split(/[\r\n\u2028\u2029]/)) {
    for (const match of normalizeCode(physicalLine).matchAll(/(?:^|[^\p{L}\p{N}])([0-9]{20})(?![\p{L}\p{N}])/gu)) candidates.add(match[1])
  }
  return [...candidates]
}
export function validateManifest(value: unknown, origin: string, base: string): BrowserManifest {
  const m = value as BrowserManifest | null
  if (!m || m.adapter !== 'paddleocr_tiny' || typeof m.version !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(m.version)) throw new Error('OCR manifest 的适配器或版本不受支持')
  if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) throw new Error('部署基础路径不合法')
  if (!Array.isArray(m.resources) || m.resources.length !== 2) throw new Error('需要且仅允许 tiny det、rec 两个模型资源')
  const seen = new Set<string>()
  for (const r of m.resources) {
    if (!r || !MODEL_NAMES.includes(r.name as typeof MODEL_NAMES[number]) || seen.has(r.name)) throw new Error('模型资源名称未知或重复')
    seen.add(r.name)
    if (!Number.isSafeInteger(r.size_bytes) || r.size_bytes <= 0 || r.size_bytes > MAX_RESOURCE_BYTES || typeof r.sha256 !== 'string' || !/^[a-f0-9]{64}$/.test(r.sha256)) throw new Error('资源大小或 SHA-256 无效')
    const portable = `ocr-assets/${m.version}/${r.name}.tar`
    const path = `${base}${portable}`
    // Portable DB URLs are expanded under the active deployment base; external URLs remain forbidden.
    if (typeof r.url !== 'string' || (r.url !== portable && r.url !== path && r.url !== `${origin}${path}`)) throw new Error('OCR 只允许内置、同源、不可变版本的模型路径')
    const url = new URL(r.url === portable ? path : r.url, origin)
    if (url.origin !== origin || url.pathname !== path || url.search || url.hash || url.username || url.password) throw new Error('OCR 资源地址不安全')
  }
  return { adapter: 'paddleocr_tiny', version: m.version, resources: m.resources.map(r => ({ ...r, url: r.url.startsWith('ocr-assets/') ? `${base}${r.url}` : r.url })) }
}
export function displayDimensions(width: number, height: number, rotation: number) {
  if (![width, height].every(n => Number.isFinite(n) && n > 0) || width * height > MAX_IMAGE_PIXELS || Math.max(width, height) > 16384) throw new Error('图片尺寸过大：最多 2400 万像素，单边不超过 16384')
  const [w, h] = rotation % 180 === 0 ? [width, height] : [height, width]
  const scale = Math.min(1, 2048 / Math.max(w, h))
  return { width: Math.max(1, Math.round(w * scale)), height: Math.max(1, Math.round(h * scale)) }
}
export function clampCrop(crop: Crop, width: number, height: number): Crop {
  if (!Object.values(crop).every(Number.isFinite)) throw new Error('裁剪选区无效')
  const x = Math.max(0, Math.min(width - 1, Math.round(crop.x)))
  const y = Math.max(0, Math.min(height - 1, Math.round(crop.y)))
  const w = Math.min(width - x, Math.round(crop.width)), h = Math.min(height - y, Math.round(crop.height))
  if (w < 1 || h < 1) throw new Error('请选择非空裁剪区域')
  return { x, y, width: w, height: h }
}
export class LatestRun {
  private generation = 0
  next() { return ++this.generation }
  isCurrent(id: number) { return id === this.generation }
}
