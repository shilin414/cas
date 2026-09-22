/** @vitest-environment jsdom */
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import AssistantResponse from '../AssistantResponse';
import type { ChatMessage } from '@/stores/useRunChatStore';

vi.mock('../ArtifactMarkdown', () => ({
  MarkdownWithArtifacts: ({ content }: { content: string }) => <div data-final-markdown>{content}</div>,
}));
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;
const message = (patch: Partial<ChatMessage> = {}): ChatMessage => ({
  id: 'run-test', role: 'assistant', content: '', created_at: '', status: 'streaming', ...patch,
});
const render = (patch: Partial<ChatMessage> = {}) => act(() => root.render(<AssistantResponse message={message(patch)} />));
beforeEach(() => { container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
afterEach(() => { act(() => root.unmount()); container.remove(); });

describe('AssistantResponse execution disclosure', () => {
  it('shows a compact waiting state before the first delta, not an empty answer', () => {
    render();
    expect(container.textContent).toContain('正在准备回复');
    expect(container.querySelector('[data-final-markdown]')).toBeNull();
    expect(container.querySelector('button')).toBeNull();
  });

  it('opens process automatically on the first delta and never renders it as a final answer', () => {
    render();
    render({ processText: '正在查询业务数据', content: '未确认内容' });
    const toggle = container.querySelector('button')!;
    expect(container.querySelector('[data-final-markdown]')).toBeNull();
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(container.querySelector('.run-chat-process__text')?.textContent).toBe('正在查询业务数据');
    const region = document.getElementById(toggle.getAttribute('aria-controls')!);
    expect(region?.hidden).toBe(false);
    render({ processText: '正在查询业务数据\n查询完成' });
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(region?.textContent).toContain('查询完成');
  });

  it('collapses on completion, displays the authoritative answer, and allows reopening', () => {
    render({ processText: '查询中' });
    expect(container.querySelector('button')!.getAttribute('aria-expanded')).toBe('true');
    render({ status: 'done', processText: '查询完成', content: '最终答案' });
    const toggle = container.querySelector('button')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('[data-final-markdown]')?.textContent).toBe('最终答案');
    expect(container.textContent).toContain('已完成');
    act(() => toggle.click());
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  });

  it.each(['failed', 'cancelled'] as const)('keeps %s process inspectable without presenting it as an answer', (status) => {
    render({ status, processText: '未完成的过程' });
    expect(container.querySelector('button')).not.toBeNull();
    expect(container.querySelector('[data-final-markdown]')).toBeNull();
    expect(container.textContent).not.toContain('正在准备回复');
  });

  it('respects manual collapse while progress and retries continue', () => {
    render({ processText: '开始查询' });
    const toggle = container.querySelector('button')!;
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    act(() => toggle.click());
    render({ processText: '开始查询\n等待重试', retryNotice: '正在重试…' });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector<HTMLElement>('.run-chat-process__body')!.hidden).toBe(true);
    act(() => toggle.click());
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  });

  it('loads completed history with its process collapsed', () => {
    render({ status: 'done', processText: '历史过程', content: '历史答案' });
    expect(container.querySelector('button')!.getAttribute('aria-expanded')).toBe('false');
  });

  it('renders historical final answers without an empty execution disclosure', () => {
    render({ status: 'done', content: '历史回复' });
    expect(container.querySelector('button')).toBeNull();
    expect(container.querySelector('[data-final-markdown]')?.textContent).toBe('历史回复');
  });

  it('shows retry state without discarding existing progress', () => {
    render({ processText: '已完成查询', retryNotice: '服务繁忙，正在重试…' });
    expect(container.textContent).toContain('服务繁忙，正在重试…');
    expect(container.querySelector('button')!.getAttribute('aria-expanded')).toBe('true');
    expect(container.textContent).toContain('已完成查询');
  });

  it('explains a completed textless result unless artifact cards provide the result', () => {
    render({ status: 'done', processText: '过程不是答案' });
    expect(container.textContent).toContain('本次执行未返回文字回复');
    expect(container.querySelector('[data-final-markdown]')).toBeNull();
    render({ status: 'done', artifacts: [{ artifactId: 'a', name: '报告', normalizedType: 'file' }] });
    expect(container.textContent).not.toContain('本次执行未返回文字回复');
  });

  it('renders process as plain text, not executable markup or external images', () => {
    render({ processText: '<img src=x onerror=alert(1)> ![图](https://example.test/a.png)' });
    expect(container.querySelector('button')!.getAttribute('aria-expanded')).toBe('true');
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('.run-chat-process__text')?.textContent).toContain('<img');
  });
});
