import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const read = (relative) => readFile(new URL(relative, import.meta.url), 'utf8');
test('standard build prepares pinned runtime AND models, dev remains model-download-free', async () => {
  const pkg=JSON.parse(await read('../package.json'));
  assert.equal(pkg.scripts.prebuild, 'npm run ocr:runtime && npm run ocr:prepare');
  assert.equal(pkg.scripts.predev, 'npm run ocr:runtime');
});
test('ORT support module is published/imported as .js and protocol agrees', async () => {
  const prepare=await read('./prepare-ocr-runtime.mjs');
  const client=await read('../src/features/ai-models/ocr/runtime-client.ts');
  const worker=await read('../src/features/ai-models/ocr/ocr.worker.ts');
  assert.match(prepare, /ort-wasm-simd-threaded\.mjs'\),path\.join\(out,'ort-wasm-simd-threaded\.js'/);
  assert.match(client, /import moduleUrl from '\.\/runtime\/ort-wasm-simd-threaded\.js\?url'/);
  assert.doesNotMatch(client, /ort-wasm-simd-threaded\.mjs/);
  assert.match(client, /moduleUrl:new URL\(moduleUrl/);
  assert.match(worker, /moduleUrl: string/);
  assert.match(worker, /wasmPaths = \{mjs:data.moduleUrl\}/);
});

import { createHash } from 'node:crypto';
import { mkdtemp, mkdir, writeFile, cp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { spawnSync } from 'node:child_process';
async function fixture(mode) {
  const directory=await mkdtemp(path.join(tmpdir(),'cas-ocr-deployment-'));
  const frontend=path.join(directory,'frontend');
  const scripts=path.join(frontend,'scripts');
  await mkdir(scripts,{recursive:true});
  for(const file of ['prepare-ocr-resources.mjs','deployment.mjs']) await cp(new URL(file,import.meta.url),path.join(scripts,file));
  await writeFile(path.join(directory,'deployment.json'),JSON.stringify({basePath:'/test-prefix/'}));
  // Small synthetic bytes ONLY test the preparation script's hash/cache behavior, not OCR output.
  const resources=['PP-OCRv6_tiny_det','PP-OCRv6_tiny_rec'].map(name=>{
    const bytes=Buffer.from(`pinned test fixture ${name}`);
    return {name,bytes,size_bytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex'),source:'https://must-not-be-contacted.invalid/'+name};
  });
  await writeFile(path.join(scripts,'ocr-resources.mjs'),`export const version='fixture-v1';export const upstreamCommit='fixture';export const resources=${JSON.stringify(resources.map(({bytes,...r})=>r))};`);
  const destination=path.join(frontend,'public/ocr-assets/fixture-v1');
  if(mode!=='missing') {
    await mkdir(destination,{recursive:true});
    for(const r of resources) await writeFile(path.join(destination,r.name+'.tar'),mode==='valid'?r.bytes:Buffer.from('corrupt'));
  }
  const script=pathToFileURL(path.join(scripts,'prepare-ocr-resources.mjs')).href;
  const result=spawnSync(process.execPath,['--input-type=module','--eval',`globalThis.fetch=async()=>{throw new Error('offline: unexpected network attempt')};await import(${JSON.stringify(script)})`],{cwd:frontend,encoding:'utf8',env:{...process.env,APP_BASE_PATH:'/test-prefix/'}});
  return {destination,result,resources};
}
test('verified fixed-hash cache builds manifest fully offline',async()=>{
  const {destination,result,resources}=await fixture('valid');
  assert.equal(result.status,0,result.stderr);
  const manifest=JSON.parse(await readFile(path.join(destination,'manifest.json'),'utf8'));
  assert.equal(manifest.resources.length,2);
  assert.equal(manifest.resources[0].sha256,resources[0].sha256);
  assert.equal(manifest.resources[0].url,'/test-prefix/ocr-assets/fixture-v1/PP-OCRv6_tiny_det.tar');
});
for(const mode of ['missing','corrupt']) test(`${mode} offline resources fail closed without producing manifest`,async()=>{
  const {destination,result}=await fixture(mode);
  assert.notEqual(result.status,0);assert.match(result.stderr,/offline: unexpected network attempt/);
  await assert.rejects(readFile(path.join(destination,'manifest.json')), {code:'ENOENT'});
});
