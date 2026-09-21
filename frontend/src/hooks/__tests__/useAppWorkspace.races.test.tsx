// @vitest-environment jsdom
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAppWorkspace } from '../useAppWorkspace';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), createProject: vi.fn(), setSearchParams: vi.fn() }));
vi.mock('@/services/api', () => ({ api: { get: mocks.get, post: mocks.post } }));
vi.mock('react-router-dom', () => ({ useSearchParams: () => [new URLSearchParams(), mocks.setSearchParams] }));
vi.mock('@/stores/useProjectStore', () => ({
  useProjectStore: (select: (state: { createProject: typeof mocks.createProject }) => unknown) => select({ createProject: mocks.createProject }),
}));
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => { resolve = res; });
  return { promise, resolve };
}
const assets = (id: number) => ({ assets: [{ id, name: `project-${id}`, asset_type: 'image' }] });
let root: Root;
let host: HTMLDivElement;
let latest: ReturnType<typeof useAppWorkspace>;
function Probe({ projectId }: { projectId: number }) {
  latest = useAppWorkspace(null, projectId);
  return <div>{latest.assets.map((asset) => asset.name).join(',')}</div>;
}
const render = async (projectId: number) => { await act(async () => root.render(<Probe projectId={projectId} />)); };
beforeEach(() => {
  vi.resetAllMocks();
  host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
});
afterEach(() => { act(() => root.unmount()); host.remove(); });
describe('workspace asset request ownership', () => {
  it('refreshes the current workspace after a successful image save', async () => {
    mocks.get.mockResolvedValueOnce(assets(1)).mockResolvedValueOnce(assets(12));
    mocks.post.mockResolvedValueOnce({});
    await render(1);
    await act(async () => latest.addImageAsset('https://example.invalid/image.png', 'new image'));
    expect(host.textContent).toBe('project-12');
    expect(mocks.post).toHaveBeenCalledWith('/projects/1/assets/', {
      asset_type: 'image', name: 'new image', url: 'https://example.invalid/image.png',
    });
  });

  it('does not launch a refresh after an unmounted asset save settles', async () => {
    const save = deferred<object>();
    mocks.get.mockResolvedValueOnce(assets(1));
    mocks.post.mockReturnValueOnce(save.promise);
    await render(1);
    let pending!: Promise<void>;
    act(() => { pending = latest.addImageAsset('https://example.invalid/image.png'); });
    await act(async () => root.render(null));
    await act(async () => { save.resolve({}); await pending; });
    expect(mocks.get).toHaveBeenCalledTimes(1);
  });

  it('does not accept the first visit response after navigating A to B to A', async () => {
    const old = deferred<ReturnType<typeof assets>>();
    mocks.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(assets(2)).mockResolvedValueOnce(assets(12));
    await render(1);
    await render(2);
    await render(1);
    await act(async () => old.resolve(assets(11)));
    expect(latest.projectId).toBe(1);
    expect(host.textContent).toBe('project-12');
  });

  it('never displays the old project assets when its response arrives after navigation', async () => {
    const old = deferred<ReturnType<typeof assets>>();
    mocks.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(assets(2));
    await render(1);
    await render(2);
    expect(latest.projectId).toBe(2);
    expect(host.textContent).toBe('project-2');
    await act(async () => old.resolve(assets(1)));
    expect(host.textContent).toBe('project-2');
  });
  it('keeps the newest asset refresh if an earlier request completes last', async () => {
    const old = deferred<ReturnType<typeof assets>>();
    mocks.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce(assets(12));
    await render(1);
    await act(async () => latest.refresh());
    await act(async () => old.resolve(assets(11)));
    expect(host.textContent).toBe('project-12');
  });
  it('does not refresh the previous workspace after an asset save completes across navigation', async () => {
    const save = deferred<object>();
    mocks.get.mockResolvedValueOnce(assets(1)).mockResolvedValueOnce(assets(2)).mockResolvedValueOnce(assets(1));
    mocks.post.mockReturnValueOnce(save.promise);
    await render(1);
    let pending!: Promise<void>;
    act(() => { pending = latest.addImageAsset('https://example.invalid/image.png'); });
    await render(2);
    await act(async () => { save.resolve({}); await pending; });
    expect(host.textContent).toBe('project-2');
    expect(mocks.get).toHaveBeenCalledTimes(2);
  });
});
