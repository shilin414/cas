import { createHash } from 'node:crypto';
import { readFile, writeFile, mkdir, rename } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { readDeployment } from './deployment.mjs';
import { version, resources, upstreamCommit } from './ocr-resources.mjs';

const frontend=fileURLToPath(new URL('../',import.meta.url));
const output=path.join(frontend,'public/ocr-assets');
const directory=path.join(output,version);
await mkdir(directory,{recursive:true});
// This generated resource subtree is deliberately excluded from source control.
await writeFile(path.join(output,'.gitignore'),'*\n!.gitignore\n');
const valid=(bytes,r)=>bytes.length===r.size_bytes && createHash('sha256').update(bytes).digest('hex')===r.sha256;
for(const resource of resources) {
  const target=path.join(directory,`${resource.name}.tar`);
  let existing;
  try { existing=await readFile(target) } catch { /* cold deployment */ }
  if(existing && valid(existing,resource)) {console.log(`${resource.name}: existing verified (${existing.length} bytes)`);continue;}
  console.log(`Downloading official ${resource.name} (${resource.size_bytes} bytes)`);
  const response=await fetch(resource.source,{redirect:'error',signal:AbortSignal.timeout(180_000)});
  if(!response.ok) throw new Error(`${resource.name}: HTTP ${response.status}; no model substituted`);
  const chunks=[];let length=0;
  for await (const chunk of response.body) {length+=chunk.length;if(length>resource.size_bytes)throw new Error(`${resource.name}: size overflow`);chunks.push(chunk);}
  const bytes=Buffer.concat(chunks);
  if(!valid(bytes,resource)) throw new Error(`${resource.name}: SHA-256 / size mismatch; upstream may have changed. Review explicitly; no silent update.`);
  const temp=target+'.partial';await writeFile(temp,bytes);await rename(temp,target);
}
const base=readDeployment().basePath;
const manifest={adapter:'paddleocr_tiny',version,resources:resources.map(({name,size_bytes,sha256})=>({name,url:`${base}ocr-assets/${version}/${name}.tar`,sha256,size_bytes}))};
await writeFile(path.join(directory,'manifest.json'),JSON.stringify(manifest,null,2)+'\n');
await writeFile(path.join(directory,'provenance.json'),JSON.stringify({upstreamCommit,sdk:'@paddleocr/paddleocr-js@0.4.2',ort:'onnxruntime-web@1.22.0',resources},null,2)+'\n');
console.log(`Verified archives total ${resources.reduce((sum,r)=>sum+r.size_bytes,0)} bytes. UI manifest: ${path.join(directory,'manifest.json')}`);
console.log(JSON.stringify(manifest,null,2));
