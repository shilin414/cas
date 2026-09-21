import { describe, expect, it } from 'vitest';
import { appDefinitions, buildInput, displayResult, needsPassword } from '../model';
describe('business app models', () => {
 it('registers exactly seven deterministic apps', () => { expect(Object.keys(appDefinitions)).toHaveLength(7); });
 it('never sends hidden stale fields', () => {
  expect(buildInput('oa-unlock', 'unlock', { usercode: '001', password: 'stale', mobile: 'secret' })).toEqual({ usercode: '001', confirmed: true });
  expect(buildInput('tpm-account', 'lock', { usercode: '001', password: 'stale' })).toEqual({ usercode: '001', action: 'lock', confirmed: true });
 });
 it('keeps barcodes as strings, including leading zero', () => { expect(buildInput('barcode-query', 'flow', { barcode: '01234567890123456789' })).toEqual({ action: 'flow', barcode: '01234567890123456789' }); });
 it('handles real and escaped line breaks without interpreting HTML', () => { expect(displayResult('工厂: H010\\r\\n名称: <script>')).toBe('工厂: H010\n名称: <script>'); });
 it('preserves unfamiliar material structures', () => { expect(displayResult([{ code: 'A1' }])).toContain('A1'); expect(displayResult(null)).toBe('未查询到相关记录'); });
 it('only reset actions collect passwords', () => { expect(needsPassword('tpm-account', 'reset')).toBe(true); expect(needsPassword('tpm-account', 'unlock')).toBe(false); });
});

import { resultRecords } from '../BusinessResultView';
describe('material result projection', () => {
 it('supports FDL output records without altering the envelope', () => { const data = { output: [{ code: 'ABC' }], code: 200, message: 'ok' }; expect(resultRecords(data)).toEqual(data.output); expect(displayResult(data)).toContain('message'); });
 it('retains text, empty lists and unfamiliar nested objects as raw result', () => { expect(resultRecords([])).toBeNull(); expect(resultRecords('text')).toBeNull(); expect(resultRecords({ output: 'unknown' })).toBeNull(); });
});
