// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EmptyState, EntityCard, IconButton, MediaCard, PageHeader, PageSurface, SearchField, StatusBadge } from '..';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const roots: Root[] = [];
function render(element: React.ReactNode) { const host = document.createElement('div'); const root = createRoot(host); roots.push(root); act(() => root.render(element)); return host; }
afterEach(() => { while (roots.length) roots.pop()?.unmount(); });

describe('ProductUI', () => {
  it('renders a structured page surface and header', () => {
    const host = render(<PageSurface width="compact"><PageHeader title="智能体中心" description="发现企业能力" actions={<button>新建</button>} /></PageSurface>);
    expect(host.querySelector('section.product-page-surface--compact')).not.toBeNull();
    expect(host.querySelector('h1')?.textContent).toBe('智能体中心');
    expect(host.textContent).toContain('发现企业能力');
  });

  it('uses native buttons for interactive entity cards', () => {
    const onClick = vi.fn();
    const host = render(<EntityCard title="财务助手" description="生成分析" onClick={onClick} />);
    const button = host.querySelector('button')!;
    act(() => button.click());
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('keeps an interactive entity card footer in the primary click target', () => {
    const onClick = vi.fn();
    const host = render(<EntityCard title="问数小安" footer={<span>打开对话</span>} onClick={onClick} />);
    const footer = host.querySelector('.product-entity-card__footer') as HTMLElement;
    act(() => footer.click());
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('makes the full media card the primary action', () => {
    const onClick = vi.fn();
    const host = render(<MediaCard media={<span>封面</span>} title="应用" description="说明" actions={<span>打开</span>} onClick={onClick} />);
    const button = host.querySelector('button')!;
    expect(button.textContent).toContain('封面');
    expect(button.textContent).toContain('打开');
    act(() => button.click());
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('labels icon buttons and respects disabled state', () => {
    const onClick = vi.fn();
    const host = render(<IconButton label="删除" icon="×" disabled onClick={onClick} />);
    const button = host.querySelector('button')!;
    expect(button.getAttribute('aria-label')).toBe('删除');
    act(() => button.click());
    expect(onClick).not.toHaveBeenCalled();
  });

  it('submits and clears search input accessibly', () => {
    const onSubmit = vi.fn(); const onClear = vi.fn();
    const host = render(<SearchField value="报表" onSubmit={onSubmit} onClear={onClear} />);
    const form = host.querySelector('form')!;
    act(() => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    expect(onSubmit).toHaveBeenCalledWith('报表');
    act(() => (host.querySelector('button[aria-label="清空搜索"]') as HTMLButtonElement).click());
    expect(onClear).toHaveBeenCalledOnce();
  });

  it('exposes feedback and status semantics', () => {
    const host = render(<><StatusBadge tone="success">已完成</StatusBadge><EmptyState title="暂无任务" description="开始执行后会显示在这里" /></>);
    expect(host.querySelector('.product-status-badge--success')?.textContent).toBe('已完成');
    expect(host.textContent).toContain('开始执行后会显示在这里');
  });
});
