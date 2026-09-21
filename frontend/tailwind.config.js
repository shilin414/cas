/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: ['class'],
  theme: {
    extend: {
      colors: {
        void: 'var(--color-bg-void)', surface: 'var(--color-bg-surface)', card: 'var(--color-bg-card)', elevated: 'var(--color-bg-elevated)',
        hover: 'var(--color-bg-hover)', active: 'var(--color-bg-active)', selected: 'var(--color-bg-selected)',
        border: { DEFAULT: 'var(--color-border)', lit: 'var(--color-border-lit)', hover: 'var(--color-border-hover)', focus: 'var(--color-border-focus)' },
        text: { DEFAULT: 'var(--color-text)', sec: 'var(--color-text-sec)', dim: 'var(--color-text-dim)', disabled: 'var(--color-text-disabled)' },
        primary: { DEFAULT: 'var(--color-primary)', hover: 'var(--color-primary-hover)', foreground: 'var(--color-on-primary)', soft: 'var(--color-primary-soft)', softer: 'var(--color-primary-softer)' },
        success: { DEFAULT: 'var(--color-success)', soft: 'var(--color-success-soft)' },
        warning: { DEFAULT: 'var(--color-warning)', soft: 'var(--color-warning-soft)' },
        error: { DEFAULT: 'var(--color-error)', soft: 'var(--color-error-soft)' },
      },
      fontFamily: { display: ['var(--font-display)'], body: ['var(--font-body)'], mono: ['var(--font-mono)'] },
      fontSize: {
        display: 'var(--font-size-display)', 'page-title': 'var(--font-size-page-title)', 'section-title': 'var(--font-size-section-title)',
        'card-title': 'var(--font-size-card-title)', body: 'var(--font-size-body)', meta: 'var(--font-size-meta)', caption: 'var(--font-size-caption)',
      },
      borderRadius: { control: 'var(--radius-control)', card: 'var(--radius-card)', panel: 'var(--radius-panel)', dialog: 'var(--radius-dialog)', large: 'var(--radius-large)', pill: 'var(--radius-pill)' },
      spacing: {
        1: 'var(--space-1)', 2: 'var(--space-2)', 3: 'var(--space-3)', 4: 'var(--space-4)', 5: 'var(--space-5)', 6: 'var(--space-6)',
        7: 'var(--space-7)', 8: 'var(--space-8)', 9: 'var(--space-9)', 10: 'var(--space-10)', 11: 'var(--space-11)',
        header: 'var(--header-h)', sidebar: 'var(--sidebar-w)',
      },
      maxWidth: { compact: 'var(--content-compact)', content: 'var(--content-default)', wide: 'var(--content-wide)', max: 'var(--content-max)' },
      transitionDuration: { fast: 'var(--motion-fast)', normal: 'var(--motion-normal)', slow: 'var(--motion-slow)' },
      transitionTimingFunction: { standard: 'var(--ease-standard)', emphasized: 'var(--ease-emphasized)' },
      animation: {
        'fade-in': 'fadeIn var(--motion-normal) var(--ease-emphasized)',
        'fade-in-scale': 'fadeInScale var(--motion-slow) var(--ease-emphasized)',
        'slide-in-right': 'slideInRight var(--motion-slow) var(--ease-emphasized)',
        'typing-dot': 'typingDot 1.4s ease-in-out infinite',
      },
    },
  },
  plugins: [require('@tailwindcss/forms')],
};
