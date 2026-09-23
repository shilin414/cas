/**
 * compressImage — 上传前的图片自动压缩（>5MB 的 Aily 官方图片限额）。
 *
 * 压缩发生在浏览器本地：原图不上传，所以不受 40MB 上传体积限制约束。
 * 策略是「先保分辨率降质量，不够再降分辨率」（质量阶梯外层、分辨率阶梯内层）：
 *   scale=1 × q 0.92→0.55，仍超限时 scale ×0.85 → ×0.7 → ×0.55 → ×0.4。
 * 产物统一为 JPEG（PNG 透明像素落到白底，同 ocr/image.ts 的绘制模式）。
 */
import { ATTACHMENT_LIMITS } from '@/services/runApi';

/** JPEG 质量阶梯（降序）：先牺牲压缩率，保住分辨率。 */
export const COMPRESS_QUALITY_STEPS = [0.92, 0.85, 0.78, 0.7, 0.62, 0.55] as const;
/** 分辨率缩放阶梯：质量降到底仍超限时才缩小图像。 */
export const COMPRESS_SCALE_STEPS = [1, 0.85, 0.7, 0.55, 0.4] as const;
/** 单次绘制内存预算（与 OCR 的 MAX_IMAGE_PIXELS 一致），防超大图撑爆移动端。 */
export const MAX_RENDER_PIXELS = 24_000_000;

/** 压缩不可完成（解码失败 / 全阶梯仍超限 / 编码失败）。 */
export class ImageCompressionError extends Error {}

const IMAGE_EXTENSIONS = ['png', 'jpg', 'jpeg'];

/**
 * 判定一个文件是否是「超限、但可以尝试压缩」的图片：png/jpg/jpeg 扩展名
 * 且超过图片限额。压缩在本地进行、原图不上传，所以没有 40MB 上限一说。
 */
export function isCompressibleImage(file: File): boolean {
  const ext = (file.name.split('.').pop() || '').toLowerCase();
  return IMAGE_EXTENSIONS.includes(ext) && file.size > ATTACHMENT_LIMITS.imageMaxBytes;
}

const toBlob = (canvas: HTMLCanvasElement, quality: number): Promise<Blob | null> =>
  new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', quality));

const mb = (bytes: number): string => (bytes / (1024 * 1024)).toFixed(1);

/**
 * 把超限图片压到 maxBytes（默认 Aily 图片限额）以内，返回 JPEG File。
 * 文件名同步换为 .jpg 扩展名（后端按扩展名白名单校验，PNG 内容已变 JPEG）。
 */
export async function compressImage(
  file: File,
  maxBytes: number = ATTACHMENT_LIMITS.imageMaxBytes,
): Promise<File> {
  let bitmap: ImageBitmap;
  try {
    // 'from-image'：浏览器按 EXIF 方向摆正（同 ocr/image.ts:39）。
    bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  } catch {
    throw new ImageCompressionError(`「${file.name}」无法读取，请换一张图片`);
  }
  try {
    // 像素预算钳制：超大图（如 100MP）先按比例缩小到预算内再进阶梯。
    const pixelScale = Math.min(
      1,
      Math.sqrt(MAX_RENDER_PIXELS / (bitmap.width * bitmap.height)),
    );
    let best: { blob: Blob; quality: number; scale: number } | null = null;
    for (const scaleStep of COMPRESS_SCALE_STEPS) {
      const scale = scaleStep * pixelScale;
      const width = Math.max(1, Math.round(bitmap.width * scale));
      const height = Math.max(1, Math.round(bitmap.height * scale));
      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const context = canvas.getContext('2d');
      if (!context) {
        throw new ImageCompressionError('浏览器无法创建图像画布');
      }
      // PNG 透明 → 白底 JPEG（同 ocr/image.ts:47）。
      context.fillStyle = '#fff';
      context.fillRect(0, 0, width, height);
      context.imageSmoothingEnabled = true;
      context.imageSmoothingQuality = 'high';
      context.drawImage(bitmap, 0, 0, width, height);
      for (const quality of COMPRESS_QUALITY_STEPS) {
        const blob = await toBlob(canvas, quality);
        if (!blob) continue;
        if (blob.size <= maxBytes) {
          return new File(
            [blob],
            `${file.name.replace(/\.[^.]+$/, '')}.jpg`,
            { type: 'image/jpeg' },
          );
        }
        // 记录全阶梯中最小的一个，全部失败时报出实际可达尺寸。
        if (!best || blob.size < best.blob.size) {
          best = { blob, quality, scale };
        }
      }
    }
    if (best) {
      throw new ImageCompressionError(
        `「${file.name}」压缩后仍超过 ${mb(maxBytes)} MB（最小 ${mb(best.blob.size)} MB），请换一张图片`,
      );
    }
    throw new ImageCompressionError(`「${file.name}」压缩失败，请换一张图片`);
  } finally {
    bitmap.close();
  }
}
