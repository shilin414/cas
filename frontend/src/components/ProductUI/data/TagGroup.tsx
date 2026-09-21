import type { ReactNode } from 'react';
export function TagGroup({ children, className = '' }: { children: ReactNode; className?: string }) { return <div className={`product-tag-group ${className}`.trim()}>{children}</div>; }
