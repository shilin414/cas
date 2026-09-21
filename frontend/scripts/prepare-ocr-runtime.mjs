import { build } from 'esbuild';
import { mkdir, copyFile, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root = fileURLToPath(new URL('../', import.meta.url));
const out = path.join(root, 'src/features/ai-models/ocr/runtime');
await mkdir(out, {recursive:true});
const ortPackage = JSON.parse(await readFile(path.join(root,'node_modules/onnxruntime-web/package.json'),'utf8'));
const sdkPackage = JSON.parse(await readFile(path.join(root,'node_modules/@paddleocr/paddleocr-js/package.json'),'utf8'));
if (ortPackage.version !== '1.22.0' || sdkPackage.version !== '0.4.2') throw new Error('OCR runtime dependency drift: expected SDK 0.4.2 / ORT 1.22.0. Review and revalidate before changing pins.');
await build({
  absWorkingDir:root, entryPoints:['src/features/ai-models/ocr/ocr.worker.ts'],
  outfile:path.join(out,'ocr.worker.js'), bundle:true, platform:'browser', format:'esm', target:'es2022', minify:true,
  // The pure WASM build needs no WebGPU/JSEP runtime and no remotely imported module.
  alias:{'onnxruntime-web':path.join(root,'node_modules/onnxruntime-web/dist/ort.wasm.bundle.min.mjs')},
  logLevel:'warning', legalComments:'eof', external:['fs','path'],
});
await copyFile(path.join(root,'node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.wasm'),path.join(out,'ort-wasm-simd-threaded.wasm'));



await copyFile(path.join(root,'node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.mjs'),path.join(out,'ort-wasm-simd-threaded.js'));
// Preserve upstream ESM bytes, but publish .js for the default Nginx MIME mapping.
console.log('Prepared pinned local OCR worker, .js support module and ORT WASM (no models downloaded).');
