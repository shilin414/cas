"""Real OCR smoke/privacy/lifecycle QA. Requires Pillow + playwright and local Chrome.
No OCR response mocking. The only generated content is a synthetic non-sensitive input image.
Run against `npm run dev -- --port 3039 --strictPort` after `npm run ocr:prepare`.
"""
import argparse, io, json, time
from pathlib import Path
from urllib.parse import urlparse
from PIL import Image, ImageDraw, ImageFont
from playwright.sync_api import sync_playwright

parser=argparse.ArgumentParser()
parser.add_argument('--url',default='http://localhost:3039/xiaoan-platform/scripts/ocr-browser-smoke.html')
parser.add_argument('--output',default=str(Path(__file__).resolve().parents[2]/'.run-logs/model/ocr'))
args=parser.parse_args(); out=Path(args.output); out.mkdir(parents=True,exist_ok=True)
code='01234567890123456789'
im=Image.new('RGB',(1500,280),'white')
ImageDraw.Draw(im).text((60,85),code,font=ImageFont.truetype('C:/Windows/Fonts/arial.ttf',90),fill='black')
buffer=io.BytesIO();im.save(buffer,format='PNG');(out/'synthetic-20.png').write_bytes(buffer.getvalue())
payload={'name':'synthetic-20.png','mimeType':'image/png','buffer':buffer.getvalue()}
origin=urlparse(args.url).netloc
instrument="""() => {
  window.__ocrAudit={created:0,terminated:0,bitmapClosed:0,action:null};
  const Original=window.Worker;
  window.Worker=class extends Original {
    constructor(...args){super(...args);window.__ocrAudit.created++;
      if(window.__ocrAudit.action) setTimeout(()=>{const action=window.__ocrAudit.action;window.__ocrAudit.action=null;
        if(action==='cancel') [...document.querySelectorAll('button')].find(b=>b.textContent==='取消并清空')?.click();
        if(action==='unmount') document.querySelector('[data-testid=ocr-unmount]')?.click();
      },0);
    }
    terminate(){window.__ocrAudit.terminated++;return super.terminate();}
  };
  const close=ImageBitmap.prototype.close;
  ImageBitmap.prototype.close=function(){window.__ocrAudit.bitmapClosed++;return close.call(this);};
}"""

def audit_runtime(requests):
  return [r for r in requests if any(s in r['url'] for s in ('runtime-client','ocr.worker','ort-wasm','.tar','opencv','paddleocr'))]

with sync_playwright() as p:
  browser=p.chromium.launch(channel='chrome',headless=True)
  report={'browser':browser.version,'deviceScope':'Desktop Chrome + mobile viewport/touch emulation; NOT a real mobile/Feishu test','syntheticInput':code,'runs':[]}
  for name,options in [('desktop',{'viewport':{'width':1280,'height':900}}),('mobile',{'viewport':{'width':390,'height':844},'is_mobile':True,'has_touch':True,'device_scale_factor':1})]:
    context=browser.new_context(**options);context.add_init_script('('+instrument+')()')
    page=context.new_page();requests=[];page_errors=[];console_messages=[];support_modules=[]
    context.on('request',lambda r:requests.append({'url':r.url,'method':r.method,'hasBody':r.post_data is not None}))
    context.on('response',lambda r:support_modules.append({'url':r.url,'contentType':r.headers.get('content-type','')}) if 'ort-wasm-simd-threaded' in r.url and urlparse(r.url).path.endswith(('.js','.mjs')) else None)
    page.on('pageerror',lambda e:page_errors.append(str(e)))
    page.on('console',lambda m:console_messages.append(m.text))
    page.goto(args.url);page.get_by_text('打开本地 OCR 测试',exact=True).wait_for();page.wait_for_timeout(250)
    before_open=audit_runtime(list(requests))
    page.get_by_text('打开本地 OCR 测试',exact=True).click();page.get_by_test_id('ocr-panel').wait_for()
    page.get_by_test_id('ocr-file-input').set_input_files(payload)
    page.wait_for_function("!document.querySelector('[data-testid=ocr-start]').disabled")
    before_start=audit_runtime(list(requests));page.get_by_test_id('ocr-start').click()
    page.wait_for_function("document.querySelector('[data-testid=ocr-error]') || document.querySelector('[data-testid=ocr-timing]')",timeout=150000)
    if page.get_by_test_id('ocr-error').count(): raise RuntimeError(page.get_by_test_id('ocr-error').inner_text())
    assert page.locator('.ocr-candidates button').filter(has_text=code).count()==1
    cold_timing=page.get_by_test_id('ocr-timing').inner_text()
    page.locator('.ocr-candidates button').filter(has_text=code).click()
    count=len(requests);page.get_by_role('button',name='确认编号（不查询）',exact=True).click();page.get_by_test_id('ocr-confirmed').wait_for();page.wait_for_timeout(150)
    confirm_requests=requests[count:]
    page.screenshot(path=str(out/f'ocr-{name}.png'),full_page=True)
    overflow=page.evaluate('document.documentElement.scrollWidth > window.innerWidth')
    cache_keys=page.evaluate('caches.keys()')
    initial_tar_count=len([r for r in requests if '.tar' in r['url']])
    # Rotation and numeric crop use the actual image, not a fake recognition response.
    for _ in range(4): page.get_by_role('button',name='旋转 90°',exact=True).click()
    for field,value in [('x','30'),('y','60'),('width','1150'),('height','160')]: page.get_by_label('选区'+field,exact=True).fill(value)
    # Repeated REAL inference on crop: cache must revalidate with no tar network request.
    page.get_by_test_id('ocr-start').click()
    page.wait_for_function("document.querySelector('[data-testid=ocr-timing]')",timeout=150000)
    warm_timing=page.get_by_test_id('ocr-timing').inner_text()
    crop_correct=page.locator('.ocr-candidates button').filter(has_text=code).count()==1
    assert crop_correct
    polygon=page.locator('.ocr-box').first.get_attribute('points')
    assert all(float(pair.split(',')[0])>=30 and float(pair.split(',')[1])>=60 for pair in polygon.split())
    cache_hit_no_download=initial_tar_count==len([r for r in requests if '.tar' in r['url']])
    assert cache_hit_no_download
    # Terminate just-created real worker, before it can produce output.
    page.evaluate("window.__ocrAudit.action='cancel'")
    page.get_by_test_id('ocr-start').click()
    page.wait_for_function("document.querySelector('[data-testid=ocr-status]').textContent.includes('已取消')")
    page.wait_for_timeout(800)
    after_cancel=page.evaluate('({...window.__ocrAudit,visibleCanvases:document.querySelectorAll(".ocr-preview canvas").length,hasOutput:!!document.querySelector("[data-testid=ocr-timing]")})')
    assert after_cancel['created']==after_cancel['terminated'] and after_cancel['bitmapClosed']>=1 and after_cancel['visibleCanvases']==0 and not after_cancel['hasOutput']
    # Decode a new image, then unmount with a live real worker.
    page.get_by_test_id('ocr-file-input').set_input_files(payload)
    page.wait_for_function("!document.querySelector('[data-testid=ocr-start]').disabled")
    page.evaluate("window.__ocrAudit.action='unmount'")
    page.get_by_test_id('ocr-start').click();page.get_by_text('已离开 OCR 面板',exact=True).wait_for();page.wait_for_timeout(800)
    after_unmount=page.evaluate('({...window.__ocrAudit,panelPresent:!!document.querySelector("[data-testid=ocr-panel]")})')
    assert after_unmount['created']==after_unmount['terminated'] and after_unmount['bitmapClosed']>=2 and not after_unmount['panelPresent']
    external=[r for r in requests if urlparse(r['url']).netloc not in ('',origin)]
    uploads=[r for r in requests if r['method'] not in ('GET','HEAD') or r['hasBody']]
    code_in_console=any(code in m for m in console_messages)
    assert not before_open and not before_start and not external and not uploads and not confirm_requests and not code_in_console and not overflow and not page_errors
    assert support_modules, 'No actual ORT support-module response was captured'
    assert all(urlparse(r['url']).path.endswith('.js') and 'javascript' in r['contentType'] for r in support_modules)
    entry={'supportModules':support_modules,'viewport':name,'coldTiming':cold_timing,'warmTiming':warm_timing,'candidateExact':True,'croppedCandidateExact':crop_correct,'cropBoxesMappedToPreview':True,'beforeOpenRuntimeRequests':before_open,'beforeStartRuntimeRequests':before_start,'externalRequests':external,'uploadsOrBodyRequests':uploads,'confirmRequests':confirm_requests,'codeInConsole':code_in_console,'horizontalOverflow':overflow,'cacheHitNoModelDownload':cache_hit_no_download,'cacheNames':cache_keys,'afterCancel':after_cancel,'afterUnmount':after_unmount,'pageErrors':page_errors}
    report['runs'].append(entry)
    (out/f'network-{name}.json').write_text(json.dumps(requests,indent=2),encoding='utf8')
    context.close()
  # Deliberately corrupt a MODEL download (not an OCR result) to prove integrity rejection.
  context=browser.new_context();context.add_init_script('('+instrument+')()');page=context.new_page()
  tar_path=Path(__file__).resolve().parents[1]/'public/ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_det.tar'
  corrupted=bytearray(tar_path.read_bytes());corrupted[0]^=1
  tar_requests=[]
  context.on('request',lambda r:tar_requests.append(r.url) if r.url.endswith('PP-OCRv6_tiny_det.tar') else None)
  page.route('**/PP-OCRv6_tiny_det.tar',lambda route:route.fulfill(status=200,content_type='application/octet-stream',body=bytes(corrupted)))
  page.goto(args.url);page.get_by_text('打开本地 OCR 测试',exact=True).click();page.get_by_test_id('ocr-file-input').set_input_files(payload)
  page.wait_for_function("!document.querySelector('[data-testid=ocr-start]').disabled")
  page.get_by_test_id('ocr-start').click();page.get_by_test_id('ocr-error').wait_for(timeout=30000)
  integrity_error=page.get_by_test_id('ocr-error').inner_text()
  assert 'SHA-256' in integrity_error and page.evaluate('window.__ocrAudit.created')==0
  page.screenshot(path=str(out/'integrity-failure.png'),full_page=True)
  page.unroute('**/PP-OCRv6_tiny_det.tar')
  page.get_by_test_id('ocr-start').click();page.get_by_test_id('ocr-timing').wait_for(timeout=150000)
  assert page.locator('.ocr-candidates button').filter(has_text=code).count()==1
  # Poison persisted cache; next call must reject the cache and recover from verified same-origin bytes.
  page.evaluate("""async()=>{const keys=await caches.keys();const cache=await caches.open(keys.find(k=>k.startsWith('cas-ocr-')));const requests=await cache.keys();await cache.put(requests.find(r=>r.url.endsWith('PP-OCRv6_tiny_det.tar')),new Response('bad cache'));}""")
  previous=len(tar_requests);page.get_by_test_id('ocr-start').click();page.get_by_test_id('ocr-timing').wait_for(timeout=150000)
  assert len(tar_requests)==previous+1 and page.locator('.ocr-candidates button').filter(has_text=code).count()==1
  report['integrity']={'tamperedModelRejected':True,'rejectionBeforeWorker':True,'error':integrity_error,'recoveredFromRealModel':True,'poisonedCacheEvictedAndRefetched':True}
  context.close()
  (out/'qa-report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf8')
  print(json.dumps(report,ensure_ascii=True,indent=2))
  browser.close()
