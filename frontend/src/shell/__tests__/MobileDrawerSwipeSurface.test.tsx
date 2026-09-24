// @vitest-environment jsdom

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';
import MobileDrawerSwipeSurface from '../MobileDrawerSwipeSurface';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLElement | null = null;
let root: Root | null = null;

async function renderSurface(open: boolean, onDismiss: () => void) {
  if (!host) {
    host = document.createElement('div');
    document.body.appendChild(host);
    root = createRoot(host);
  }
  await act(async () => {
    root!.render(
      <MobileDrawerSwipeSurface open={open} onDismiss={onDismiss}>
        <button type="button">首页</button>
      </MobileDrawerSwipeSurface>,
    );
  });
  const surface = host.querySelector<HTMLElement>('.mobile-shell__swipe-surface')!;
  vi.spyOn(surface, 'getBoundingClientRect').mockReturnValue({ width: 300 } as DOMRect);
  return surface;
}

function touch(surface: HTMLElement, type: 'touchstart' | 'touchmove' | 'touchend' | 'touchcancel', x: number, y: number) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  const point = { clientX: x, clientY: y } as Touch;
  Object.defineProperties(event, {
    touches: { value: type === 'touchend' || type === 'touchcancel' ? [] : [point] },
    changedTouches: { value: [point] },
  });
  surface.dispatchEvent(event);
  return event;
}

afterEach(async () => {
  if (root) await act(async () => root!.unmount());
  host?.remove();
  host = null;
  root = null;
  vi.restoreAllMocks();
});

describe('mobile navigation drawer swipe', () => {
  it('follows a left swipe and dismisses once it has moved far enough', async () => {
    const dismiss = vi.fn();
    const surface = await renderSurface(true, dismiss);
    await act(async () => {
      touch(surface, 'touchstart', 240, 300);
      const move = touch(surface, 'touchmove', 130, 305);
      expect(move.defaultPrevented).toBe(true);
      expect(surface.style.transform).toBe('translate3d(-110px, 0, 0)');
      touch(surface, 'touchend', 130, 305);
    });
    expect(dismiss).toHaveBeenCalledTimes(1);

    await renderSurface(false, dismiss);
    const reopened = await renderSurface(true, dismiss);
    expect(reopened.style.transform).toBe('');
  });

  it('leaves vertical scrolling and rightward gestures alone', async () => {
    const dismiss = vi.fn();
    const surface = await renderSurface(true, dismiss);
    await act(async () => {
      touch(surface, 'touchstart', 240, 300);
      const vertical = touch(surface, 'touchmove', 210, 220);
      expect(vertical.defaultPrevented).toBe(false);
      touch(surface, 'touchend', 210, 220);
      touch(surface, 'touchstart', 120, 300);
      touch(surface, 'touchmove', 190, 300);
      touch(surface, 'touchend', 190, 300);
    });
    expect(surface.style.transform).toBe('');
    expect(dismiss).not.toHaveBeenCalled();
  });

  it('snaps back after a short slow drag, but closes on a quick flick', async () => {
    let now = 0;
    vi.spyOn(performance, 'now').mockImplementation(() => now);
    const dismiss = vi.fn();
    const surface = await renderSurface(true, dismiss);
    await act(async () => {
      touch(surface, 'touchstart', 240, 300);
      now = 400;
      touch(surface, 'touchmove', 210, 300);
      now = 500;
      touch(surface, 'touchend', 210, 300);
    });
    expect(dismiss).not.toHaveBeenCalled();
    expect(surface.style.transform).toBe('translate3d(0, 0, 0)');

    await act(async () => {
      now = 600;
      touch(surface, 'touchstart', 240, 300);
      now = 650;
      touch(surface, 'touchmove', 190, 300);
      now = 670;
      touch(surface, 'touchend', 190, 300);
    });
    expect(dismiss).toHaveBeenCalledTimes(1);
  });
});
