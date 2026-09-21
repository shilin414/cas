import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const productUICss = readFileSync(
  fileURLToPath(new URL('../ProductUI.css', import.meta.url)),
  'utf8',
);

describe('MetricCard typography', () => {
  it('uses the body font and tabular numerals for dashboard values', () => {
    const valueRule = productUICss.match(/\.product-metric-card__value\s*\{([^}]*)\}/)?.[1] ?? '';

    expect(valueRule).toContain('font-family: var(--font-body)');
    expect(valueRule).toContain('font-variant-numeric: tabular-nums');
  });
});