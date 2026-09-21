// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import AgentAvatar from '../AgentAvatar';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('AgentAvatar', () => {
  it('waits for a complete image before revealing it and remembers decoded URLs', async () => {
    const url = '/avatars/wenshuxiaoan.png?v=stable';
    const host = document.createElement('div');
    const root = createRoot(host);

    await act(async () => root.render(<AgentAvatar application={{ name: '问数小安', icon: '🤖', avatar_url: url }} />));
    expect(host.querySelector('[data-agent-avatar="loading"]')).toBeTruthy();
    expect(host.querySelector('.agent-avatar__emoji')?.textContent).toContain('🤖');
    expect(host.querySelector('img')?.classList.contains('is-loaded')).toBe(false);

    await act(async () => host.querySelector('img')?.dispatchEvent(new Event('load')));
    expect(host.querySelector('[data-agent-avatar="image"]')).toBeTruthy();
    expect(host.querySelector('.agent-avatar__emoji')).toBeNull();
    expect(host.querySelector('img')?.classList.contains('is-loaded')).toBe(true);

    await act(async () => root.unmount());

    const secondHost = document.createElement('div');
    const secondRoot = createRoot(secondHost);
    await act(async () => secondRoot.render(<AgentAvatar application={{ name: '问数小安', icon: '🤖', avatar_url: url }} />));
    expect(secondHost.querySelector('[data-agent-avatar="image"]')).toBeTruthy();
    expect(secondHost.querySelector('.agent-avatar__emoji')).toBeNull();
    await act(async () => secondRoot.unmount());
  });

  it('falls back to the application icon when an avatar URL returns 404', async () => {
    const host = document.createElement('div');
    const root = createRoot(host);
    await act(async () => root.render(<AgentAvatar application={{ name: '问数小安', icon: '🤖', avatar_url: '/missing.png' }} />));
    expect(host.querySelector('[data-agent-avatar="loading"]')).toBeTruthy();
    const image = host.querySelector('img');
    await act(async () => image?.dispatchEvent(new Event('error')));
    expect(host.querySelector('[data-agent-avatar="emoji"]')?.textContent).toContain('🤖');
    await act(async () => root.unmount());
  });
});
