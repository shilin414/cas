// @vitest-environment jsdom

import { beforeEach, describe, expect, it } from 'vitest';
import { SELECTABLE_THEME_PRESETS, getThemePreset } from '@/components/Theme/themePresets';
import { applyThemeToDOM, useThemeStore } from '../useThemeStore';

const persistedTheme = (theme: string) => JSON.stringify({ state: { theme }, version: 0 });

describe('useThemeStore', () => {
  beforeEach(async () => {
    localStorage.clear();
    document.documentElement.className = '';
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.removeAttribute('style');
    useThemeStore.setState({ theme: 'feishu' });
    applyThemeToDOM('feishu');
    await useThemeStore.persist.clearStorage();
  });

  it('applies every selectable theme to the DOM', () => {
    for (const preset of SELECTABLE_THEME_PRESETS) {
      applyThemeToDOM(preset.id);
      expect(document.documentElement.dataset.theme).toBe(preset.id);
      expect(document.documentElement.style.getPropertyValue('--color-primary')).toBe(preset.colors.primary);
      expect(document.documentElement.classList.contains('dark')).toBe(preset.mode === 'dark');
    }
  });

  it('applies dark and light theme modes independently from theme ids', () => {
    useThemeStore.getState().setTheme('midnight');
    expect(document.documentElement.dataset.theme).toBe('midnight');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(document.documentElement.style.getPropertyValue('--color-primary'))
      .toBe(getThemePreset('midnight').colors.primary);

    useThemeStore.getState().setTheme('sage');
    expect(document.documentElement.dataset.theme).toBe('sage');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('restores persisted built-in and Feishu-style themes', async () => {
    for (const theme of ['light', 'dark', 'feishu'] as const) {
      localStorage.setItem('theme-storage', persistedTheme(theme));
      await useThemeStore.persist.rehydrate();
      expect(useThemeStore.getState().theme).toBe(theme);
      expect(document.documentElement.dataset.theme).toBe(theme);
    }
  });

  it('falls back to Feishu style for an invalid persisted theme', async () => {
    localStorage.setItem('theme-storage', persistedTheme('something-invalid'));
    await useThemeStore.persist.rehydrate();
    expect(useThemeStore.getState().theme).toBe('feishu');
    expect(document.documentElement.dataset.theme).toBe('feishu');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });
});
