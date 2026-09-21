import { displayDimensions, MAX_IMAGE_BYTES } from './logic'
export function sniffImage(buffer: ArrayBuffer): {mime:string;width:number;height:number} {
  const b = new Uint8Array(buffer), v = new DataView(buffer)
  let mime = '', width = 0, height = 0
  if (b.length >= 24 && [137,80,78,71,13,10,26,10].every((n,i)=>b[i]===n)) {
    mime='image/png'; width=v.getUint32(16); height=v.getUint32(20)
  } else if (b.length >= 4 && b[0]===255 && b[1]===216) {
    mime='image/jpeg'
    let i=2
    while(i+8 < b.length) {
      if(b[i++]!==255) break
      while(b[i]===255) i++
      const marker=b[i++]
      if(marker===0xda || marker===0xd9) break
      const length=v.getUint16(i)
      if(length<2 || i+length>b.length) break
      if([0xc0,0xc1,0xc2,0xc3,0xc5,0xc6,0xc7,0xc9,0xca,0xcb,0xcd,0xce,0xcf].includes(marker)) {height=v.getUint16(i+3);width=v.getUint16(i+5);break}
      i+=length
    }
  } else if (b.length >= 30 && String.fromCharCode(...b.slice(0,4))==='RIFF' && String.fromCharCode(...b.slice(8,12))==='WEBP') {
    mime='image/webp'
    const kind=String.fromCharCode(...b.slice(12,16))
    if(kind==='VP8X') {
      if(b[20]&2) throw new Error('不支持动态 WebP，请上传静态图片')
      width=1+b[24]+(b[25]<<8)+(b[26]<<16);height=1+b[27]+(b[28]<<8)+(b[29]<<16)
    } else if(kind==='VP8 ' && b[23]===0x9d && b[24]===1 && b[25]===0x2a) {width=v.getUint16(26,true)&0x3fff;height=v.getUint16(28,true)&0x3fff}
    else if(kind==='VP8L' && b[20]===0x2f) {width=1+b[21]+((b[22]&0x3f)<<8);height=1+(b[22]>>6)+(b[23]<<2)+((b[24]&0xf)<<10)}
  }
  if (!mime || !width || !height) throw new Error('只支持有效的静态 JPEG、PNG、WebP 图片')
  displayDimensions(width,height,0)
  return {mime,width,height}
}
export async function decodeFile(file: File): Promise<ImageBitmap> {
  if (file.size > MAX_IMAGE_BYTES || !file.size) throw new Error('图片不能为空且不能超过 15 MiB')
  const bytes=await file.arrayBuffer()
  const {mime}=sniffImage(bytes)
  if(file.type && file.type!==mime) throw new Error('文件类型与图片内容不符')
  // Browser applies EXIF orientation. Manual 90-degree rotation remains available.
  const bitmap=await createImageBitmap(new Blob([bytes],{type:mime}),{imageOrientation:'from-image'})
  try {displayDimensions(bitmap.width,bitmap.height,0); return bitmap} catch(error) {bitmap.close();throw error}
}
export function renderImage(bitmap: ImageBitmap, rotation: number): HTMLCanvasElement {
  const {width,height}=displayDimensions(bitmap.width,bitmap.height,rotation)
  const canvas=document.createElement('canvas');canvas.width=width;canvas.height=height
  const context=canvas.getContext('2d')
  if(!context) throw new Error('浏览器无法创建图像画布')
  context.fillStyle='#fff';context.fillRect(0,0,width,height)
  context.translate(width/2,height/2);context.rotate(rotation*Math.PI/180)
  const swapped=rotation%180!==0
  context.drawImage(bitmap,-(swapped?height:width)/2,-(swapped?width:height)/2,swapped?height:width,swapped?width:height)
  return canvas
}
