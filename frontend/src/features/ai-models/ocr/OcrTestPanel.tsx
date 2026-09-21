import { useEffect, useRef, useState } from 'react'
import type { AIModel } from '../../../services/aiModels'
import { candidatesFromLines, clampCrop, isCode20, LatestRun, normalizeCode, validateManifest } from './logic'
import type { Crop, OcrLine } from './logic'
import { decodeFile, renderImage } from './image'
import { clearModelCache } from './resources'
import type { OcrOutput } from './runtime-client'
import './ocr.css'

export default function OcrTestPanel({model}: {model: AIModel}) {
  const generation=useRef(new LatestRun())
  const abort=useRef<AbortController>()
  const source=useRef<ImageBitmap>()
  const prepared=useRef<HTMLCanvasElement>()
  const preview=useRef<HTMLCanvasElement>(null)
  const drag=useRef<{x:number;y:number}>()
  const upload=useRef<HTMLInputElement>(null)
  const capture=useRef<HTMLInputElement>(null)
  const [dimensions,setDimensions]=useState<{width:number;height:number}>()
  const [rotation,setRotation]=useState(0)
  const [crop,setCrop]=useState<Crop>()
  const [busy,setBusy]=useState(false)
  const [decoding,setDecoding]=useState(false)
  const [status,setStatus]=useState('选择图片后点击“开始识别”，才会加载 OCR 引擎与模型。')
  const [error,setError]=useState('')
  const [lines,setLines]=useState<OcrLine[]>([])
  const [result,setResult]=useState<OcrOutput>()
  const [manual,setManual]=useState('')
  const [confirmed,setConfirmed]=useState('')
  const modelKey=JSON.stringify([model.id,model.model_id,model.enabled,model.version,model.capability_kind,model.execution_location,model.browser_manifest])
  const modelKeyRef=useRef(modelKey)
  modelKeyRef.current=modelKey
  let manifestError=''
  try {
    if(model.model_id!=='PP-OCRv6_tiny' || model.capability_kind!=='ocr' || model.execution_location!=='browser_local') throw new Error('此面板仅支持已验证适配路径 PP-OCRv6_tiny / browser_local，不会自动替换模型')
    if(!model.enabled) throw new Error('此模型已停用')
    validateManifest(model.browser_manifest,window.location.origin,import.meta.env.BASE_URL)
  } catch(e) {manifestError=e instanceof Error?e.message:'OCR manifest 无效'}
  const invalidate=() => {generation.current.next();abort.current?.abort();abort.current=undefined}
  const clearResult=() => {setLines([]);setResult(undefined);setManual('');setConfirmed('');setError('')}
  const releaseImage=() => {
    source.current?.close();source.current=undefined
    if(prepared.current) {prepared.current.width=0;prepared.current.height=0;prepared.current=undefined}
    if(preview.current) {preview.current.width=0;preview.current.height=0}
  }
  useEffect(()=>{
    invalidate();releaseImage();setDimensions(undefined);setCrop(undefined);setBusy(false);setDecoding(false);setRotation(0);clearResult()
    setStatus('选择图片后点击“开始识别”，才会加载 OCR 引擎与模型。')
    return ()=>{invalidate();releaseImage()}
    // A model revision is a new privacy/lifecycle boundary, not just a visual prop change.
  },[modelKey])
  useEffect(()=>{
    if(!dimensions || !preview.current || !prepared.current) return
    const canvas=preview.current; canvas.width=dimensions.width;canvas.height=dimensions.height
    canvas.getContext('2d')?.drawImage(prepared.current,0,0)
  },[dimensions])
  const applyImage=(bitmap: ImageBitmap,angle: number)=>{
    const canvas=renderImage(bitmap,angle)
    if(prepared.current) {prepared.current.width=0;prepared.current.height=0}
    prepared.current=canvas;setDimensions({width:canvas.width,height:canvas.height});setCrop({x:0,y:0,width:canvas.width,height:canvas.height});setRotation(angle);clearResult()
  }
  const selectFile=async(file?:File)=>{
    if(!file) return
    invalidate();const id=generation.current.next();setBusy(false);setDecoding(true);clearResult();releaseImage();setDimensions(undefined);setCrop(undefined)
    setStatus('正在本机解码并校正图片方向')
    let bitmap:ImageBitmap|undefined
    try {
      bitmap=await decodeFile(file)
      if(!generation.current.isCurrent(id)) {bitmap.close();return}
      source.current=bitmap;applyImage(bitmap,0);setStatus('图片已在本机准备好；可旋转或拖动选择裁剪区域。')
    } catch(e) {bitmap?.close();if(generation.current.isCurrent(id)) setError(e instanceof Error?e.message:'图片解码失败')}
    finally {if(generation.current.isCurrent(id)) setDecoding(false)}
  }
  const cancel=()=>{invalidate();setBusy(false);setDecoding(false);clearResult();releaseImage();setDimensions(undefined);setCrop(undefined);setStatus('已取消并释放 Worker 和图片；请重新选择图片。')}
  const start=async()=>{
    if(!prepared.current || !crop || busy || decoding || manifestError) return
    invalidate();const id=generation.current.next();const key=modelKey;const controller=new AbortController();abort.current=controller
    setBusy(true);clearResult();setStatus('准备选区与本地资源')
    const current=()=>generation.current.isCurrent(id)&&modelKeyRef.current===key&&!controller.signal.aborted
    try {
      const manifest=validateManifest(model.browser_manifest,location.origin,import.meta.env.BASE_URL)
      const selected=clampCrop(crop,prepared.current.width,prepared.current.height)
      const image=prepared.current.getContext('2d')!.getImageData(selected.x,selected.y,selected.width,selected.height)
      // Crucial second lazy boundary: opening the panel/choosing a file never imports runtime assets.
      const {recognize}=await import('./runtime-client')
      if(!current()) return
      const output=await recognize(image,manifest,controller.signal,message=>{if(current()) setStatus(message)})
      if(!current()) return
      setResult(output);setLines(output.items.map(item=>({...item,poly:item.poly.map(([x,y])=>[x+selected.x,y+selected.y])})))
      setStatus(output.items.length?'识别完成；请对照原图，手动选择、修正并确认。':'识别完成，但未检测到文字；请调整选区、方向或清晰度。')
    } catch(e) {if(current()) {setError(e instanceof Error?e.message:'本地 OCR 失败');setStatus('识别失败；没有替换模型或生成模拟结果。')}}
    finally {if(current()) {abort.current=undefined;setBusy(false)}}
  }
  const candidates=candidatesFromLines(lines.map(l=>l.text))
  const changeCrop=(value:Crop)=>{if(!dimensions)return;try{setCrop(clampCrop(value,dimensions.width,dimensions.height));clearResult()}catch{ /* Empty drag is ignored; numeric fields retain the last valid crop. */ }}
  const point=(event:React.PointerEvent<SVGSVGElement>)=>{const box=event.currentTarget.getBoundingClientRect();return{x:Math.max(0,Math.min(dimensions!.width,(event.clientX-box.left)/box.width*dimensions!.width)),y:Math.max(0,Math.min(dimensions!.height,(event.clientY-box.top)/box.height*dimensions!.height))}}
  return <section className="ocr-panel" aria-label="浏览器端 OCR 测试台" data-testid="ocr-panel">
    <header><h3>图片识别 · 20 位数字测试台</h3><p>固定模型 PP-OCRv6 tiny · 本机单线程 WASM · 仅格式检查，不验证业务真伪、不自动查询。飞书兼容性尚未验证。</p></header>
    <p className="ocr-privacy">图片、识别文字和确认编号仅在当前页面内存中；不会上传、写入日志或浏览器存储。缓存仅含公开模型资源。</p>
    {manifestError&&<p role="alert" className="ocr-error">{manifestError}。仍可手动输入编号，但无法开始识别。</p>}
    <div className="ocr-actions">
      <label className="ocr-file">上传图片<input ref={upload} data-testid="ocr-file-input" aria-label="上传 OCR 图片" type="file" accept="image/jpeg,image/png,image/webp" disabled={busy||decoding} onChange={e=>{void selectFile(e.target.files?.[0]);e.target.value=''}}/></label>
      <label className="ocr-file">拍照<input ref={capture} data-testid="ocr-camera-input" aria-label="拍照识别" type="file" accept="image/jpeg,image/png,image/webp" capture="environment" disabled={busy||decoding} onChange={e=>{void selectFile(e.target.files?.[0]);e.target.value=''}}/></label>
      <button type="button" disabled={!dimensions||busy||decoding} onClick={()=>{if(source.current)applyImage(source.current,(rotation+90)%360)}}>旋转 90°</button>
      <button type="button" className="ocr-primary" data-testid="ocr-start" disabled={!dimensions||busy||decoding||!!manifestError} onClick={()=>void start()}>开始识别</button>
      <button type="button" onClick={cancel} disabled={!dimensions&&!busy&&!decoding}>取消并清空</button>
      <button type="button" disabled={busy} onClick={()=>void clearModelCache().then(()=>setStatus('已清除模型缓存'),()=>setError('无法清除模型缓存，请使用浏览器站点设置'))}>清除模型缓存</button>
    </div>
    <p className="ocr-muted">JPEG / PNG / 静态 WebP，最多 15 MiB / 2400 万像素；自动校正 EXIF 方向，识别前长边缩至 2048。拍照入口是否调用相机取决于浏览器。</p>
    <p role="status" aria-live="polite" data-testid="ocr-status">{(busy||decoding)&&<span className="ocr-spinner" aria-hidden="true"/>}{status}</p>
    {error&&<p role="alert" className="ocr-error" data-testid="ocr-error">{error}</p>}
    {dimensions&&<div className="ocr-workspace">
      <div><div className="ocr-preview" style={{aspectRatio:`${dimensions.width} / ${dimensions.height}`}}>
        <canvas ref={preview} aria-label="原图预览（已校正方向与缩放）"/>
        <svg viewBox={`0 0 ${dimensions.width} ${dimensions.height}`} aria-label="拖动选择识别区域" onPointerDown={e=>{if(busy)return;drag.current=point(e);e.currentTarget.setPointerCapture(e.pointerId)}} onPointerMove={e=>{if(!drag.current||busy)return;const p=point(e);changeCrop({x:Math.min(p.x,drag.current.x),y:Math.min(p.y,drag.current.y),width:Math.abs(p.x-drag.current.x),height:Math.abs(p.y-drag.current.y)})}} onPointerUp={()=>{drag.current=undefined}} onPointerCancel={()=>{drag.current=undefined}}>
          {crop&&<rect className="ocr-crop" x={crop.x} y={crop.y} width={crop.width} height={crop.height}/>}
          {lines.map((line,i)=><polygon key={i} className="ocr-box" points={line.poly.map(p=>p.join(',')).join(' ')}><title>{`${i+1}. ${line.text}`}</title></polygon>)}
        </svg>
      </div><p className="ocr-muted">原图预览（{dimensions.width} × {dimensions.height}，旋转 {rotation}°）；虚线为选区，实线为真实 OCR 文字框。</p></div>
      <fieldset disabled={busy}><legend>手动选区（可拖动或输入像素）</legend><div className="ocr-crop-fields">{crop&&(['x','y','width','height'] as const).map(field=><label key={field}>{{x:'左侧 X',y:'顶部 Y',width:'宽度',height:'高度'}[field]}<input type="number" aria-label={`选区${field}`} min={field==='x'||field==='y'?0:1} max={field==='x'||field==='width'?dimensions.width:dimensions.height} value={crop[field]} onChange={e=>changeCrop({...crop,[field]:Number(e.target.value)})}/></label>)}</div><button type="button" onClick={()=>changeCrop({x:0,y:0,...dimensions})}>选择整图</button></fieldset>
    </div>}
    {result&&<p data-testid="ocr-timing">端到端 {(result.elapsedMs/1000).toFixed(2)} 秒（含加载）；检测 {result.metrics.detMs.toFixed(0)} ms / 识别 {result.metrics.recMs.toFixed(0)} ms。首次下载通常更慢。</p>}
    {!!lines.length&&<details open><summary>识别原文与模型分数（{lines.length} 行）</summary><p className="ocr-muted">模型分数仅供对照，不代表正确概率；请人工核对原图。</p><ol className="ocr-lines">{lines.map((line,i)=><li key={i}><span>{line.text}</span><small>模型分数：{Number.isFinite(line.score)?line.score.toFixed(6):'不可用'}</small></li>)}</ol></details>}
    <div><h4>20 位候选</h4><p className="ocr-muted">只合并同一识别行中的分组空格；不跨行拼接、不截取 21 位、不补零、不把 O 改为 0。</p>{candidates.length?<div className="ocr-candidates">{candidates.map(code=><button type="button" key={code} onClick={()=>{setManual(code);setConfirmed('')}}>{code}</button>)}</div>:<p>暂无严格匹配候选，可对照原图手动输入。</p>}</div>
    <label className="ocr-manual">手动修正编号<input data-testid="ocr-manual-code" aria-label="手动修正20位编号" type="text" inputMode="numeric" autoComplete="off" spellCheck={false} onPaste={e=>{if(/[\r\n\u2028\u2029]/.test(e.clipboardData.getData('text'))){e.preventDefault();setConfirmed('');setError('不允许跨行粘贴编号；请核对后输入完整单行编号')}}} value={manual} onChange={e=>{setManual(e.target.value);setConfirmed('')}} placeholder="保留前导零；请输入20位数字"/></label>
    <div className="ocr-actions"><span aria-live="polite">{manual?(isCode20(manual)?(confirmed?'格式有效：20 位 ASCII 数字（已确认）':'格式有效：20 位 ASCII 数字（尚未确认）'):'格式无效：必须是完整 20 位数字，不能跨行'):'等待输入或选择候选'}</span><button type="button" disabled={!isCode20(manual)||busy} onClick={()=>setConfirmed(normalizeCode(manual))}>确认编号（不查询）</button></div>
    {confirmed&&<p className="ocr-confirmed" data-testid="ocr-confirmed">已在本页确认：<strong>{confirmed}</strong>。未发送任何查询请求。</p>}
  </section>
}
