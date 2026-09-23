// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  compressImage,
  isCompressibleImage,
  ImageCompressionError,
  COMPRESS_QUALITY_STEPS,
  COMPRESS_SCALE_STEPS,
} from '../compressImage';
import { ATTACHMENT_LIMITS } from '@/services/runApi';

const fiveMb = ATTACHMENT_LIMITS.imageMaxBytes;

function file(name: string, size: number, type = 'image/png'): File {
  return new File([new Uint8Array(size)], name, { type });
}

/** Fake ImageBitmap with a controllable footprint; close() must be spyable. */
function fakeBitmap(width: number, height: number) {
  return { width, height, close: vi.fn() } as unknown as ImageBitmap;
}

type ToBlobPlan = Array<{ size: number } | null>;

/**
 * A real Blob whose payload is exactly `size` bytes. The File constructor
 * re-reads the underlying data (jsdom ignores patched `size` properties), so
 * the only reliable way to control `File.size` is a real payload of that
 * length; a few MB of zeros per test is nothing.
 */
function fakeBlob(size: number): Blob {
  return new Blob([new Uint8Array(size)], { type: 'image/jpeg' });
}

/**
 * jsdom ships canvas methods as "Not implemented" stubs (getContext returns
 * null, toBlob never fires its callback), so BOTH are replaced here:
 *
 *   getContext → a recording 2D-context fake (fillRect/drawImage spies).
 *   toBlob     → every call consumes the next plan entry ({size} → JPEG-ish
 *                blob of that size, null → encoding failure), recording the
 *                canvas (width, height) and quality it was called with.
 */
function stubCanvas(plan: ToBlobPlan) {
  const calls: Array<{ quality?: number; width: number; height: number }> = [];
  const fillRect = vi.fn();
  const drawImage = vi.fn();
  const getContext = vi
    .spyOn(HTMLCanvasElement.prototype, 'getContext')
    .mockImplementation(() => ({
      fillStyle: '',
      fillRect,
      drawImage,
      imageSmoothingEnabled: false,
      imageSmoothingQuality: '',
    }) as unknown as CanvasRenderingContext2D);
  const toBlob = vi
    .spyOn(HTMLCanvasElement.prototype, 'toBlob')
    .mockImplementation(function (
      this: HTMLCanvasElement,
      callback: BlobCallback,
      _type?: string,
      quality?: number,
    ) {
      calls.push({ quality, width: this.width, height: this.height });
      const next = plan.length ? plan.shift()! : null;
      const blob = next === null ? null : fakeBlob(next.size);
      queueMicrotask(() => callback(blob));
    });
  return { calls, fillRect, drawImage, getContext, toBlob };
}

describe('isCompressibleImage', () => {
  it('flags oversize png/jpg/jpeg only', () => {
    expect(isCompressibleImage(file('big.png', fiveMb + 1))).toBe(true);
    expect(isCompressibleImage(file('big.PNG', fiveMb + 1))).toBe(true);
    expect(isCompressibleImage(file('big.jpg', fiveMb + 1))).toBe(true);
    expect(isCompressibleImage(file('big.jpeg', fiveMb + 1))).toBe(true);
    expect(isCompressibleImage(file('small.png', fiveMb))).toBe(false);
    expect(isCompressibleImage(file('doc.pdf', fiveMb + 1, 'application/pdf'))).toBe(false);
    // 超过 40MB 的图片同样压缩：原图不上传，不受上传体积限制约束。
    expect(isCompressibleImage(file('huge.png', 60 * 1024 * 1024))).toBe(true);
  });
});

describe('compressImage', () => {
  const originalBitmap = globalThis.createImageBitmap;

  afterEach(() => {
    Object.defineProperty(globalThis, 'createImageBitmap', {
      value: originalBitmap,
      configurable: true,
      writable: true,
    });
    vi.restoreAllMocks();
  });

  function stubBitmap(width: number, height: number) {
    const bitmap = fakeBitmap(width, height);
    Object.defineProperty(globalThis, 'createImageBitmap', {
      value: vi.fn().mockResolvedValue(bitmap),
      configurable: true,
      writable: true,
    });
    return bitmap;
  }

  it('keeps full resolution and lowers quality first', async () => {
    const bitmap = stubBitmap(2000, 1000);
    // q=0.92 超限，q=0.85 命中。
    const { calls } = stubCanvas([{ size: fiveMb + 1 }, { size: 3 * 1024 * 1024 }]);

    const out = await compressImage(file('photo.png', fiveMb + 1));

    expect(out.name).toBe('photo.jpg');
    expect(out.type).toBe('image/jpeg');
    expect(out.size).toBe(3 * 1024 * 1024);
    // 只有原分辨率的两次尝试，没有动 scale。
    expect(calls.length).toBe(2);
    expect(calls[0].quality).toBe(COMPRESS_QUALITY_STEPS[0]);
    expect(calls[1].quality).toBe(COMPRESS_QUALITY_STEPS[1]);
    expect(calls[0].width).toBe(2000);
    expect(calls[0].height).toBe(1000);
    expect(bitmap.close).toHaveBeenCalled();
  });

  it('falls back to a smaller scale only after quality is exhausted', async () => {
    stubBitmap(2000, 1000);
    // scale=1 的全部 quality 超限，scale=0.85 的 q=0.92 命中。
    const over = { size: fiveMb + 1 };
    const { calls } = stubCanvas([
      ...COMPRESS_QUALITY_STEPS.map(() => over),
      { size: 3 * 1024 * 1024 },
    ]);

    const out = await compressImage(file('photo.png', fiveMb + 1));

    expect(out.size).toBe(3 * 1024 * 1024);
    expect(calls.length).toBe(COMPRESS_QUALITY_STEPS.length + 1);
    // 降分辨率那一轮的画布是 2000×0.85 = 1700。
    const lastCall = calls[calls.length - 1];
    expect(lastCall.width).toBe(1700);
    expect(lastCall.height).toBe(850);
    expect(lastCall.quality).toBe(COMPRESS_QUALITY_STEPS[0]);
  });

  it('renames the extension but keeps the stem (jpg passthrough too)', async () => {
    stubBitmap(100, 100);
    stubCanvas([{ size: 1000 }]);

    const jpgIn = await compressImage(file('photo.jpeg', fiveMb + 1, 'image/jpeg'));

    expect(jpgIn.name).toBe('photo.jpg');
    expect(jpgIn.type).toBe('image/jpeg');
  });

  it('throws ImageCompressionError when every step stays over the limit', async () => {
    stubBitmap(100, 100);
    const over = { size: fiveMb + 1 };
    stubCanvas(
      Array.from({ length: COMPRESS_QUALITY_STEPS.length * COMPRESS_SCALE_STEPS.length }, () => over),
    );

    await expect(compressImage(file('photo.png', fiveMb + 1)))
      .rejects.toBeInstanceOf(ImageCompressionError);
  });

  it('throws when toBlob always returns null', async () => {
    stubBitmap(100, 100);
    stubCanvas([null, null, null]);

    await expect(compressImage(file('photo.png', fiveMb + 1)))
      .rejects.toBeInstanceOf(ImageCompressionError);
  });

  it('throws when decoding fails (unreadable file)', async () => {
    Object.defineProperty(globalThis, 'createImageBitmap', {
      value: vi.fn().mockRejectedValue(new Error('decode failed')),
      configurable: true,
      writable: true,
    });

    await expect(compressImage(file('broken.png', fiveMb + 1)))
      .rejects.toBeInstanceOf(ImageCompressionError);
  });

  it('clamps oversized bitmaps into the 24MP render budget', async () => {
    // 5000×6000 = 30MP → 首轮画布按 sqrt(24/30) 缩放。
    stubBitmap(5000, 6000);
    const { calls } = stubCanvas([{ size: 1000 }]);

    await compressImage(file('huge.png', fiveMb + 1));

    const clamp = Math.sqrt(24_000_000 / (5000 * 6000));
    expect(calls[0].width).toBe(Math.round(5000 * clamp));
    expect(calls[0].height).toBe(Math.round(6000 * clamp));
  });

  it('draws a white background (PNG transparency → white JPEG)', async () => {
    stubBitmap(10, 10);
    const { fillRect, drawImage } = stubCanvas([{ size: 1000 }]);

    await compressImage(file('photo.png', fiveMb + 1));

    expect(fillRect).toHaveBeenCalledWith(0, 0, expect.any(Number), expect.any(Number));
    expect(drawImage).toHaveBeenCalled();
  });
});
