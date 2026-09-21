/// <reference lib="webworker" />
import { PaddleOCR } from '@paddleocr/paddleocr-js'
import * as ort from 'onnxruntime-web'
import cvModule from '@techstark/opencv-js'
import type { OcrLine } from './logic'

interface Request { image: ImageData; resources: Record<string, ArrayBuffer>; wasm: ArrayBuffer; moduleUrl: string }
const scope = self as unknown as DedicatedWorkerGlobalScope
// This source is bundled by prepare-ocr-runtime.mjs. No manifest-supplied executable is accepted.
scope.onmessage = async ({data}: MessageEvent<Request>) => {
  let pipeline: Awaited<ReturnType<typeof PaddleOCR.create>> | undefined
  try {
    ort.env.wasm.numThreads = 1
    ort.env.wasm.proxy = false
    ort.env.wasm.wasmBinary = data.wasm
    ort.env.wasm.wasmPaths = {mjs:data.moduleUrl}
    ort.env.logLevel = 'fatal'
    scope.postMessage({type:'progress', message:'初始化本地 WASM 与 tiny 检测/识别模型'})
    pipeline = await PaddleOCR.create({
      worker: false,
      textDetectionModelName: 'PP-OCRv6_tiny_det',
      textRecognitionModelName: 'PP-OCRv6_tiny_rec',
      textDetectionModelAsset: {url:'PP-OCRv6_tiny_det'},
      textRecognitionModelAsset: {url:'PP-OCRv6_tiny_rec'},
      fetch: async (input: RequestInfo | URL) => {
        const bytes = data.resources[String(input)]
        if (!bytes) throw new Error('拒绝未校验的模型资源')
        return new Response(bytes)
      },
      ortOptions:{backend:'wasm',numThreads:1,proxy:false},
      textDetectionBatchSize:1, textRecognitionBatchSize:1,
      unsupportedBehavior:'error',
    })
    // Release duplicate tar buffers before inference; ONNX sessions own their model data.
    data.resources = {}
    scope.postMessage({type:'progress', message:'正在本机识别；图片不会上传'})
    // SDK 0.4.2's ImageData/Bitmap path uses document; cv.Mat is its supported DOM-free input.
    // Initialization above has already awaited the shared OpenCV runtime.
    const cv = cvModule instanceof Promise ? await cvModule : cvModule
    const mat = cv.matFromImageData(data.image)
    let result
    try { [result] = await pipeline.predict(mat, {textDetLimitSideLen:1536,textDetLimitType:'max',textRecScoreThresh:0}) }
    finally { mat.delete() }
    const items: OcrLine[] = result.items.map(item => ({text:item.text,score:item.score,poly:item.poly}))
    scope.postMessage({type:'result',items,metrics:result.metrics})
  } catch (error) {
    // Error details go only to the calling UI, never the console or telemetry.
    scope.postMessage({type:'error', message: error instanceof Error ? error.message : '本地 OCR 初始化或识别失败'})
  } finally {
    await pipeline?.dispose().catch(() => undefined)
    scope.close()
  }
}
