import React from 'react';
import { displayResult } from './model';

type RecordRow = Record<string, unknown>;
function isRecord(value: unknown): value is RecordRow { return value !== null && typeof value === 'object' && !Array.isArray(value); }
/** Presentation-only projection. The untouched response remains available below. */
export function resultRecords(data: unknown): RecordRow[] | null {
 const candidate = isRecord(data) && Array.isArray(data.output) ? data.output : data;
 return Array.isArray(candidate) && candidate.length > 0 && candidate.every(isRecord) ? candidate : null;
}
export default function BusinessResultView({ data }: { data: unknown }) {
 const records = resultRecords(data);
 if (!records) return <pre className="business-app__data">{displayResult(data)}</pre>;
 const visible = records.slice(0, 100);
 const columns = Array.from(new Set(visible.flatMap(record => Object.keys(record))));
 const cell = (value: unknown) => value == null ? '—' : typeof value === 'object' ? JSON.stringify(value) : String(value);
 return <div>
  <p className="business-app__record-count">查询到 {records.length} 条记录{records.length > 100 ? '，列表展示前100条，完整内容见原始结果' : ''}</p>
  <div className="business-app__table-wrap" tabIndex={0} role="region" aria-label="物料结果表格，可横向滚动"><table className="business-app__table"><thead><tr>{columns.map(column => <th key={column}>{column}</th>)}</tr></thead><tbody>{visible.map((record, index) => <tr key={index}>{columns.map(column => <td key={column}>{cell(record[column])}</td>)}</tr>)}</tbody></table></div>
  <div className="business-app__record-cards">{visible.map((record, index) => <article key={index}><h3>记录 {index + 1}</h3><dl>{Object.entries(record).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{cell(value)}</dd></div>)}</dl></article>)}</div>
  <details className="business-app__raw"><summary>查看完整原始结果</summary><pre className="business-app__data">{displayResult(data)}</pre></details>
 </div>;
}
