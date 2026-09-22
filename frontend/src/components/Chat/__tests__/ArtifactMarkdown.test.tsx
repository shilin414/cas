/** @vitest-environment jsdom */
import React from 'react';
import { act } from 'react-dom/test-utils';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ChatImage, MarkdownWithArtifacts } from '../ArtifactMarkdown';

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let container: HTMLDivElement;
let root: Root;
beforeEach(() => { container = document.createElement('div'); document.body.append(container); root = createRoot(container); });
afterEach(() => { act(() => root.unmount()); container.remove(); });
const markdown = `## 销售概览

**查询条件：** 昨天 | 销售部：示例销售部

| 销售部 | 销售收入金额（元，未税） | 销售收入数量 |
|---|---:|---:|
| 示例销售部 | 428,656.8 | 5,050.0 |

---

- **指标说明**：按开票日期汇总。`;
const render = (content: string) => act(() => root.render(<MarkdownWithArtifacts content={content} />));

describe('artifact-aware GFM rendering', () => {
  it('parses a real-shaped pipe table into semantic cells and keeps numeric alignment', () => {
    render(markdown);
    const table = container.querySelector('table');
    expect(table).not.toBeNull();
    expect(table?.querySelectorAll('thead th')).toHaveLength(3);
    expect(table?.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(table?.querySelectorAll('tbody td')[1].textContent).toBe('428,656.8');
    expect(table?.querySelectorAll('thead th')[1].getAttribute('style')).toContain('text-align: right');
    expect(container.querySelector('h2')?.textContent).toBe('销售概览');
    expect(container.querySelector('li strong')?.textContent).toBe('指标说明');
    expect(container.textContent).not.toContain('|---|');
  });

  it('contains wide tables inside a keyboard-focusable scrolling region', () => {
    render(markdown);
    const region = container.querySelector('.artifact-markdown__table-scroll');
    expect(region?.getAttribute('role')).toBe('region');
    expect(region?.getAttribute('aria-label')).toBe('表格，可横向滚动');
    expect(region?.getAttribute('tabindex')).toBe('0');
    expect(region?.querySelector('table')).not.toBeNull();
  });

  it('does not reinterpret pipe tables inside code fences', () => {
    render('```text\n| a | b |\n|---|---|\n| 1 | 2 |\n```');
    expect(container.querySelector('table')).toBeNull();
    expect(container.querySelector('pre code')?.textContent).toContain('|---|---|');
  });

  it('keeps artifact image and public-share link resolution inside table cells', () => {
    act(() => root.render(<MarkdownWithArtifacts
      content={'| 图片 | 文件 |\n|---|---|\n| ![预览](artifacts/chart.png) | [下载](artifacts/chart.png) |'}
      artifacts={[{ artifactId: 'art-1', name: 'chart.png' }]}
      openArtifactUrl={(id) => `/share/test-token/artifacts/${id}/open`}
    />));
    expect(container.querySelector('table img')?.getAttribute('src')).toBe('/share/test-token/artifacts/art-1/open');
    expect(container.querySelector('table a')?.getAttribute('href')).toBe('/share/test-token/artifacts/art-1/open');
  });

  it('does not enable raw HTML or unsafe link protocols', () => {
    render('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[unsafe](javascript:alert%281%29)');
    expect(container.querySelector('script, img')).toBeNull();
    expect(container.querySelector('a')?.getAttribute('href')).not.toMatch(/^javascript:/i);
  });
});

describe('chat image lifecycle and paragraph validity', () => {
  it.each(['load', 'error'])('resets the %s state when ChatImage gets a different src', event => {
    const renderImage = (src: string) => act(() => root.render(<ChatImage src={src} alt="preview" />));
    renderImage('https://example.test/first.png');
    act(() => container.querySelector('img')!.dispatchEvent(new Event(event)));
    expect(container.querySelector('.chat-img__loading')).toBeNull();
    expect(!!container.querySelector('.chat-img--broken')).toBe(event === 'error');

    renderImage('https://example.test/second.png');
    expect(container.querySelector('.chat-img--broken')).toBeNull();
    expect(container.querySelector('.chat-img__loading')).not.toBeNull();
    expect(container.querySelector('img')?.getAttribute('src')).toBe('https://example.test/second.png');
    act(() => container.querySelector('img')!.dispatchEvent(new Event('load')));
    expect(container.querySelector('.chat-img__loading')).toBeNull();
  });

  it.each(['load', 'error'])('resets the %s state when a Markdown external image URL changes', event => {
    render('![preview](https://example.test/first.png)');
    act(() => container.querySelector('img')!.dispatchEvent(new Event(event)));
    render('![preview](https://example.test/second.png)');
    expect(container.querySelector('.chat-img--broken')).toBeNull();
    expect(container.querySelector('.chat-img__loading')).not.toBeNull();
    expect(container.querySelector('img')?.getAttribute('src')).toBe('https://example.test/second.png');
  });

  it('keeps the loaded image node and state when only surrounding streaming text changes', () => {
    render('Before ![preview](https://example.test/same.png) after');
    const image = container.querySelector('img');
    act(() => image!.dispatchEvent(new Event('load')));
    render('Before ![preview](https://example.test/same.png) after more text');
    expect(container.querySelector('img')).toBe(image);
    expect(container.querySelector('.chat-img__loading')).toBeNull();
  });

  it('does not retry a failed image on same-src re-renders', () => {
    act(() => root.render(<ChatImage src="https://example.test/same.png" alt="before" />));
    act(() => container.querySelector('img')!.dispatchEvent(new Event('error')));
    act(() => root.render(<ChatImage src="https://example.test/same.png" alt="after" />));
    expect(container.querySelector('.chat-img--broken')?.textContent).toContain('after');
    expect(container.querySelector('img')).toBeNull();
  });

  it.each([
    'Before ![preview](https://example.test/image.png) after',
    'Before [![preview](https://example.test/image.png)](https://example.test) after',
  ])('uses only phrasing content inside image paragraphs: %s', content => {
    render(content);
    const paragraph = container.querySelector('p')!;
    expect(paragraph.querySelector('img')).not.toBeNull();
    expect(paragraph.querySelector('div, p, pre, section')).toBeNull();
    // HTML reparsing (SSR/hydration) must not split the paragraph around a spinner div.
    const parsed = document.createElement('div');
    parsed.innerHTML = container.innerHTML;
    expect(parsed.querySelectorAll('p')).toHaveLength(1);
    expect(parsed.querySelector('p')?.textContent).toBe(paragraph.textContent);
    expect(paragraph.querySelector('[role="status"]')?.getAttribute('aria-label')).toBeTruthy();
  });
});

describe('long fenced code content', () => {
  it.each(['plain', 'list', 'quote'])('preserves long lines and indentation in a %s fence', nesting => {
    const code = `const value = '${'x'.repeat(2000)}';\n  <not-html> & indented\n`;
    const fence = `\`\`\`text\n${code}\`\`\``;
    const content = nesting === 'list'
      ? `- item\n\n${fence.split('\n').map(line => `  ${line}`).join('\n')}`
      : nesting === 'quote'
        ? fence.split('\n').map(line => `> ${line}`).join('\n')
        : fence;
    render(content);
    expect(container.querySelectorAll('pre > code')).toHaveLength(1);
    expect(container.querySelector('pre > code')?.textContent).toBe(code);
    expect(container.querySelector('not-html')).toBeNull();
    if (nesting === 'list') expect(container.querySelector('li pre')).not.toBeNull();
    if (nesting === 'quote') expect(container.querySelector('blockquote pre')).not.toBeNull();
  });
});


describe('artifact discovery preserves existing images', () => {
  it('does not reload an already loaded image when another artifact arrives', () => {
    const first = { artifactId: 'first', name: 'first.png' };
    act(() => root.render(<MarkdownWithArtifacts content={'![first](artifacts/first.png)'} artifacts={[first]} />));
    const image = container.querySelector('img')!;
    act(() => image.dispatchEvent(new Event('load')));
    act(() => root.render(<MarkdownWithArtifacts
      content={'![first](artifacts/first.png)\n\n![second](artifacts/second.png)'}
      artifacts={[first, { artifactId: 'second', name: 'second.png' }]}
    />));
    expect(container.querySelectorAll('img')).toHaveLength(2);
    expect(container.querySelector('img')).toBe(image);
    expect(image.parentElement?.querySelector('.chat-img__loading')).toBeNull();
  });
});
