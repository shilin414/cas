import { describe, expect, it } from 'vitest'
import { sniffImage } from '../image'
describe('image header checks before decoding', () => {
  it('reads PNG bounds', () => { const b=new Uint8Array(24); b.set([137,80,78,71,13,10,26,10]); new DataView(b.buffer).setUint32(16,800); new DataView(b.buffer).setUint32(20,200); expect(sniffImage(b.buffer)).toEqual({mime:'image/png',width:800,height:200}) })
  it('rejects disguised SVG, corrupt images and huge declared PNG', () => {
    expect(()=>sniffImage(new TextEncoder().encode('<svg/>').buffer)).toThrow()
    const b=new Uint8Array(24); b.set([137,80,78,71,13,10,26,10]); new DataView(b.buffer).setUint32(16,100000); new DataView(b.buffer).setUint32(20,100000); expect(()=>sniffImage(b.buffer)).toThrow()
  })
})
