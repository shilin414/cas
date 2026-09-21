import { describe, expect, it } from 'vitest'
import { candidatesFromLines, isCode20, normalizeCode, validateManifest, clampCrop, displayDimensions, LatestRun } from '../logic'

const origin = 'https://studio.example'
const manifest = () => ({ adapter: 'paddleocr_tiny', version: 'v6-tiny-1', resources: ['PP-OCRv6_tiny_det', 'PP-OCRv6_tiny_rec'].map(name => ({ name, url: `/studio/ocr-assets/v6-tiny-1/${name}.tar`, sha256: 'a'.repeat(64), size_bytes: 10240 })) })
describe('20 digit candidates — conservative, never repair or splice', () => {
  it('keeps leading zeroes and normalizes horizontal grouping only', () => {
    expect(normalizeCode('0000 1234\t5678\u00a09012\u30003456')).toBe('00001234567890123456')
    expect(isCode20('0000 1234 5678 9012 3456')).toBe(true)
  })
  it.each(['123456789012345678901', '1234567890123456789', 'O0012345678901234567', '００００１２３４５６７８９０１２３４５６', '0000123456\n7890123456', '0000123456\r7890123456'])('rejects without guessing: %s', value => expect(isCode20(value)).toBe(false))
  it('extracts independent same-line candidates, not adjoining letter/digit substrings', () => {
    expect(candidatesFromLines(['编号：0000 1234 5678 9012 3456', '0000123456', '7890123456', '123456789012345678901', 'O00012345678901234567', '00001234567890123456'])).toEqual(['00001234567890123456'])
    expect(candidatesFromLines(['0000123456\n7890123456'])).toEqual([])
    expect(candidatesFromLines(['１00001234567890123456', '00001234567890123456９'])).toEqual([])
  })
})
describe('trusted manifest', () => {
  it('accepts only exact model resources under the app base path and version', () => expect(validateManifest(manifest(), origin, '/studio/').resources).toHaveLength(2))
  it('expands portable built-in URLs under the active deployment base', () => { const m = manifest(); for (const r of m.resources) r.url = r.url.replace('/studio/', ''); expect(validateManifest(m, origin, '/studio/').resources[0].url).toBe('/studio/ocr-assets/v6-tiny-1/PP-OCRv6_tiny_det.tar') })
  it.each(['https://evil.example/a.tar', '//evil.example/a.tar', '/studio/api/a.tar', '/studio/ocr-assets/v6-tiny-1/../evil.tar', '/studio/ocr-assets/v6-tiny-1/%2e%2e/evil.tar', '/studio/ocr-assets/v6-tiny-1/PP-OCRv6_tiny_det.tar?x=1', 'javascript:alert(1)'])('rejects arbitrary paths: %s', url => {
    const m = manifest(); m.resources[0].url = url; expect(() => validateManifest(m, origin, '/studio/')).toThrow()
  })
  it('rejects duplicate, missing, oversized, wrong digest, adapter, version', () => {
    for (const mutate of [(m: any) => m.resources.pop(), (m: any) => m.resources[1] = m.resources[0], (m: any) => m.resources[0].size_bytes = 1e10, (m: any) => m.resources[0].sha256 = 'no', (m: any) => m.adapter = 'custom-js', (m: any) => m.version = '../v1']) {
      const m = manifest(); mutate(m); expect(() => validateManifest(m, origin, '/studio/')).toThrow()
    }
  })
})
describe('image bounds and cancellation generation', () => {
  it('downsizes and handles rotated aspect ratio', () => { expect(displayDimensions(4000, 2000, 90)).toEqual({ width: 1024, height: 2048 }); expect(() => displayDimensions(100000, 2000, 0)).toThrow() })
  it('clamps crop to image and rejects empty selection', () => { expect(clampCrop({x: -5,y: 10,width: 500,height: 40},100,100)).toEqual({x:0,y:10,width:100,height:40}); expect(() => clampCrop({x:0,y:0,width:0,height:1},100,100)).toThrow() })
  it('late results cannot replace a newer run', () => { const run = new LatestRun(); const old = run.next(); expect(run.isCurrent(old)).toBe(true); run.next(); expect(run.isCurrent(old)).toBe(false) })
})
