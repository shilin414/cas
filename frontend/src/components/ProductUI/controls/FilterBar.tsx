import type { ReactNode } from 'react';
export function FilterBar({ children, className = '' }: { children: ReactNode; className?: string }) { return <div className={`product-filter-bar ${className}`.trim()}>{children}</div>; }
