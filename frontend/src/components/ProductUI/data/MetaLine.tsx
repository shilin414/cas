import type { ReactNode } from 'react';
export function MetaLine({ children, className = '' }: { children: ReactNode; className?: string }) { return <div className={`product-meta-line ${className}`.trim()}>{children}</div>; }
