// @vitest-environment jsdom

/**
 * useOverlayNavigation — 导航时序测试（Architecture 2.0 §69）。
 *
 * requestNavigation 只记录目标；navigate 只发生在 afterOpenChange(false)
 * 之后，绝不提前、绝不用 setTimeout。
 */
import React from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useOverlayNavigation } from '@/router/useOverlayNavigation';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const roots: Array<{ host: HTMLElement; root: Root }> = [];

function setup() {
  const navigate = vi.fn();
  const Probe = () => {
    const { requestNavigation, afterOpenChange } = useOverlayNavigation(navigate);
    return (
      <div>
        <button type="button" onClick={() => requestNavigation('/chat/foo')}>pick</button>
        <button type="button" onClick={() => afterOpenChange(true)}>open-done</button>
        <button type="button" onClick={() => afterOpenChange(false)}>close-done</button>
      </div>
    );
  };
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  roots.push({ host, root });
  return { navigate, host, root, Probe };
}

afterEach(async () => {
  while (roots.length) {
    const { host, root } = roots.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('useOverlayNavigation', () => {
  it('navigates only after afterOpenChange(false) — never on pick, never on open', async () => {
    const { navigate, host, root, Probe } = setup();
    await act(async () => { root.render(<Probe />); });

    const pick = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'pick')!;
    await act(async () => pick.click());
    // 点击后：只记录，不导航（动画还在进行）。
    expect(navigate).not.toHaveBeenCalled();

    const openDone = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'open-done')!;
    await act(async () => openDone.click());
    expect(navigate).not.toHaveBeenCalled();

    const closeDone = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'close-done')!;
    await act(async () => closeDone.click());
    // 动画结束后导航恰好一次，目标是记录的路径。
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith('/chat/foo');
  });

  it('a plain close without a pending navigation does nothing', async () => {
    const { navigate, host, root, Probe } = setup();
    await act(async () => { root.render(<Probe />); });
    const closeDone = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'close-done')!;
    await act(async () => closeDone.click());
    expect(navigate).not.toHaveBeenCalled();
  });

  it('a second close event does not re-navigate (pending consumed once)', async () => {
    const { navigate, host, root, Probe } = setup();
    await act(async () => { root.render(<Probe />); });
    const pick = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'pick')!;
    await act(async () => pick.click());
    const closeDone = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'close-done')!;
    await act(async () => closeDone.click());
    await act(async () => closeDone.click());
    expect(navigate).toHaveBeenCalledTimes(1);
  });
});
