// @vitest-environment jsdom
/**
 * handleFiles 的附件链路集成测试：超限图片本地压缩后再上传（压缩功能 §2），
 * 以及顺带修复的 chip 重复 bug（多选文件时只 append 本轮 chip）。
 * Mock 风格照 RunChatPanel.home.test.tsx。
 */
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { message as antdMessage } from 'antd';

vi.mock('@/shell/useIsMobile', () => ({ useIsMobile: () => false }));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: () => ({ user: { id: 1, username: 'test' } }) }));
vi.mock('@/services/runApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/services/runApi')>();
  return {
    ...actual,
    uploadAttachment: vi.fn(),
    validateAttachment: actual.validateAttachment,
  };
});
vi.mock('@/lib/compressImage', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/compressImage')>();
  return {
    ...actual,
    compressImage: vi.fn(),
    // isCompressibleImage 用真实实现（纯逻辑），只 mock 重 IO 的压缩本体。
    isCompressibleImage: actual.isCompressibleImage,
  };
});

import RunChatPanel from '../RunChatPanel';
import { useRunChatStore } from '@/stores/useRunChatStore';
import { useWorkspaceStore } from '@/stores/useWorkspaceStore';
import { uploadAttachment } from '@/services/runApi';
import { compressImage, ImageCompressionError } from '@/lib/compressImage';
import type { ComposerApplication } from '@/services/runApi';

const app = {
  id: 7, slug: 'agent-seven', name: '问数小安', kind: 'chat', description: '查询数据',
  icon: '', color: '', capabilities: { attachment: true },
} as ComposerApplication;

const fiveMb = 5 * 1024 * 1024;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  useRunChatStore.setState({
    conversations: {}, activeConversationId: null, isLoading: false, error: null,
    sendMessage: vi.fn(), loadConversation: vi.fn().mockResolvedValue(undefined),
  });
  useWorkspaceStore.setState({ workspaces: {} });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  (globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
});

async function mount() {
  await act(async () => root.render(
    <MemoryRouter>
      <RunChatPanel applicationId={7} application={app} conversationId={null} />
    </MemoryRouter>,
  ));
}

/** 直接触发组件里唯一的隐藏 file input 的 change（桌面 + 移动共用入口）。 */
async function pickFiles(files: File[]) {
  const input = host.querySelector('input[type="file"]') as HTMLInputElement;
  expect(input).toBeTruthy();
  Object.defineProperty(input, 'files', { value: files, configurable: true });
  await act(async () => {
    input.dispatchEvent(new Event('change', { bubbles: true }));
    // handleFiles 是 async：让压缩与上传的微任务/宏任务链跑完。
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function chipNames(): string[] {
  return Array.from(host.querySelectorAll('.run-chat-upload-chip .run-chat-upload-name'))
    .map((el) => el.textContent || '');
}

function pngFile(name: string, size: number): File {
  return new File([new Uint8Array(size)], name, { type: 'image/png' });
}

describe('handleFiles attachment pipeline', () => {
  it('compresses oversize images before uploading and notifies once', async () => {
    await mount();
    const original = pngFile('big-photo.png', 6 * 1024 * 1024);
    const compressed = new File([new Uint8Array(1024)], 'big-photo.jpg', { type: 'image/jpeg' });
    vi.mocked(compressImage).mockResolvedValue(compressed);
    vi.mocked(uploadAttachment).mockResolvedValue({
      id: 'att-1', name: 'big-photo.jpg', size: 1024, attachment_type: 'image',
    });
    const infoSpy = vi.spyOn(antdMessage, 'info');

    await pickFiles([original]);

    expect(compressImage).toHaveBeenCalledWith(original);
    expect(uploadAttachment).toHaveBeenCalledWith(7, compressed);
    expect(infoSpy).toHaveBeenCalledTimes(1);
    expect(infoSpy.mock.calls[0][0]).toContain('big-photo.png');
    expect(infoSpy.mock.calls[0][0]).toContain('已自动压缩');
    // chip 保留原始文件名并进入 ready。
    expect(chipNames()).toEqual(['big-photo.png']);
  });

  it('uploads normal files without compressing', async () => {
    await mount();
    const small = pngFile('small.png', 1000);
    vi.mocked(uploadAttachment).mockResolvedValue({
      id: 'att-2', name: 'small.png', size: 1000, attachment_type: 'image',
    });

    await pickFiles([small]);

    expect(compressImage).not.toHaveBeenCalled();
    expect(uploadAttachment).toHaveBeenCalledWith(7, small);
    expect(chipNames()).toEqual(['small.png']);
  });

  it('shows the compression error and drops the chip when compression fails', async () => {
    await mount();
    const oversize = pngFile('bad.png', 6 * 1024 * 1024);
    vi.mocked(compressImage).mockRejectedValue(
      new ImageCompressionError('「bad.png」压缩失败，请换一张图片'));
    const errorSpy = vi.spyOn(antdMessage, 'error');

    await pickFiles([oversize]);

    expect(uploadAttachment).not.toHaveBeenCalled();
    expect(errorSpy).toHaveBeenCalledWith('「bad.png」压缩失败，请换一张图片');
    expect(chipNames()).toEqual([]);
  });

  it('does not duplicate chips when several files are picked at once', async () => {
    await mount();
    const a = pngFile('a.png', 1000);
    const b = pngFile('b.jpg', 2000);
    vi.mocked(uploadAttachment).mockImplementation(async () => ({
      id: 'att-x', name: 'x', size: 1, attachment_type: 'image',
    }));

    await pickFiles([a, b]);

    // 回归：旧实现把累积数组整体 append，多选时前面的 chip 被重复插入。
    expect(chipNames()).toEqual(['a.png', 'b.jpg']);
  });

  it('still rejects non-image oversize files via validateAttachment', async () => {
    await mount();
    const bigPdf = new File([new Uint8Array(41 * 1024 * 1024)], 'doc.pdf', { type: 'application/pdf' });
    const warnSpy = vi.spyOn(antdMessage, 'warning');

    await pickFiles([bigPdf]);

    expect(compressImage).not.toHaveBeenCalled();
    expect(uploadAttachment).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith('doc.pdf：文件不能超过 40MB');
    expect(chipNames()).toEqual([]);
  });
});
