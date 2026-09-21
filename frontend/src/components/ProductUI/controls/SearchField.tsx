import type { ChangeEvent } from 'react';
import { CloseOutlined, SearchOutlined } from '@ant-design/icons';
export interface SearchFieldProps { value?: string; defaultValue?: string; placeholder?: string; onChange?: (value: string) => void; onSubmit?: (value: string) => void; onClear?: () => void; disabled?: boolean; className?: string; ariaLabel?: string; }
export function SearchField({ value, defaultValue, placeholder = '搜索', onChange, onSubmit, onClear, disabled, className = '', ariaLabel = '搜索' }: SearchFieldProps) {
  const handleChange = (event: ChangeEvent<HTMLInputElement>) => onChange?.(event.target.value);
  return <form className={`product-search-field ${className}`.trim()} role="search" onSubmit={(event) => { event.preventDefault(); onSubmit?.((event.currentTarget.elements.namedItem('query') as HTMLInputElement).value); }}>
    <SearchOutlined aria-hidden="true" /><input name="query" type="search" value={value} defaultValue={defaultValue} placeholder={placeholder} onChange={handleChange} disabled={disabled} aria-label={ariaLabel} />
    {value && <button type="button" onClick={onClear} aria-label="清空搜索" disabled={disabled}><CloseOutlined /></button>}
  </form>;
}
