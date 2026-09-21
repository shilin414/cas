// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';

const mocks = vi.hoisted(() => ({
  beforeUpload: null as null | ((file: File) => boolean | Promise<boolean>),
  upload: vi.fn(),
  clear: vi.fn(),
  createObjectURL: vi.fn(),
  revokeObjectURL: vi.fn(),
}));

vi.mock('antd', () => ({
  Avatar: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  Button: ({ children }: { children?: React.ReactNode }) => <button type="button">{children}</button>,
  Modal: ({ open, children }: { open?: boolean; children?: React.ReactNode }) => (
    open ? <div data-testid="avatar-modal">{children}</div> : null
  ),
  Upload: ({ beforeUpload, children }: {
    beforeUpload: (file: File) => boolean | Promise<boolean>; children?: React.ReactNode;
  }) => {
    mocks.beforeUpload = beforeUpload;
    return <div>{children}</div>;
  },
  message: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('@ant-design/icons', () => ({
  DeleteOutlined: () => null,
  UploadOutlined: () => null,
}));

vi.mock('@/services/runApi', () => ({
  uploadAgentAvatar: mocks.upload,
  clearAgentAvatar: mocks.clear,
}));

import AgentAvatarModal from '../AgentAvatarModal';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const agent = {
  id: 7,
  name: '销售助手',
  icon: '🤖',
  avatar_url: '',
};

const mounted: Array<{ host: HTMLDivElement; root: Root }> = [];

async function render(open: boolean) {
  const current = mounted[0];
  if (current) {
    await act(async () => {
      current.root.render(
        <AgentAvatarModal agent={agent} open={open} onClose={() => {}} onSaved={() => {}} />,
      );
    });
    return;
  }
  const host = document.createElement('div');
  document.body.appendChild(host);
  const root = createRoot(host);
  mounted.push({ host, root });
  await act(async () => {
    root.render(
      <AgentAvatarModal agent={agent} open={open} onClose={() => {}} onSaved={() => {}} />,
    );
  });
}

beforeEach(() => {
  mocks.beforeUpload = null;
  mocks.createObjectURL.mockReset().mockReturnValue('blob:avatar-preview');
  mocks.revokeObjectURL.mockReset();
  Object.defineProperty(URL, 'createObjectURL', { value: mocks.createObjectURL, configurable: true });
  Object.defineProperty(URL, 'revokeObjectURL', { value: mocks.revokeObjectURL, configurable: true });
  Object.defineProperty(globalThis, 'createImageBitmap', {
    value: vi.fn().mockResolvedValue({ width: 64, height: 64, close: vi.fn() }),
    configurable: true,
  });
});

afterEach(async () => {
  while (mounted.length) {
    const { host, root } = mounted.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = '';
});

describe('AgentAvatarModal preview lifecycle', () => {
  it('revokes the selected object URL as soon as the modal closes', async () => {
    await render(true);
    const file = new File([new Uint8Array([1, 2, 3])], 'avatar.png', { type: 'image/png' });

    await act(async () => {
      await expect(mocks.beforeUpload?.(file)).resolves.toBe(false);
    });
    expect(mocks.createObjectURL).toHaveBeenCalledWith(file);

    await render(false);

    expect(mocks.revokeObjectURL).toHaveBeenCalledWith('blob:avatar-preview');
  });
});
